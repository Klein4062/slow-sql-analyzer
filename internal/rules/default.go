package rules

import "github.com/Klein4062/slow-sql-analyzer/internal/analyzer"

// Default returns the full ordered rule set used by the CLI/API. Order matters
// only for stable output of same-severity findings.
func Default() []analyzer.Rule {
	return []analyzer.Rule{
		SeqScanLargeTable{},
		CardinalityMisestimate{},
		DiskSort{},
		HashSpill{},
		NestedLoopExpensiveInner{},
		InefficientFilter{},
		LowBufferHitRatio{},
		Hotspot{},
	}
}
