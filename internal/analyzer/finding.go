// Package analyzer runs detection rules over a parsed plan and collects
// findings. It is pure: given a *plan.PlanResult and config, it returns a
// deterministic slice of Findings with no I/O.
package analyzer

import (
	"encoding/json"

	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// Severity ranks how much a finding matters.
type Severity int

const (
	// SeverityInfo is a low-priority observation (worth knowing, rarely urgent).
	SeverityInfo Severity = iota
	// SeverityWarning suggests a likely improvement.
	SeverityWarning
	// SeverityCritical flags something that typically causes major slowdowns.
	SeverityCritical
)

// String returns a lowercase severity name.
func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "critical"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// Glyph returns a single-character marker for text reports.
func (s Severity) Glyph() string {
	switch s {
	case SeverityCritical:
		return "🔴"
	case SeverityWarning:
		return "⚠"
	case SeverityInfo:
		return "ℹ"
	default:
		return "•"
	}
}

// MarshalJSON renders severity as its name ("critical"/"warning"/"info") in
// JSON output instead of a bare integer.
func (s Severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// Finding is one issue discovered by a rule, with a fix recommendation.
type Finding struct {
	Severity       Severity         `json:"severity"`
	Rule           string           `json:"rule"`
	NodeLabel      string           `json:"node_label"`
	NodePath       string           `json:"node_path"`
	NodeType       string           `json:"node_type"`
	RelationName   string           `json:"relation,omitempty"`
	Problem        string           `json:"problem"`
	Recommendation string           `json:"recommendation"`
	Evidence       map[string]any   `json:"evidence,omitempty"`
}

// AnalysisContext bundles everything a rule needs to inspect a plan.
type AnalysisContext struct {
	// Result is the parsed plan.
	Result *plan.PlanResult
	// Thresholds control rule cut-offs.
	Thresholds config.Thresholds
}

// NewContext builds an AnalysisContext.
func NewContext(result *plan.PlanResult, t config.Thresholds) *AnalysisContext {
	return &AnalysisContext{Result: result, Thresholds: t}
}

// HasAnalyze reports whether the plan carries actual execution stats.
func (c *AnalysisContext) HasAnalyze() bool {
	return c.Result != nil && c.Result.IsAnalyze
}
