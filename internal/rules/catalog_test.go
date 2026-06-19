package rules

import "testing"

func TestCatalogCoversAllRules(t *testing.T) {
	names := map[string]bool{}
	for _, r := range Catalog() {
		names[r.Name] = true
	}
	for _, want := range []string{
		"SeqScanLargeTable", "CardinalityMisestimate", "DiskSort", "HashSpill",
		"NestedLoopExpensiveInner", "InefficientFilter", "LowBufferHitRatio",
		"Hotspot", "StaleStatistics",
	} {
		if !names[want] {
			t.Errorf("catalog missing rule %q", want)
		}
	}
}

func TestCatalogCategories(t *testing.T) {
	cats := map[string]int{}
	for _, r := range Catalog() {
		switch r.Category {
		case CategoryCommon, CategoryLive, CategoryOffline:
			cats[r.Category]++
		default:
			t.Errorf("unknown category %q for %s", r.Category, r.Name)
		}
	}
	for _, want := range []string{CategoryCommon, CategoryLive, CategoryOffline} {
		if cats[want] == 0 {
			t.Errorf("category %q empty", want)
		}
	}
}
