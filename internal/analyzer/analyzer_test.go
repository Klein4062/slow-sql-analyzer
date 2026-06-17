package analyzer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
	"github.com/Klein4062/slow-sql-analyzer/internal/rules"
)

// fixture loads a plan file from the repo-root testdata directory.
func fixture(t *testing.T, name string) *plan.PlanResult {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	res, err := plan.Parse(data)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return res
}

func findingsByRule(report analyzer.Report) map[string][]analyzer.Finding {
	m := map[string][]analyzer.Finding{}
	for _, f := range report.Findings {
		m[f.Rule] = append(m[f.Rule], f)
	}
	return m
}

func TestSeqScanLargeTableFlagsLargeScan(t *testing.T) {
	a := analyzer.New([]analyzer.Rule{rules.SeqScanLargeTable{}})
	report := a.Run(fixture(t, "seqscan_large.json"), config.Default())

	ss := findingsByRule(report)["SeqScanLargeTable"]
	if len(ss) != 1 {
		t.Fatalf("want 1 SeqScanLargeTable finding, got %d", len(ss))
	}
	if ss[0].Severity != analyzer.SeverityCritical {
		t.Errorf("want critical severity for 1M-row seq scan, got %s", ss[0].Severity)
	}
	if ss[0].RelationName != "public.orders" {
		t.Errorf("unexpected relation: %s", ss[0].RelationName)
	}
}

func TestCardinalityDetectsUnderestimate(t *testing.T) {
	a := analyzer.New([]analyzer.Rule{rules.CardinalityMisestimate{}})
	report := a.Run(fixture(t, "cardinality_misestimate.json"), config.Default())

	cm := findingsByRule(report)["CardinalityMisestimate"]
	// The Nested Loop (1 vs 50000) and Seq Scan (1 vs 50000) both misestimate.
	if len(cm) < 2 {
		t.Fatalf("want >=2 CardinalityMisestimate findings, got %d (%+v)", len(cm), cm)
	}
	var nested *analyzer.Finding
	for i := range cm {
		if cm[i].NodeType == "Nested Loop" {
			nested = &cm[i]
		}
	}
	if nested == nil {
		t.Fatalf("no finding for Nested Loop node")
	}
	if nested.Evidence["direction"] != "低估" {
		t.Errorf("want 低估, got %v", nested.Evidence["direction"])
	}
}

func TestAnalyzeSortsBySeverity(t *testing.T) {
	a := analyzer.New(rules.Default())
	report := a.Run(fixture(t, "cardinality_misestimate.json"), config.Default())
	for i := 1; i < len(report.Findings); i++ {
		if report.Findings[i-1].Severity < report.Findings[i].Severity {
			t.Fatalf("findings not sorted by severity desc at index %d", i)
		}
	}
}

// TestOpenGaussRowStorePlan verifies the analyzer handles openGauss row-store
// EXPLAIN FORMAT JSON (PG-compatible structure) out of the box.
func TestOpenGaussRowStorePlan(t *testing.T) {
	a := analyzer.New(rules.Default())
	report := a.Run(fixture(t, "opengauss_rowstore.json"), config.Default())
	m := findingsByRule(report)
	for _, want := range []string{"SeqScanLargeTable", "InefficientFilter", "HashSpill"} {
		if len(m[want]) == 0 {
			t.Errorf("openGauss row-store plan: expected %s to fire, got rules %v", want, ruleNames(report))
		}
	}
}

// TestOpenGaussColumnarPlan verifies the analyzer recognizes openGauss columnar
// / vectorized nodes (CStore Scan, Vec Hash Join, VecAgg).
func TestOpenGaussColumnarPlan(t *testing.T) {
	a := analyzer.New(rules.Default())
	report := a.Run(fixture(t, "opengauss_columnar.json"), config.Default())
	m := findingsByRule(report)
	// InefficientFilter firing means the CStore Scan was recognized as a scan
	// with a filter — the core openGauss columnar recognition.
	if len(m["InefficientFilter"]) == 0 {
		t.Errorf("expected InefficientFilter on CStore Scan; got rules %v", ruleNames(report))
	}
	if len(m["SeqScanLargeTable"]) == 0 {
		t.Errorf("expected SeqScanLargeTable on CStore Scan; got rules %v", ruleNames(report))
	}
}

func ruleNames(r analyzer.Report) []string {
	out := make([]string, len(r.Findings))
	for i, f := range r.Findings {
		out[i] = f.Rule
	}
	return out
}
