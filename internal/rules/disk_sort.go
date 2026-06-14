package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// DiskSort flags Sort nodes that spilled to disk (external merge sort). Disk
// sorts are slow and usually indicate either too-low work_mem or a missing
// index that could deliver rows pre-sorted.
type DiskSort struct{}

// Name implements analyzer.Rule.
func (DiskSort) Name() string { return "DiskSort" }

// Analyze implements analyzer.Rule.
func (DiskSort) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	var out []analyzer.Finding

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		if node.NodeType != "Sort" {
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

		rec := "raise work_mem so the sort fits in memory"
		if len(node.SortKey) > 0 {
			rec = fmt.Sprintf(
				"raise work_mem to keep this sort in memory, or add an index on (%s) "+
					"so PostgreSQL can read rows pre-sorted and skip the sort",
				joinCols(node.SortKey),
			)
		}

		out = append(out, analyzer.Finding{
			Severity:       severity,
			Rule:           "DiskSort",
			NodeLabel:      node.Label(),
			NodePath:       joinPath(path),
			NodeType:       node.NodeType,
			Problem: fmt.Sprintf(
				"sort spilled %s to disk using %q — in-memory sort would be much faster",
				formatBytes(node.SortSpaceUsed), node.SortMethod,
			),
			Recommendation: rec,
			Evidence: map[string]any{
				"sort_method":    node.SortMethod,
				"sort_space_kb":  node.SortSpaceUsed,
				"sort_key":       node.SortKey,
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
