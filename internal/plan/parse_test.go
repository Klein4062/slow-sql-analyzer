package plan

import (
	"os"
	"path/filepath"
	"testing"
)

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseDetectsAnalyzeFlag(t *testing.T) {
	res, err := Parse(mustRead(t, "cardinality_misestimate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsAnalyze {
		t.Error("expected IsAnalyze=true for a plan with Actual Rows")
	}
	if res.Root.NodeType != "Nested Loop" {
		t.Errorf("root node type = %q, want Nested Loop", res.Root.NodeType)
	}
}

func TestParseRejectsNonArray(t *testing.T) {
	if _, err := Parse([]byte(`{"Plan": {}}`)); err == nil {
		t.Error("expected error for non-array input")
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse([]byte(`[]`)); err == nil {
		t.Error("expected error for empty array")
	}
}

func TestHasDistinguishesAbsentFromZero(t *testing.T) {
	// A node that legitimately has zero Actual Rows must still report HasActual.
	res, err := Parse([]byte(`[
      {"Plan": {"Node Type":"Seq Scan","Actual Rows":0,"Actual Loops":1},
       "Execution Time": 0.1}
    ]`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsAnalyze {
		t.Error("IsAnalyze should be true when 'Actual Rows' key is present (even if 0)")
	}
	if !res.Root.HasActual() {
		t.Error("HasActual should be true when 'Actual Rows' key is present")
	}
}

func TestWalkVisitsAllNodes(t *testing.T) {
	res, err := Parse(mustRead(t, "disk_sort_and_hash.json"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	ForEach(res.Root, func(node, parent *PlanNode, depth int) { count++ })
	// GroupAggregate > Sort > HashJoin > (SeqScan, Hash > SeqScan) = 6 nodes
	if count != 6 {
		t.Errorf("visited %d nodes, want 6", count)
	}
}
