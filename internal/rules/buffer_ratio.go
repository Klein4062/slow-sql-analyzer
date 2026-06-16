package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// LowBufferHitRatio flags nodes that read a lot from disk (cache misses) rather
// than from the shared buffer cache. A low hit ratio on big scans suggests the
// working set does not fit in shared_buffers.
//
// 触发条件：节点共享缓冲命中率 Hit/(Hit+Read) 低于阈值（默认 0.9），
// 且总读取块数 >= 门槛（默认 128 块，忽略小扫描）。命中率低 → 数据未驻留内存。
type LowBufferHitRatio struct{}

// Name implements analyzer.Rule.
func (LowBufferHitRatio) Name() string { return "LowBufferHitRatio" }

// Analyze implements analyzer.Rule.
func (LowBufferHitRatio) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	var out []analyzer.Finding
	t := ctx.Thresholds

	plan.WalkPath(ctx.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		total := node.SharedHitBlocks + node.SharedReadBlocks
		if total < t.BufferMinBlocks {
			return true
		}
		ratio := node.SharedHitRatio()
		if ratio >= t.BufferHitRatioMin {
			return true
		}

		severity := analyzer.SeverityInfo
		if ratio < 0.5 {
			severity = analyzer.SeverityWarning
		}

		out = append(out, analyzer.Finding{
			Severity:  severity,
			Rule:      "LowBufferHitRatio",
			NodeLabel: node.Label(),
			NodePath:  joinPath(path),
			NodeType:  node.NodeType,
			Problem: fmt.Sprintf(
				"%s 读取了 %s 个块，但仅 %s 个命中缓存（命中率 %s）",
				node.Label(),
				formatRows(total), formatRows(node.SharedHitBlocks), formatPct(ratio),
			),
			Recommendation: "调大 shared_buffers，或预热后再跑；若该表是热点表，确保它能驻留内存",
			Evidence: map[string]any{
				"shared_hit":  node.SharedHitBlocks,
				"shared_read": node.SharedReadBlocks,
				"hit_ratio":   ratio,
			},
		})
		return true
	})

	return out
}
