package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// SeqScanLargeTable flags sequential scans over many rows, which usually means
// a missing index for the query's filter columns.
//
// 触发条件：Seq Scan 且估算行数超过阈值（默认 1000，可经 --seqscan-rows 调整）。
type SeqScanLargeTable struct{}

// Name implements analyzer.Rule.
func (SeqScanLargeTable) Name() string { return "SeqScanLargeTable" }

// Analyze implements analyzer.Rule.
func (SeqScanLargeTable) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	var out []analyzer.Finding
	t := ctx.Thresholds

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		if !node.IsSeqScan() {
			return true
		}
		if node.PlanRows < t.SeqScanRowThreshold {
			return true
		}

		severity := analyzer.SeverityWarning
		if node.PlanRows >= 100000 {
			severity = analyzer.SeverityCritical
		}

		problem := fmt.Sprintf(
			"对 %s 的 %s 预计读取约 %s 行",
			node.QualifiedName(), node.NodeType, formatRows(node.PlanRows),
		)
		if node.Filter != "" {
			problem += fmt.Sprintf("；过滤条件 %q 会丢弃其中大部分行", node.Filter)
		}

		rec := "在 WHERE 条件涉及的列上建索引，避免全表扫描"
		out = append(out, analyzer.Finding{
			Severity:       severity,
			Rule:           "SeqScanLargeTable",
			NodeLabel:      node.Label(),
			NodePath:       joinPath(path),
			NodeType:       node.NodeType,
			RelationName:   node.QualifiedName(),
			Problem:        problem,
			Recommendation: rec,
			Evidence:       indexEvidence(node.QualifiedName(), node.Alias, node.Filter, node.IndexCond, node.PlanRows),
		})
		return true
	})

	return out
}
