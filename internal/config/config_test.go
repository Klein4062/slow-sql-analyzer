package config

import "testing"

func TestDefaultThresholds(t *testing.T) {
	d := DefaultThresholds()
	// Spot-check the documented defaults; if these change, update README/CLI flags too.
	cases := map[string]float64{
		"SeqScanRowThreshold":  d.SeqScanRowThreshold,
		"CardinalityRatio":     d.CardinalityRatio,
		"CardinalityMinActual": d.CardinalityMinActual,
		"FilterRemovalRatio":   d.FilterRemovalRatio,
		"FilterMinScanned":     d.FilterMinScanned,
		"BufferHitRatioMin":    d.BufferHitRatioMin,
		"BufferMinBlocks":      d.BufferMinBlocks,
		"HotspotTimeFraction":  d.HotspotTimeFraction,
		"NestedLoopMinLoops":   d.NestedLoopMinLoops,
		"StaleModRatio":        d.StaleModRatio,
		"StaleMinMods":         d.StaleMinMods,
	}
	want := map[string]float64{
		"SeqScanRowThreshold": 1000, "CardinalityRatio": 10, "CardinalityMinActual": 10,
		"FilterRemovalRatio": 0.9, "FilterMinScanned": 100, "BufferHitRatioMin": 0.9,
		"BufferMinBlocks": 128, "HotspotTimeFraction": 0.5, "NestedLoopMinLoops": 10,
		"StaleModRatio": 0.1, "StaleMinMods": 1000,
	}
	for k, got := range cases {
		if got != want[k] {
			t.Errorf("%s = %v, want %v", k, got, want[k])
		}
	}
}

func TestDefaultAndIsRuleEnabled(t *testing.T) {
	c := Default()
	if !c.IsRuleEnabled("SeqScanLargeTable") {
		t.Error("all rules should be enabled by default")
	}
	c.Options.DisabledRules["SeqScanLargeTable"] = true
	if c.IsRuleEnabled("SeqScanLargeTable") {
		t.Error("disabled rule should report false")
	}
	if c.IsRuleEnabled("NoSuchRule") {
		// unknown rules are not in the map -> enabled (not disabled). That's fine.
	}
	// Thresholds come from DefaultThresholds.
	if c.Thresholds.SeqScanRowThreshold != 1000 {
		t.Errorf("Default should carry DefaultThresholds; got %v", c.Thresholds.SeqScanRowThreshold)
	}
}
