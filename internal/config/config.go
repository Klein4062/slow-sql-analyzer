// Package config holds tunable thresholds and runtime options for the analyzer.
package config

// Thresholds are the numeric cut-offs used by analysis rules. They carry
// documented defaults so the tool works out of the box; each is overridable
// via CLI flags.
//
// 各规则的数值阈值，都带开箱即用的默认值，并可通过 CLI flag（如 --seqscan-rows）
// 覆盖。DefaultThresholds() 集中维护默认值。
type Thresholds struct {
	// SeqScanRowThreshold: a Seq Scan whose estimated row count meets this is
	// flagged as a large-table scan (potential missing index).
	SeqScanRowThreshold float64

	// CardinalityRatio: when actual rows differ from the planner estimate by
	// more than this factor (in either direction), flag a misestimate.
	CardinalityRatio float64

	// CardinalityMinActual: ignore misestimates when actual rows are below
	// this (small row counts produce noisy ratios).
	CardinalityMinActual float64

	// FilterRemovalRatio: a scan whose filter discards at least this fraction
	// of rows (and uses no index) is flagged as inefficient.
	FilterRemovalRatio float64

	// FilterMinScanned: ignore filter checks when total scanned rows (kept +
	// removed) are below this.
	FilterMinScanned float64

	// BufferHitRatioMin: shared-block cache hit ratio below this is flagged.
	BufferHitRatioMin float64

	// BufferMinBlocks: ignore buffer checks when shared blocks touched are
	// below this (small scans are uninteresting).
	BufferMinBlocks float64

	// HotspotTimeFraction: a node whose self time meets this fraction of total
	// execution time is flagged as a hotspot.
	HotspotTimeFraction float64

	// NestedLoopMinLoops: a Nested Loop whose inner side is rescanned this many
	// times (and the inner is an expensive scan) is flagged.
	NestedLoopMinLoops float64
}

// DefaultThresholds returns the built-in defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{
		SeqScanRowThreshold:  1000,
		CardinalityRatio:     10,
		CardinalityMinActual: 10,
		FilterRemovalRatio:   0.9,
		FilterMinScanned:     100,
		BufferHitRatioMin:    0.9,
		BufferMinBlocks:      128,
		HotspotTimeFraction:  0.5,
		NestedLoopMinLoops:   10,
	}
}

// Options holds non-threshold runtime options.
type Options struct {
	// NoColor disables ANSI color in text reports.
	NoColor bool
	// DisabledRules maps rule names that should be skipped to true.
	DisabledRules map[string]bool
}

// Config bundles thresholds and options for a run.
type Config struct {
	Thresholds Thresholds
	Options    Options
}

// Default returns a Config with default thresholds and all rules enabled.
func Default() Config {
	return Config{
		Thresholds: DefaultThresholds(),
		Options: Options{
			DisabledRules: map[string]bool{},
		},
	}
}

// IsRuleEnabled reports whether the named rule should run.
func (c *Config) IsRuleEnabled(name string) bool {
	return !c.Options.DisabledRules[name]
}
