package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// InefficientFilter flags scans that read many rows and then throw most of them
// away in a filter, without using an index. When 90%+ of scanned rows are
// rejected, an index on the filter column would avoid reading them at all.
//
// 触发条件：扫描节点未走索引，且「被过滤丢弃的行 / (丢弃+保留)」>= 阈值（默认 0.9），
// 同时总扫描行数 >= 最低门槛（默认 100，避免小表噪声）。需要 ANALYZE。
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
			Severity:     severity,
			Rule:         "InefficientFilter",
			NodeLabel:    node.Label(),
			NodePath:     joinPath(path),
			NodeType:     node.NodeType,
			RelationName: node.QualifiedName(),
			Problem: fmt.Sprintf(
				"%s 扫描了 %s 行，但过滤条件 %q 丢弃了其中 %s",
				node.NodeType, formatRows(scanned), node.Filter, formatPct(ratio),
			),
			Recommendation: fmt.Sprintf(
				"在 %s 上为 %q 建索引，使 PostgreSQL 只读取匹配的行",
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
