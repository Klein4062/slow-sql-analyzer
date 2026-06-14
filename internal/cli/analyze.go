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
	)
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Run EXPLAIN against a live PostgreSQL database and analyze the plan",
		Long: `Connect to PostgreSQL, run EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on the
given query, and analyze the resulting plan.

The query runs inside a READ ONLY transaction with a statement_timeout, so
write statements are rejected unless --allow-writes is given (in which case the
transaction is still rolled back). Use --no-analyze for an estimate-only plan
(EXPLAIN without ANALYZE) when you must not execute the query.`,
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
			if dsn == "" {
				dsn = os.Getenv("PGDATABASE_DSN")
			}
			if dsn == "" {
				return fmt.Errorf("a --dsn (or PGDATABASE_DSN env) is required")
			}
			src := source.PostgresSource{
				DSN:         dsn,
				Query:       q,
				Analyze:     !noAnalyze,
				AllowWrites: allowWrites,
				Timeout:     timeout,
			}
			return runAnalysis(cmd.OutOrStdout(), src, q)
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "PostgreSQL connection string (or set PGDATABASE_DSN)")
	cmd.Flags().StringVar(&query, "query", "", "SQL query to analyze")
	cmd.Flags().StringVarP(&queryFile, "file", "f", "", "read the SQL query from this file")
	cmd.Flags().BoolVar(&noAnalyze, "no-analyze", false, "estimate-only EXPLAIN (do not execute the query)")
	cmd.Flags().BoolVar(&allowWrites, "allow-writes", false, "allow write/DDL statements (rolled back, never committed)")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "statement timeout for the EXPLAIN")
	return cmd
}
