package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// HashSpill flags hash tables (Hash nodes and Hashed aggregates) that did not
// fit in memory and split into multiple batches, writing to disk. Raising
// work_mem usually lets the hash fit in a single batch.
//
// 触发条件：Hash 节点或 Hashed 聚合的 Hash Batches > 1（内存放不下，分批溢出磁盘）。
type HashSpill struct{}

// Name implements analyzer.Rule.
func (HashSpill) Name() string { return "HashSpill" }

// Analyze implements analyzer.Rule.
func (HashSpill) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	var out []analyzer.Finding

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		isHashNode := node.IsHashNode()
		isHashAgg := node.IsHashAggregate()
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

		kind := "Hash Join"
		if isHashAgg {
			kind = "Hash Aggregate"
		}
		rec := "调大 work_mem，使哈希表单批次（一趟）完成，避免溢出"
		if isHashAgg {
			rec = "调大 work_mem；若分组数极多，可考虑改写为分组聚合或调大 maintenance_work_mem"
		}

		out = append(out, analyzer.Finding{
			Severity:  severity,
			Rule:      "HashSpill",
			NodeLabel: node.Label(),
			NodePath:  joinPath(path),
			NodeType:  node.NodeType,
			Problem: fmt.Sprintf(
				"%s 使用了 %d 个批次（原始 %d）——已溢出到磁盘",
				kind, node.HashBatches, node.OriginalHashBatches,
			),
			Recommendation: rec,
			Evidence: map[string]any{
				"hash_batches":          node.HashBatches,
				"original_hash_batches": node.OriginalHashBatches,
				"peak_memory_kb":        node.PeakMemory,
			},
		})
		return true
	})

	return out
}
