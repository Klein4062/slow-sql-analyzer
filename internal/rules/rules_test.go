package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Klein4062/slow-sql-analyzer/internal/advise"
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

// statsResult builds a minimal PlanResult carrying per-table statistics.
func statsResult(stats map[string]plan.TableStat) *plan.PlanResult {
	return &plan.PlanResult{Root: &plan.PlanNode{NodeType: "Result"}, TableStats: stats}
}

func TestStaleStatisticsFlagsStaleAndNeverAnalyzed(t *testing.T) {
	now := time.Now()
	res := statsResult(map[string]plan.TableStat{
		"public.orders": {Schema: "public", Relation: "orders", LiveTuples: 1000000, ModSinceAnalyze: 200000, LastAutoAnalyze: now}, // 20% 修改 → 过时(警告)
		"public.fresh":  {Schema: "public", Relation: "fresh", LiveTuples: 1000000, ModSinceAnalyze: 100, LastAutoAnalyze: now},     // 新鲜 → 不报
		"public.never":  {Schema: "public", Relation: "never", LiveTuples: 50000},                                                   // 从未 ANALYZE → 严重
	})
	ctx := analyzer.NewContext(res, config.DefaultThresholds())
	fs := StaleStatistics{}.Analyze(ctx)

	rels := map[string]analyzer.Finding{}
	for _, f := range fs {
		rels[f.RelationName] = f
	}
	if len(fs) != 2 {
		t.Fatalf("want 2 stale findings, got %d: %+v", len(fs), fs)
	}
	if _, ok := rels["public.orders"]; !ok {
		t.Error("missing stale finding for public.orders")
	}
	if _, ok := rels["public.never"]; !ok {
		t.Error("missing finding for never-analyzed public.never")
	}
	if rels["public.never"].Severity != analyzer.SeverityCritical {
		t.Errorf("never-analyzed should be critical, got %s", rels["public.never"].Severity)
	}
	if _, ok := rels["public.fresh"]; ok {
		t.Error("fresh table should not be flagged")
	}
}

func TestStaleStatisticsEmptyTableStatsIsNoop(t *testing.T) {
	// 离线/命令模式无 TableStats → 规则应静默返回 nil。
	res := &plan.PlanResult{Root: &plan.PlanNode{NodeType: "Result"}}
	ctx := analyzer.NewContext(res, config.DefaultThresholds())
	fs := (StaleStatistics{}).Analyze(ctx)
	if len(fs) != 0 {
		t.Errorf("expected 0 findings without table stats, got %d", len(fs))
	}
}

func TestStaleStatisticsActionsProduceAnalyze(t *testing.T) {
	// StaleStatistics 的 finding 应被 advise 转成 ANALYZE 动作。
	now := time.Now()
	res := statsResult(map[string]plan.TableStat{
		"public.orders": {Schema: "public", Relation: "orders", LiveTuples: 1000000, ModSinceAnalyze: 200000, LastAutoAnalyze: now},
	})
	ctx := analyzer.NewContext(res, config.DefaultThresholds())
	findings := StaleStatistics{}.Analyze(ctx)
	actions := advise.Actions(findings)
	found := false
	for _, a := range actions {
		if a.Kind == advise.ActionAnalyze && strings.Contains(a.SQL, "ANALYZE public.orders") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ANALYZE public.orders action, got %+v", actions)
	}
}
