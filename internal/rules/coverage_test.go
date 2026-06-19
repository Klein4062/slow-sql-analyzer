package rules

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// --- 规则正向触发（用既有 fixture）---

func TestSeqScanLargeTableFires(t *testing.T) {
	ctx := analyzer.NewContext(load(t, "seqscan_large.json"), config.DefaultThresholds())
	fs := SeqScanLargeTable{}.Analyze(ctx)
	if len(fs) != 1 || fs[0].Rule != "SeqScanLargeTable" {
		t.Fatalf("want 1 SeqScanLargeTable, got %+v", fs)
	}
	// severity critical at 1M rows, index evidence present
	if fs[0].Evidence["index_columns"] == nil {
		t.Error("expected index_columns evidence")
	}
}

func TestInefficientFilterFires(t *testing.T) {
	ctx := analyzer.NewContext(load(t, "seqscan_large.json"), config.DefaultThresholds())
	fs := InefficientFilter{}.Analyze(ctx)
	if len(fs) != 1 {
		t.Fatalf("want 1 InefficientFilter, got %d", len(fs))
	}
	if fs[0].Evidence["removal_ratio"].(float64) < 0.9 {
		t.Errorf("removal ratio should be >=0.9, got %v", fs[0].Evidence["removal_ratio"])
	}
}

func TestLowBufferHitRatioFires(t *testing.T) {
	ctx := analyzer.NewContext(load(t, "seqscan_large.json"), config.DefaultThresholds())
	fs := LowBufferHitRatio{}.Analyze(ctx)
	if len(fs) != 1 {
		t.Fatalf("want 1 LowBufferHitRatio, got %d", len(fs))
	}
}

func TestHotspotFires(t *testing.T) {
	ctx := analyzer.NewContext(load(t, "seqscan_large.json"), config.DefaultThresholds())
	fs := Hotspot{}.Analyze(ctx)
	if len(fs) != 1 {
		t.Fatalf("want 1 Hotspot, got %d", len(fs))
	}
}

func TestCardinalityMisestimateFires(t *testing.T) {
	ctx := analyzer.NewContext(load(t, "cardinality_misestimate.json"), config.DefaultThresholds())
	fs := CardinalityMisestimate{}.Analyze(ctx)
	if len(fs) < 1 {
		t.Fatalf("want >=1 CardinalityMisestimate, got %d", len(fs))
	}
}

func TestDiskSortNoSpillSkips(t *testing.T) {
	// in-memory sort (no Disk) -> no finding.
	res, _ := plan.Parse([]byte(`[{"Plan":{"Node Type":"Sort","Sort Space Type":"Memory","Sort Method":"quicksort","Plan Rows":10,"Actual Rows":10,"Actual Loops":1}}]`))
	ctx := analyzer.NewContext(res, config.DefaultThresholds())
	if fs := (DiskSort{}).Analyze(ctx); len(fs) != 0 {
		t.Errorf("in-memory sort -> 0 findings, got %d", len(fs))
	}
}

// --- helpers ---

func TestFormatHelpers(t *testing.T) {
	if formatRows(1500) != "1.5k" {
		t.Errorf("formatRows(1500)=%q", formatRows(1500))
	}
	if formatRows(2500000) != "2.5M" {
		t.Errorf("formatRows(2.5M)=%q", formatRows(2500000))
	}
	if formatPct(0.5) != "50%" {
		t.Errorf("formatPct(0.5)=%q", formatPct(0.5))
	}
	if formatBytes(2048) != "2.0 MB" {
		t.Errorf("formatBytes(2048)=%q", formatBytes(2048))
	}
	if formatBytes(500) != "500 KB" {
		t.Errorf("formatBytes(500)=%q", formatBytes(500))
	}
}

func TestJoinColsAndStripParenInfo(t *testing.T) {
	if got := joinCols([]string{"a", "b"}); got != "a, b" {
		t.Errorf("joinCols = %q", got)
	}
	// stripParenInfo trims at first space/paren
	if got := stripParenInfo("col DESC NULLS LAST"); got != "col" {
		t.Errorf("stripParenInfo = %q", got)
	}
	if got := stripParenInfo("col"); got != "col" {
		t.Errorf("stripParenInfo = %q", got)
	}
}

func TestChildIndexRelationship(t *testing.T) {
	outer := &plan.PlanNode{NodeType: "Seq Scan", ParentRelationship: "Outer"}
	inner := &plan.PlanNode{NodeType: "Index Scan", ParentRelationship: "Inner"}
	node := &plan.PlanNode{NodeType: "Nested Loop", Plans: []*plan.PlanNode{outer, inner}}
	if childByRelationship(node, "Inner") != inner {
		t.Error("should find Inner child")
	}
	if childByRelationship(node, "Outer") != outer {
		t.Error("should find Outer child")
	}
	if childByRelationship(node, "Subquery") != nil {
		t.Error("missing relationship -> nil")
	}
}

