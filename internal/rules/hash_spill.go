package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// HashSpill flags hash tables (Hash nodes and Hashed aggregates) that did not
// fit in memory and split into multiple batches, writing to disk. Raising
// work_mem usually lets the hash fit in a single batch.
type HashSpill struct{}

// Name implements analyzer.Rule.
func (HashSpill) Name() string { return "HashSpill" }

// Analyze implements analyzer.Rule.
func (HashSpill) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	var out []analyzer.Finding

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		isHashNode := node.NodeType == "Hash"
		isHashAgg := node.NodeType == "Aggregate" && node.Strategy == "Hashed"
		if !isHashNode && !isHashAgg {
			return true
		}
		if node.HashBatches <= 1 {
			return true
		}

		severity := analyzer.SeverityWarning
		if node.HashBatches >= 8 {
			severity = analyzer.SeverityCritical
		}

		kind := "hash join probe"
		if isHashAgg {
			kind = "hash aggregate"
		}
		rec := "raise work_mem so the hash table fits in a single batch (one pass) instead of spilling"
		if isHashAgg {
			rec = "raise work_mem; if the aggregate groups are very numerous, also consider rewriting to a grouped aggregate or increasing maintenance_work_mem"
		}

		out = append(out, analyzer.Finding{
			Severity:       severity,
			Rule:           "HashSpill",
			NodeLabel:      node.Label(),
			NodePath:       joinPath(path),
			NodeType:       node.NodeType,
			Problem: fmt.Sprintf(
				"%s used %d batches (original %d) — it spilled to disk",
				kind, node.HashBatches, node.OriginalHashBatches,
			),
			Recommendation: rec,
			Evidence: map[string]any{
				"hash_batches":         node.HashBatches,
				"original_hash_batches": node.OriginalHashBatches,
				"peak_memory_kb":       node.PeakMemory,
			},
		})
		return true
	})

	return out
}
