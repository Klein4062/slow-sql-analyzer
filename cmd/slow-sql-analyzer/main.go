// slow-sql-analyzer analyzes PostgreSQL query plans for optimality and suggests
// concrete fixes (missing indexes, work_mem tuning, ANALYZE, query rewrites).
//
// It works offline on a captured EXPLAIN (FORMAT JSON) document or live against
// a PostgreSQL instance, via CLI or HTTP API.
package main

import (
	"fmt"
	"os"

	"github.com/Klein4062/slow-sql-analyzer/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
