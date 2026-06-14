package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// InefficientFilter flags scans that read many rows and then throw most of them
// away in a filter, without using an index. When 90%+ of scanned rows are
// rejected, an index on the filter column would avoid reading them at all.
type InefficientFilter struct{}

// Name implements analyzer.Rule.
func (InefficientFilter) Name() string { return "InefficientFilter" }

// Analyze implements analyzer.Rule.
func (InefficientFilter) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	if !ctx.HasAnalyze() {
		return nil
	}
	var out []analyzer.Finding
	t := ctx.Thresholds

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		if !node.IsScan() {
			return true
		}
		// Index scans may still carry a residual filter, but they already use
		// an index — only flag non-index scans here.
		if node.UsesIndex() {
			return true
		}
		if !node.Has("Rows Removed by Filter") || node.Filter == "" {
			return true
		}
		removed := node.RowsRemovedByFilter
		kept := node.ActualRows
		scanned := removed + kept
		if scanned < t.FilterMinScanned || removed <= 0 {
			return true
		}
		ratio := removed / scanned
		if ratio < t.FilterRemovalRatio {
			return true
		}

		severity := analyzer.SeverityWarning
		if scanned >= 100000 {
			severity = analyzer.SeverityCritical
		}

		out = append(out, analyzer.Finding{
			Severity:       severity,
			Rule:           "InefficientFilter",
			NodeLabel:      node.Label(),
			NodePath:       joinPath(path),
			NodeType:       node.NodeType,
			RelationName:   node.QualifiedName(),
			Problem: fmt.Sprintf(
				"%s scanned %s rows but the filter %q discarded %s of them",
				node.NodeType, formatRows(scanned), node.Filter, formatPct(ratio),
			),
			Recommendation: fmt.Sprintf(
				"add an index on %s covering %q so PostgreSQL reads only matching rows",
				node.QualifiedName(), node.Filter,
			),
			Evidence: mergeEvidence(
				indexEvidence(node.QualifiedName(), node.Alias, node.Filter, node.IndexCond, scanned),
				map[string]any{
					"rows_scanned":  scanned,
					"rows_removed":  removed,
					"rows_kept":     kept,
					"removal_ratio": ratio,
				},
			),
		})
		return true
	})

	return out
}
