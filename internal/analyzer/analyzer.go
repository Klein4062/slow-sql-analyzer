package analyzer

import (
	"sort"

	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// Rule detects one class of plan anti-pattern and returns findings for it.
type Rule interface {
	// Name is the stable rule identifier (used for disable flags & JSON).
	Name() string
	// Analyze inspects the plan and returns zero or more findings.
	Analyze(ctx *AnalysisContext) []Finding
}

// Analyzer runs a configured set of rules over a plan.
type Analyzer struct {
	rules []Rule
}

// New builds an Analyzer from an explicit rule list (see internal/rules).
func New(rules []Rule) *Analyzer {
	return &Analyzer{rules: rules}
}

// Report is the full output of an analysis run.
type Report struct {
	Result   *plan.PlanResult `json:"-"`
	Findings []Finding        `json:"findings"`
}

// Run executes all enabled rules and returns their findings, sorted by
// severity (critical first). Rules that require actual stats are expected to
// self-skip when the plan was not run with ANALYZE.
func (a *Analyzer) Run(result *plan.PlanResult, cfg config.Config) Report {
	ctx := NewContext(result, cfg.Thresholds)
	var findings []Finding
	for _, r := range a.rules {
		if !cfg.IsRuleEnabled(r.Name()) {
			continue
		}
		findings = append(findings, r.Analyze(ctx)...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Severity > findings[j].Severity
	})
	return Report{Result: result, Findings: findings}
}

// CountBySeverity returns the number of findings at each severity.
func (r Report) CountBySeverity() map[Severity]int {
	counts := map[Severity]int{}
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	return counts
}
