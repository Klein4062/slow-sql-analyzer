package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

func load(t *testing.T, name string) *plan.PlanResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	res, err := plan.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func hasFinding(fs []analyzer.Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestDiskSortFlagsExternalMerge(t *testing.T) {
	ctx := analyzer.NewContext(load(t, "disk_sort_and_hash.json"), config.DefaultThresholds())
	fs := DiskSort{}.Analyze(ctx)
	if !hasFinding(fs, "DiskSort") {
		t.Fatalf("DiskSort did not fire; got %+v", fs)
	}
	if fs[0].Evidence["sort_space_kb"] != 95000 {
		t.Errorf("sort_space_kb = %v, want 95000", fs[0].Evidence["sort_space_kb"])
	}
}

func TestHashSpillFlagsMultipleBatches(t *testing.T) {
	ctx := analyzer.NewContext(load(t, "disk_sort_and_hash.json"), config.DefaultThresholds())
	fs := HashSpill{}.Analyze(ctx)
	if !hasFinding(fs, "HashSpill") {
		t.Fatalf("HashSpill did not fire; got %+v", fs)
	}
	if fs[0].Evidence["hash_batches"].(int) != 4 {
		t.Errorf("hash_batches = %v, want 4", fs[0].Evidence["hash_batches"])
	}
}

func TestNestedLoopNeedsAnalyze(t *testing.T) {
	// Same fixture has no problematic nested loop; ensure no crash and no finding.
	ctx := analyzer.NewContext(load(t, "disk_sort_and_hash.json"), config.DefaultThresholds())
	fs := NestedLoopExpensiveInner{}.Analyze(ctx)
	if len(fs) != 0 {
		t.Errorf("expected 0 nested-loop findings, got %d", len(fs))
	}
}

func TestCardinalitySkipsWithoutAnalyze(t *testing.T) {
	// Build an estimate-only plan (no Actual Rows keys).
	res, err := plan.Parse([]byte(`[
      {"Plan":{"Node Type":"Seq Scan","Plan Rows":1000},
       "Execution Time": 0.0}
    ]`))
	if err != nil {
		t.Fatal(err)
	}
	ctx := analyzer.NewContext(res, config.DefaultThresholds())
	got := (CardinalityMisestimate{}).Analyze(ctx)
	if len(got) != 0 {
		t.Errorf("expected 0 findings on estimate-only plan, got %d", len(got))
	}
}
