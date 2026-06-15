package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Klein4062/slow-sql-analyzer/internal/source"
)

func newAnalyzeCmd() *cobra.Command {
	var (
		dsn         string
		query       string
		queryFile   string
		noAnalyze   bool
		allowWrites bool
		timeout     time.Duration
		connector   string // 连接器：pgx（内置驱动，默认）或 command（自定义客户端）
		exec        string // command 连接器的命令模板
	)
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Run EXPLAIN against a live PostgreSQL database and analyze the plan",
		Long: `Connect to PostgreSQL, run EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on the
given query, and analyze the resulting plan.

Two connectors are available:

  --connector pgx       (default) built-in pgx driver. Connects directly with
                        --dsn. The query runs in a READ ONLY transaction with a
                        statement_timeout; write statements are rejected unless
                        --allow-writes is given. Zero client install on target.

  --connector command   run your own client to produce EXPLAIN JSON. Pass the
                        command template via --exec, using {dsn}/{sql}
                        placeholders (or the SSA_DSN/SSA_SQL/SSA_TIMEOUT env
                        vars). Useful for psql, bastion/ssh wrappers, or custom
                        scripts. Safety (read-only tx, timeout) is then yours
                        to enforce inside the command.

默认 pgx 连接器在只读事务内执行并设置 statement_timeout；写语句需 --allow-writes。
--exec 命令模板支持 {dsn}/{sql} 占位符，等价地可用 $SSA_DSN/$SSA_SQL/$SSA_TIMEOUT。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := query
			if queryFile != "" {
				data, err := os.ReadFile(queryFile)
				if err != nil {
					return err
				}
				q = string(data)
			}
			if q == "" {
				return fmt.Errorf("provide --query or --file with the SQL to analyze")
			}

			src, err := buildLiveSource(connector, exec, dsn, q, noAnalyze, allowWrites, timeout)
			if err != nil {
				return err
			}
			return runAnalysis(cmd.OutOrStdout(), src, q)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "PostgreSQL connection string (pgx connector; or injected as {dsn}/$SSA_DSN for command connector)")
	cmd.Flags().StringVar(&query, "query", "", "SQL query to analyze")
	cmd.Flags().StringVarP(&queryFile, "file", "f", "", "read the SQL query from this file")
	cmd.Flags().StringVar(&connector, "connector", "pgx", "connector: pgx (built-in driver) or command (custom client via --exec)")
	cmd.Flags().StringVar(&exec, "exec", "", "command connector template, e.g. 'psql {dsn} -At -c \"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) {sql}\"'")
	cmd.Flags().BoolVar(&noAnalyze, "no-analyze", false, "estimate-only EXPLAIN (pgx connector only; do not execute the query)")
	cmd.Flags().BoolVar(&allowWrites, "allow-writes", false, "allow write/DDL statements (pgx connector only; rolled back, never committed)")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "statement/subprocess timeout")
	return cmd
}

// buildLiveSource selects the connector and assembles the corresponding source.
func buildLiveSource(connector, execCmd, dsn, query string, noAnalyze, allowWrites bool, timeout time.Duration) (source.PlanSource, error) {
	// --exec 隐式选择 command 连接器。
	if execCmd != "" && connector == "pgx" {
		connector = "command"
	}

	switch connector {
	case "pgx":
		if dsn == "" {
			dsn = os.Getenv("PGDATABASE_DSN")
		}
		if dsn == "" {
			return nil, fmt.Errorf("pgx connector needs --dsn (or PGDATABASE_DSN env); or use --connector command with --exec")
		}
		return source.PostgresSource{
			DSN:         dsn,
			Query:       query,
			Analyze:     !noAnalyze,
			AllowWrites: allowWrites,
			Timeout:     timeout,
		}, nil
	case "command":
		if execCmd == "" {
			return nil, fmt.Errorf("command connector needs --exec (a command template using {dsn}/{sql} or $SSA_DSN/$SSA_SQL)")
		}
		return source.CommandSource{
			Cmd:     execCmd,
			DSN:     dsn,
			Query:   query,
			Timeout: timeout,
		}, nil
	default:
		return nil, fmt.Errorf("unknown connector %q (use pgx or command)", connector)
	}
}
