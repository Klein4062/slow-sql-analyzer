package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// NestedLoopExpensiveInner flags Nested Loop joins whose inner side is rescanned
// many times and is itself an expensive scan (worst case: a Seq Scan). Each
// outer row re-runs the inner scan, so the cost multiplies — this is one of the
// most damaging patterns and usually means the inner side lacks a useful index.
//
// 触发条件：Nested Loop 的内表被重复扫描（Actual Loops >= 阈值），且内表本身是
// 昂贵扫描（最坏是 Seq Scan——外层每行都全表扫一次，代价成倍放大）。需要 ANALYZE。
type NestedLoopExpensiveInner struct{}

// Name implements analyzer.Rule.
func (NestedLoopExpensiveInner) Name() string { return "NestedLoopExpensiveInner" }

// Analyze implements analyzer.Rule.
func (NestedLoopExpensiveInner) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	if !ctx.HasAnalyze() {
		return nil
	}
	var out []analyzer.Finding
	t := ctx.Thresholds

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		if node.NodeType != "Nested Loop" {
			return true
		}
		inner := childByRelationship(node, "Inner")
		if inner == nil || !inner.HasActual() {
			return true
		}
		loops := inner.ActualLoops
		if loops < t.NestedLoopMinLoops {
			return true
		}

		// Determine severity: a re-scanned Seq Scan is catastrophic.
		severity := analyzer.SeverityInfo
		detail := ""
		switch {
		case inner.NodeType == "Seq Scan":
			severity = analyzer.SeverityCritical
			detail = fmt.Sprintf(
				"the inner Seq Scan on %s ran %s times — that is %s sequential scans",
				inner.QualifiedName(), formatRows(loops), formatRows(loops),
			)
		case inner.IsScan() && inner.TotalCost >= 4.0:
			severity = analyzer.SeverityWarning
			detail = fmt.Sprintf(
				"the inner %s (cost %.1f) ran %s times",
				inner.NodeType, inner.TotalCost, formatRows(loops),
			)
		default:
			return true
		}

		out = append(out, analyzer.Finding{
			Severity:  severity,
			Rule:      "NestedLoopExpensiveInner",
			NodeLabel: node.Label(),
			NodePath:  joinPath(path),
			NodeType:  node.NodeType,
			Problem: fmt.Sprintf(
				"Nested Loop re-executes its inner side %s times; %s",
				formatRows(loops), detail,
			),
			Recommendation: fmt.Sprintf(
				"ensure the inner relation %s has a supporting index on the join key, "+
					"or let the planner pick a hash/merge join instead",
				inner.QualifiedName(),
			),
			Evidence: mergeEvidence(
				indexEvidence(inner.QualifiedName(), inner.Alias, inner.Filter, inner.IndexCond, inner.PlanRows),
				map[string]any{
					"inner_node":     inner.NodeType,
					"inner_relation": inner.QualifiedName(),
					"inner_loops":    loops,
					"inner_cost":     inner.TotalCost,
				},
			),
		})
		return true
	})

	return out
}

// childByRelationship returns the direct child of node whose
// "Parent Relationship" matches rel ("Outer", "Inner", "Subquery", ...).
func childByRelationship(node *plan.PlanNode, rel string) *plan.PlanNode {
	for _, c := range node.Plans {
		if c.ParentRelationship == rel {
			return c
		}
	}
	return nil
}
