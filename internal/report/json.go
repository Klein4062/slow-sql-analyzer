package report

import (
	"encoding/json"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
)

// jsonReport is the wire shape for --format json.
type jsonReport struct {
	Query         string             `json:"query,omitempty"`
	IsAnalyze     bool               `json:"is_analyze"`
	ExecutionTime float64            `json:"execution_time_ms,omitempty"`
	PlanningTime  float64            `json:"planning_time_ms,omitempty"`
	Summary       summary            `json:"summary"`
	PlanTree      *treeNode          `json:"plan_tree,omitempty"`
	Findings      []analyzer.Finding `json:"findings"`
	Actions       actionJSON         `json:"actions"`
}

type summary struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}

type actionJSON struct {
	Indexes []string `json:"indexes"`
	Analyze []string `json:"analyze"`
	Config  []string `json:"config"`
}

// RenderJSON produces a machine-readable JSON report.
func RenderJSON(m Model) ([]byte, error) {
	counts := summary{}
	for _, f := range m.Findings {
		switch f.Severity {
		case analyzer.SeverityCritical:
			counts.Critical++
		case analyzer.SeverityWarning:
			counts.Warning++
		case analyzer.SeverityInfo:
			counts.Info++
		}
	}

	// Pre-initialize slices so empty arrays serialize as [] (not null), giving
	// API consumers a stable contract.
	aj := actionJSON{Indexes: []string{}, Analyze: []string{}, Config: []string{}}
	for _, a := range m.Actions {
		sql := a.SQL
		switch a.Kind {
		case "index":
			aj.Indexes = append(aj.Indexes, sql)
		case "analyze":
			aj.Analyze = append(aj.Analyze, sql)
		case "config":
			aj.Config = append(aj.Config, sql)
		}
	}

	rep := jsonReport{
		Query:    m.Query,
		Summary:  counts,
		Findings: m.Findings,
		Actions:  aj,
	}
	if m.Result != nil {
		rep.IsAnalyze = m.Result.IsAnalyze
		rep.ExecutionTime = m.Result.ExecutionTime
		rep.PlanningTime = m.Result.PlanningTime
	}
	rep.PlanTree = buildPlanTree(m.Result, m.Findings)
	if rep.Findings == nil {
		rep.Findings = []analyzer.Finding{}
	}

	return json.MarshalIndent(rep, "", "  ")
}
