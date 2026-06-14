package cli

import (
	"github.com/spf13/cobra"

	"github.com/Klein4062/slow-sql-analyzer/internal/source"
)

func newPlanCmd() *cobra.Command {
	var file, query string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Analyze a pre-captured PostgreSQL EXPLAIN (FORMAT JSON) plan",
		Long: `Analyze an EXPLAIN plan captured as JSON, without any database connection.

Read the plan from --file or, when omitted, from stdin. For example:

    psql -d mydb -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT ..." -t -A \
        | slow-sql-analyzer plan
    slow-sql-analyzer plan -f explain.json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalysis(cmd.OutOrStdout(), source.FileSource{Path: file, Query: query}, query)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to an EXPLAIN JSON file (default: stdin)")
	cmd.Flags().StringVar(&query, "query", "", "originating SQL, shown in the report for context")
	return cmd
}
