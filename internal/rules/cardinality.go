package rules

import (
	"fmt"
	"math"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// CardinalityMisestimate flags nodes where the actual row count differs from
// the planner's estimate by a large factor. Misestimates are the root cause of
// most bad plans (under-sized hash tables, wrong join method, nested loops run
// far longer than expected).
type CardinalityMisestimate struct{}

// Name implements analyzer.Rule.
func (CardinalityMisestimate) Name() string { return "CardinalityMisestimate" }

// Analyze implements analyzer.Rule.
func (CardinalityMisestimate) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	if !ctx.HasAnalyze() {
		return nil
	}
	var out []analyzer.Finding
	t := ctx.Thresholds

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		if !node.HasActual() {
			return true
		}
		actual := node.ActualRows
		estimate := node.PlanRows
		if actual < t.CardinalityMinActual {
			return true
		}

		var ratio float64
		if estimate <= 0 {
			ratio = math.Inf(1)
		} else {
			ratio = math.Max(actual/estimate, estimate/actual)
		}
		if ratio < t.CardinalityRatio {
			return true
		}

		direction := "under-estimated"
		if actual < estimate {
			direction = "over-estimated"
		}

		severity := analyzer.SeverityWarning
		if ratio >= 100 || (math.IsInf(ratio, 1) && estimate == 0) {
			severity = analyzer.SeverityCritical
		}

		target := relationOrNode(node)
		var rec string
		if node.RelationName != "" {
			rec = fmt.Sprintf(
				"run ANALYZE on %s to refresh statistics; if multiple columns are correlated, "+
					"create extended statistics (CREATE STATISTICS) on them",
				target,
			)
		} else {
			rec = "run ANALYZE on the tables feeding this node to refresh statistics; " +
				"for correlated predicate columns, create extended statistics (CREATE STATISTICS)"
		}

		out = append(out, analyzer.Finding{
			Severity:       severity,
			Rule:           "CardinalityMisestimate",
			NodeLabel:      node.Label(),
			NodePath:       joinPath(path),
			NodeType:       node.NodeType,
			RelationName:   node.QualifiedName(),
			Problem: fmt.Sprintf(
				"planner %s rows: estimated %s but actual %s (%.0fx off) at %q",
				direction, formatRows(estimate), formatRows(actual), ratio, node.Label(),
			),
			Recommendation: rec,
			Evidence: map[string]any{
				"estimated_rows": estimate,
				"actual_rows":    actual,
				"ratio":          ratio,
				"direction":      direction,
			},
		})
		return true
	})

	return out
}

// relationOrNode returns the relation name for scan nodes, else the node type.
func relationOrNode(n *plan.PlanNode) string {
	if n.RelationName != "" {
		return n.QualifiedName()
	}
	return n.NodeType
}