func TestIndexEvidenceAndMerge(t *testing.T) {
	ev := indexEvidence("public.t", "t", "status = 1", "", 1000)
	if ev["index_relation"] != "public.t" || ev["index_columns"] == nil {
		t.Errorf("indexEvidence missing keys: %+v", ev)
	}
	merged := mergeEvidence(ev, map[string]any{"extra": 1})
	if merged["extra"] != 1 || merged["index_relation"] != "public.t" {
		t.Errorf("mergeEvidence lost keys: %+v", merged)
	}
}

func TestCardinalityRatioAndRelationOrNode(t *testing.T) {
	// below threshold -> not assessable (parsed so HasActual is set)
	if _, ok := cardinalityRatio(planNodeWithActual(100, 100), config.DefaultThresholds()); ok {
		t.Error("ratio 1.0 (<10) should not be assessable")
	}
	// above threshold
	big := planNodeWithActual(1, 100000)
	r, ok := cardinalityRatio(big, config.DefaultThresholds())
	if !ok || r < 10 {
		t.Errorf("100000x should be assessable ratio>=10, got %v ok=%v", r, ok)
	}
	if relationOrNode(&plan.PlanNode{RelationName: "t", Schema: "s"}) != "s.t" {
		t.Error("relationOrNode with relation -> qualified")
	}
	if relationOrNode(&plan.PlanNode{NodeType: "Hash Join"}) != "Hash Join" {
		t.Error("relationOrNode without relation -> node type")
	}
}

// planNodeWithActual builds a node parsed from JSON so HasActual (present map) is set.
func planNodeWithActual(estRows, actRows float64) *plan.PlanNode {
	js := fmt.Sprintf(
		`[{"Plan":{"Node Type":"Seq Scan","Plan Rows":%v,"Actual Rows":%v,"Actual Loops":1},"Execution Time":1.0}]`,
		estRows, actRows,
	)
	res, _ := plan.Parse([]byte(js))
	return res.Root
}

func TestAssessStaleAndDescribe(t *testing.T) {
	th := config.DefaultThresholds()
	// never analyzed with rows -> stale
	ok, ratio, reason := assessStale(plan.TableStat{Relation: "t", LiveTuples: 100}, th)
	if !ok || reason != "never" {
		t.Errorf("never analyzed: ok=%v reason=%q", ok, reason)
	}
	_ = ratio
	// stale by ratio
	ok, ratio, reason = assessStale(plan.TableStat{Relation: "t", LiveTuples: 1000000, ModSinceAnalyze: 200000, LastAutoAnalyze: time.Now()}, th)
	if !ok || !strings.Contains(reason, "%") {
		t.Errorf("20%% mod should be stale with %% reason, got ok=%v reason=%q", ok, reason)
	}
	// fresh
	ok, _, _ = assessStale(plan.TableStat{Relation: "t", LiveTuples: 1000000, ModSinceAnalyze: 10, LastAutoAnalyze: time.Now()}, th)
	if ok {
		t.Error("fresh table should not be stale")
	}
	// describeProblem both branches
	if p := describeProblem(plan.TableStat{Schema: "p", Relation: "t", LiveTuples: 5}, 1, "never"); !strings.Contains(p, "从未") {
		t.Errorf("never describe: %q", p)
	}
	if p := describeProblem(plan.TableStat{Schema: "p", Relation: "t", LiveTuples: 1000, ModSinceAnalyze: 200}, 0.2, "20% changed"); !strings.Contains(p, "过时") {
		t.Errorf("stale describe: %q", p)
	}
}

func TestZeroTimeStr(t *testing.T) {
	if zeroTimeStr(time.Time{}) != "never" {
		t.Error("zero time -> never")
	}
	if got := zeroTimeStr(time.Now()); got == "never" || got == "" {
		t.Errorf("non-zero time -> RFC3339, got %q", got)
	}
}

func TestScanMisestimateByRelation(t *testing.T) {
	root := &plan.PlanNode{NodeType: "Aggregate", Plans: []*plan.PlanNode{
		// scan with huge misestimate on relation "a"
		planNodeWithActual(1, 1000000),
	}}
	root.Plans[0].NodeType = "Seq Scan"
	root.Plans[0].RelationName = "a"
	m := scanMisestimateByRelation(root, config.DefaultThresholds())
	if len(m) != 1 || m["a"].Ratio < 10 {
		t.Errorf("expected misestimate for a, got %+v", m)
	}
}
