// Package source abstracts how a plan is obtained: from a pre-captured
// EXPLAIN file (offline) or by running EXPLAIN against a live PostgreSQL
// instance. Both implement PlanSource so the analyzer and report layers stay
// transport-agnostic.
package source

import (
	"fmt"
	"io"
	"os"

	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// PlanSource yields a parsed plan result.
type PlanSource interface {
	// Fetch returns the plan. The returned PlanResult.SourceQuery is set when
	// the originating SQL is known.
	Fetch() (*plan.PlanResult, error)
}

// FileSource reads a pre-captured PostgreSQL EXPLAIN (FORMAT JSON) document
// from a file path or, when Path is empty/"-", from stdin.
type FileSource struct {
	Path  string
	Query string
}

// Fetch implements PlanSource.
func (s FileSource) Fetch() (*plan.PlanResult, error) {
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	result, err := plan.Parse(data)
	if err != nil {
		return nil, err
	}
	result.SourceQuery = s.Query
	return result, nil
}

func (s FileSource) read() ([]byte, error) {
	if s.Path == "" || s.Path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("no plan data on stdin (expected EXPLAIN FORMAT JSON)")
		}
		return data, nil
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.Path, err)
	}
	return data, nil
}
