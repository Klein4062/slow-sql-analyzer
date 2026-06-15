package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// PostgresSource runs EXPLAIN against a live PostgreSQL instance.
//
// Safety: by default the EXPLAIN is executed inside a READ ONLY transaction,
// so write statements are rejected by the server rather than run, and a
// statement_timeout bounds runaway queries. Set AllowWrites to run write
// statements (still never committed — the transaction is rolled back).
//
// 安全设计：默认在 READ ONLY 事务内执行 EXPLAIN——写语句会被 PG 直接拒绝而非执行；
// 设置 statement_timeout 防止失控查询；--allow-writes 才放开写语句，且仍在 ROLLBACK
// 的事务里（EXPLAIN ANALYZE 对 UPDATE 的实际修改会被回滚）。
type PostgresSource struct {
	DSN         string
	Query       string
	Analyze     bool // include ANALYZE (execute the query). Default true.
	AllowWrites bool // allow write/DDL statements inside a rolled-back tx.
	Timeout     time.Duration
}

// Fetch implements PlanSource.
func (s PostgresSource) Fetch() (*plan.PlanResult, error) {
	if strings.TrimSpace(s.Query) == "" {
		return nil, errors.New("no query to analyze")
	}
	if err := s.guardWrite(); err != nil {
		return nil, err
	}

	timeout := s.timeoutOr()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, s.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer conn.Close(context.Background())

	// Bound runaway queries at the server. SET is a utility command and does
	// not accept $1 parameter binding, so inline the integer (we compute it).
	// 在服务端设置语句超时，防止失控查询。注意：SET 是工具命令，不支持 $1 参数绑定
	// （用 "SET ... = $1" 会报 syntax error at or near "$1"），故直接内联计算好的毫秒整数。
	ms := int64(timeout / time.Millisecond)
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = %d", ms)); err != nil {
		return nil, fmt.Errorf("set statement_timeout: %w", err)
	}

	// 默认只读事务：写语句会被 PG 拒绝；--allow-writes 时改为读写事务（但仍回滚）。
	txOpts := pgx.TxOptions{AccessMode: pgx.ReadOnly}
	if s.AllowWrites {
		txOpts.AccessMode = pgx.ReadWrite
	}
	tx, err := conn.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	// Always roll back: we never want to persist any effect of EXPLAIN ANALYZE.
	// 始终回滚：绝不持久化 EXPLAIN ANALYZE 的任何副作用。
	defer tx.Rollback(ctx) //nolint:errcheck

	// --no-analyze 时只跑估算（不执行查询），适合完全不允许执行的生产库。
	explainSQL := "EXPLAIN (BUFFERS, FORMAT JSON) " + s.Query
	if s.Analyze {
		explainSQL = "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + s.Query
	}

	var raw []byte
	if err := tx.QueryRow(ctx, explainSQL).Scan(&raw); err != nil {
		return nil, fmt.Errorf("run EXPLAIN: %w", err)
	}

	result, err := plan.Parse(raw)
	if err != nil {
		return nil, err
	}
	result.SourceQuery = s.Query

	// 统计新鲜度富化：查 pg_stat_user_tables，供 StaleStatistics 规则使用。
	// 失败不致命（某些权限/版本下视图不可用），静默跳过即可。
	if stats, err := s.queryTableStats(ctx, tx, result.Relations()); err == nil {
		result.TableStats = stats
	}

	return result, nil
}

// queryTableStats fetches statistics-freshness columns for the given relations
// from pg_stat_user_tables (user tables only). Relations not present in the
// view (e.g. system catalogs) are silently skipped.
//
// 按裸表名 relname 匹配——真实计划的 EXPLAIN JSON 常省略 Schema 字段，
// 故不能依赖 "schema.relname" 复合键。结果按 relname 建立（不同 schema 同名表极少见）。
func (s PostgresSource) queryTableStats(ctx context.Context, tx pgx.Tx, qualified []string) (map[string]plan.TableStat, error) {
	relnames := bareRelationNames(qualified)
	if len(relnames) == 0 {
		return nil, nil
	}
	const q = `SELECT schemaname, relname, n_live_tup, n_mod_since_analyze,
	                  last_analyze, last_autoanalyze
	           FROM pg_stat_user_tables
	           WHERE relname = ANY($1)`
	rows, err := tx.Query(ctx, q, relnames)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]plan.TableStat, len(relnames))
	for rows.Next() {
		var ts plan.TableStat
		var lastAnalyze, lastAuto pgtype.Timestamptz
		if err := rows.Scan(&ts.Schema, &ts.Relation, &ts.LiveTuples,
			&ts.ModSinceAnalyze, &lastAnalyze, &lastAuto); err != nil {
			return nil, err
		}
		if lastAnalyze.Valid {
			ts.LastAnalyze = lastAnalyze.Time
		}
		if lastAuto.Valid {
			ts.LastAutoAnalyze = lastAuto.Time
		}
		stats[ts.Relation] = ts
	}
	return stats, rows.Err()
}

// bareRelationNames strips any "schema." prefix and de-duplicates, so we can
// match pg_stat_user_tables by relname regardless of whether the plan carried
// a Schema field.
func bareRelationNames(qualified []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, q := range qualified {
		name := q
		if i := lastDot(q); i >= 0 {
			name = q[i+1:]
		}
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

func (s PostgresSource) timeoutOr() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return 30 * time.Second
}

// guardWrite refuses write/DDL statements unless AllowWrites is set, so the
// user gets a clear message instead of a server-side read-only error.
func (s PostgresSource) guardWrite() error {
	if s.AllowWrites {
		return nil
	}
	switch strings.ToUpper(firstWord(s.Query)) {
	case "INSERT", "UPDATE", "DELETE", "MERGE", "CREATE", "ALTER", "DROP",
		"TRUNCATE", "VACUUM", "REINDEX", "REFRESH", "GRANT", "REVOKE":
		return fmt.Errorf(
			"query looks like a write/DDL statement (%s); EXPLAIN ANALYZE would execute it. "+
				"Pass --allow-writes to run it inside a rolled-back transaction",
			firstWord(s.Query),
		)
	}
	return nil
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '(' {
			return s[:i]
		}
	}
	return s
}
