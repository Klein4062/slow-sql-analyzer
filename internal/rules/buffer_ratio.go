package rules

import (
	"fmt"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// LowBufferHitRatio flags nodes that read a lot from disk (cache misses) rather
// than from the shared buffer cache. A low hit ratio on big scans suggests the
// working set does not fit in shared_buffers.
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
			Severity:       severity,
			Rule:           "LowBufferHitRatio",
			NodeLabel:      node.Label(),
			NodePath:       joinPath(path),
			NodeType:       node.NodeType,
			Problem: fmt.Sprintf(
				"%s read %s blocks but only %s were cache hits (%s hit ratio)",
				node.Label(),
				formatRows(total), formatRows(node.SharedHitBlocks), formatPct(ratio),
			),
			Recommendation: "increase shared_buffers, or run the query again after warm-up; " +
				"if this table is hot, ensure it fits in memory",
			Evidence: map[string]any{
				"shared_hit":   node.SharedHitBlocks,
				"shared_read":  node.SharedReadBlocks,
				"hit_ratio":    ratio,
			},
		})
		return true
	})

	return out
}
