package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// Hotspot flags the node(s) that dominate execution time, computed as
// "exclusive" time (the node's own time minus its children's time, so it does
// not just echo the root). This points the user at where to focus, even before
// other rules explain why.
type Hotspot struct{}

// Name implements analyzer.Rule.
func (Hotspot) Name() string { return "Hotspot" }

// Analyze implements analyzer.Rule.
func (Hotspot) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	if !ctx.HasAnalyze() {
		return nil
	}
	execTime := ctx.Result.ExecutionTime
	if execTime <= 0 {
		return nil
	}
	var out []analyzer.Finding
	t := ctx.Thresholds

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		if !node.HasActual() {
			return true
		}
		exclusive := exclusiveTime(node)
		if exclusive <= 0 {
			return true
		}
		frac := exclusive / execTime
		if frac < t.HotspotTimeFraction {
			return true
		}

		out = append(out, analyzer.Finding{
			Severity:  analyzer.SeverityWarning,
			Rule:      "Hotspot",
			NodeLabel: node.Label(),
			NodePath:  joinPath(path),
			NodeType:  node.NodeType,
			Problem: fmt.Sprintf(
				"%s spends ~%.1f ms of its own time — %.0f%% of total execution time",
				node.Label(), exclusive, frac*100,
			),
			Recommendation: "this is the main time sink; prioritize fixing any findings on this node or its subtree",
			Evidence: map[string]any{
				"exclusive_ms":      exclusive,
				"fraction_of_total": frac,
			},
		})
		return true
	})

	return out
}

// exclusiveTime returns the node's own time: its total time minus the total
// time of its direct children (per-loop figures, as PostgreSQL reports them).
//
// 计算「独占耗时」= 节点总耗时 - 各直接子节点总耗时。用独占而非累计，是为了
// 避免把根节点（天然≈总执行时间）反复标记为热点；真正花时间的叶子扫描/排序节点
// 才会被高亮。PG 的 ActualTotalTime 是「每轮循环」口径，这里按同口径相减。
func exclusiveTime(node *plan.PlanNode) float64 {
	exclusive := node.ActualTotalTime
	for _, c := range node.Plans {
		if c.HasActual() {
			exclusive -= c.ActualTotalTime
		}
	}
	if exclusive < 0 {
		exclusive = 0 // 浮点相减可能产生极小负值，钳到 0
	}
	return exclusive
}
