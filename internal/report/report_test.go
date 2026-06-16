package report

import (
	"bytes"
	"encoding/json"
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

// TestJSONActionsArraysNeverNull ensures empty action groups serialize as []
// (not null), giving API consumers a stable contract.
func TestJSONActionsArraysNeverNull(t *testing.T) {
	// Model with no findings -> no actions.
	data, err := RenderJSON(Model{
		Result:   loadFixture(t, "seqscan_large.json"),
		Findings: []analyzer.Finding{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Actions struct {
			Indexes []string `json:"indexes"`
			Analyze []string `json:"analyze"`
			Config  []string `json:"config"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Actions.Indexes == nil || got.Actions.Analyze == nil || got.Actions.Config == nil {
		t.Errorf("action arrays must be [] not null: %+v", got.Actions)
	}
}

// TestExampleReportIsUpToDate asserts the checked-in examples/sample-report.json
// matches a fresh render from its source fixture — so the documented example
// never silently drifts from real output. If this fails, regenerate:
//
//	slow-sql-analyzer plan -f testdata/disk_sort_and_hash.json --format json > examples/sample-report.json
func TestExampleReportIsUpToDate(t *testing.T) {
	examplePath := filepath.Join("..", "..", "examples", "sample-report.json")
	want, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	// Re-render from the fixture via the same pipeline the CLI uses.
	result := loadFixture(t, "disk_sort_and_hash.json")
	a := analyzer.New(rules.Default())
	rep := a.Run(result, config.Default())
	got, err := RenderJSON(Model{
		Result:   result,
		Findings: rep.Findings,
		Actions:  advise.Actions(rep.Findings),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Errorf("examples/sample-report.json is stale. Regenerate with:\n" +
			"  go run ./cmd/slow-sql-analyzer plan -f testdata/disk_sort_and_hash.json --format json > examples/sample-report.json")
	}
}
