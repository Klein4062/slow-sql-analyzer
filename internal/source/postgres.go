package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// PostgresSource runs EXPLAIN against a live PostgreSQL instance.
//
// Safety: by default the EXPLAIN is executed inside a READ ONLY transaction,
// so write statements are rejected by the server rather than run, and a
// statement_timeout bounds runaway queries. Set AllowWrites to run write
// statements (still never committed — the transaction is rolled back).
type PostgresSource struct {
	DSN        string
	Query      string
	Analyze    bool // include ANALYZE (execute the query). Default true.
	AllowWrites bool // allow write/DDL statements inside a rolled-back tx.
	Timeout    time.Duration
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
	ms := int64(timeout / time.Millisecond)
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = %d", ms)); err != nil {
		return nil, fmt.Errorf("set statement_timeout: %w", err)
	}

	txOpts := pgx.TxOptions{AccessMode: pgx.ReadOnly}
	if s.AllowWrites {
		txOpts.AccessMode = pgx.ReadWrite
	}
	tx, err := conn.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	// Always roll back: we never want to persist any effect of EXPLAIN ANALYZE.
	defer tx.Rollback(ctx) //nolint:errcheck

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
	return result, nil
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
