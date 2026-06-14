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
	if nested.Evidence["direction"] != "under-estimated" {
		t.Errorf("want under-estimated, got %v", nested.Evidence["direction"])
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
