// Package report renders an analysis result for humans (text) and machines
// (JSON). Both renderers consume the same Model, built by the CLI/API from an
// analyzer.Report plus the actions derived from it.
package report

import (
	"github.com/Klein4062/slow-sql-analyzer/internal/advise"
	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// Model bundles everything a renderer needs.
type Model struct {
	Result   *plan.PlanResult
	Findings []analyzer.Finding
	Actions  []advise.Action
	NoColor  bool
	// Query is the originating SQL when known (live mode).
	Query string
}

// HasAnalyze reports whether the plan carried actual execution stats.
func (m Model) HasAnalyze() bool {
	return m.Result != nil && m.Result.IsAnalyze
}
