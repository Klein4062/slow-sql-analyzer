package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// DiskSort flags Sort nodes that spilled to disk (external merge sort). Disk
// sorts are slow and usually indicate either too-low work_mem or a missing
// index that could deliver rows pre-sorted.
//
// 触发条件：Sort 节点的 Sort Space Type 为 "Disk"（外部归并排序落盘）。
type DiskSort struct{}

// Name implements analyzer.Rule.
func (DiskSort) Name() string { return "DiskSort" }

// Analyze implements analyzer.Rule.
func (DiskSort) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	var out []analyzer.Finding

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		if !node.IsSort() {
			return true
		}
		// Spilled to disk? PostgreSQL sets Sort Space Type to "Disk".
		if node.SortSpaceType != "Disk" || node.SortSpaceUsed <= 0 {
			return true
		}

		severity := analyzer.SeverityWarning
		if node.SortSpaceUsed >= 1024*10 { // >= 10 MB on disk
			severity = analyzer.SeverityCritical
		}

		rec := "调大 work_mem，让该排序放进内存"
		if len(node.SortKey) > 0 {
			rec = fmt.Sprintf(
				"调大 work_mem 让排序留在内存；或按 (%s) 建索引，使 PostgreSQL 按序读取、跳过排序",
				joinCols(node.SortKey),
			)
		}

		out = append(out, analyzer.Finding{
			Severity:  severity,
			Rule:      "DiskSort",
			NodeLabel: node.Label(),
			NodePath:  joinPath(path),
			NodeType:  node.NodeType,
			Problem: fmt.Sprintf(
				"排序使用 %q 溢出到磁盘 %s——内存排序会快得多",
				node.SortMethod, formatBytes(node.SortSpaceUsed),
			),
			Recommendation: rec,
			Evidence: map[string]any{
				"sort_method":   node.SortMethod,
				"sort_space_kb": node.SortSpaceUsed,
				"sort_key":      node.SortKey,
			},
		})
		return true
	})

	return out
}

// joinCols renders a column list for index advice, e.g. "a, b, c".
func joinCols(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ", "
		}
		out += stripParenInfo(c)
	}
	return out
}

// stripParenInfo drops a trailing "(...)" ordering/expr hint a Sort Key may
// carry (e.g. "id DESC NULLS LAST" → keep "id"; we only want the column name
// for a rough index hint).
func stripParenInfo(c string) string {
	for i, r := range c {
		if r == ' ' || r == '(' {
			return c[:i]
		}
	}
	return c
}
