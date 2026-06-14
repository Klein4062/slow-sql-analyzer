package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Klein4062/slow-sql-analyzer/internal/advise"
	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
	"github.com/Klein4062/slow-sql-analyzer/internal/rules"
)

func loadFixture(t *testing.T, name string) *plan.PlanResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	r, err := plan.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func buildModel(t *testing.T, name string) Model {
	t.Helper()
	result := loadFixture(t, name)
	a := analyzer.New(rules.Default())
	rep := a.Run(result, config.Default())
	return Model{
		Result:   result,
		Findings: rep.Findings,
		Actions:  advise.Actions(rep.Findings),
		NoColor:  true,
	}
}

func TestRenderTextSeqScanFixture(t *testing.T) {
	m := buildModel(t, "seqscan_large.json")
	out := RenderText(m)

	// Sanity-check key sections are present.
	for _, want := range []string{"Plan Analysis", "Seq Scan on public.orders", "SEQSCANLARGETABLE", "recommendation:"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q", want)
		}
	}
	if !strings.Contains(out, "CREATE INDEX") {
		t.Error("text output missing CREATE INDEX action")
	}
}

func TestRenderJSONRoundTrips(t *testing.T) {
	m := buildModel(t, "cardinality_misestimate.json")
	data, err := RenderJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`"findings"`, `"summary"`, `"CardinalityMisestimate"`, `"is_analyze": true`} {
		if !strings.Contains(s, want) {
			t.Errorf("json output missing %q\n%s", want, s)
		}
	}
}
