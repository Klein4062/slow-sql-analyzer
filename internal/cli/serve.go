package cli

import (
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Klein4062/slow-sql-analyzer/internal/api"
)

func newServeCmd() *cobra.Command {
	var addr, dsn string
	var writeTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP API (POST /v1/plan and /v1/analyze)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dsn == "" {
				dsn = os.Getenv("PGDATABASE_DSN")
			}
			srv := api.New(api.Config{DefaultDSN: dsn, WriteTimeout: writeTimeout})
			cmd.Printf("listening on %s (default DSN %s)\n", addr, dsnStatus(dsn))
			return http.ListenAndServe(addr, srv.Handler())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	cmd.Flags().StringVar(&dsn, "dsn", "", "default PostgreSQL DSN for /v1/analyze (or PGDATABASE_DSN)")
	cmd.Flags().DurationVar(&writeTimeout, "explain-timeout", 30*time.Second, "statement_timeout used for /v1/analyze")
	return cmd
}

func dsnStatus(dsn string) string {
	if dsn == "" {
		return "unset — /v1/analyze requires a request-level 'dsn'"
	}
	return "set"
}
