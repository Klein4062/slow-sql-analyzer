package plan

import (
	"os"
	"path/filepath"
	"testing"
)

// The user's exact example (psql default output: header + dashes + footer).
func TestParseTextUserExample(t *testing.T) {
	in := []byte(`
                                              QUERY PLAN
-------------------------------------------------------------------------------------------------------
 Seq Scan on public.t1  (cost=0.00..32.60 rows=2260 width=8) (actual time=0.018..0.019 rows=0 loops=1)
   Output: a, b
 Planning Time: 0.396 ms
 Execution Time: 0.064 ms
(4 行记录)
`)
	res, err := Parse(in) // auto-detect -> text
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsAnalyze {
		t.Error("expected IsAnalyze (actual block present)")
	}
	if res.ExecutionTime != 0.064 {
		t.Errorf("ExecutionTime = %v, want 0.064", res.ExecutionTime)
	}
	if res.PlanningTime != 0.396 {
		t.Errorf("PlanningTime = %v, want 0.396", res.PlanningTime)
	}
	if res.Root.NodeType != "Seq Scan" {
		t.Errorf("NodeType = %q, want Seq Scan", res.Root.NodeType)
	}
	if res.Root.RelationName != "t1" || res.Root.Schema != "public" {
		t.Errorf("relation = %s.%s, want public.t1", res.Root.Schema, res.Root.RelationName)
	}
	if res.Root.PlanRows != 2260 {
		t.Errorf("PlanRows = %v, want 2260", res.Root.PlanRows)
	}
	if !res.Root.HasActual() {
		t.Error("HasActual should be true")
	}
}

func TestParseTextMultiNode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "explain_text.txt"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsAnalyze {
		t.Error("expected IsAnalyze")
	}
	// Tree: Finalize Aggregate > Gather > Partial Aggregate > Hash Join > [Seq Scan, Hash > Seq Scan]
	if res.Root.NodeType != "Finalize Aggregate" {
		t.Errorf("root = %q", res.Root.NodeType)
	}
	// Find the Hash Join and its children via traversal.
	var seqScans []*PlanNode
	ForEach(res.Root, func(n, _ *PlanNode, _ int) {
		if n.NodeType == "Seq Scan" {
			seqScans = append(seqScans, n)
		}
	})
	if len(seqScans) != 2 {
		t.Fatalf("want 2 Seq Scan nodes (Parallel prefix stripped), got %d", len(seqScans))
	}
	for _, s := range seqScans {
		if s.RelationName != "orders" {
			t.Errorf("Seq Scan relation = %q, want orders", s.RelationName)
		}
		if !s.HasActual() {
			t.Error("Seq Scan should have actual stats")
		}
	}
	// The second Seq Scan (o1) carries the filter + rows-removed.
	var filtered *PlanNode
	for _, s := range seqScans {
		if s.Filter != "" {
			filtered = s
		}
	}
	if filtered == nil {
		t.Fatal("no Seq Scan with a Filter")
	}
	if filtered.RowsRemovedByFilter != 128728 {
		t.Errorf("RowsRemovedByFilter = %v, want 128728", filtered.RowsRemovedByFilter)
	}
	if !filtered.Has("Rows Removed by Filter") {
		t.Error("Has(Rows Removed by Filter) should be true")
	}
}

func TestParseTextError(t *testing.T) {
	if _, err := ParseText([]byte("not an explain plan at all\nno cost here")); err == nil {
		t.Error("expected error for non-plan text")
	}
}
