package plan

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tv, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tv
}

func TestQualifiedNameAndLabel(t *testing.T) {
	cases := []struct {
		node        *PlanNode
		qual, label string
	}{
		{&PlanNode{NodeType: "Seq Scan", RelationName: "orders", Schema: "public"},
			"public.orders", "Seq Scan on public.orders"},
		{&PlanNode{NodeType: "Seq Scan", RelationName: "orders"}, // no schema
			"orders", "Seq Scan on orders"},
		{&PlanNode{NodeType: "Hash Join"}, // no relation
			"", "Hash Join"},
		{&PlanNode{NodeType: "Sort"}, "", "Sort"},
	}
	for _, c := range cases {
		if got := c.node.QualifiedName(); got != c.qual {
			t.Errorf("QualifiedName=%q want %q", got, c.qual)
		}
		if got := c.node.Label(); got != c.label {
			t.Errorf("Label=%q want %q", got, c.label)
		}
	}
}

func TestSharedHitRatio(t *testing.T) {
	if r := (&PlanNode{}).SharedHitRatio(); r != 1 {
		t.Errorf("no blocks -> ratio 1, got %v", r)
	}
	n := &PlanNode{SharedHitBlocks: 30, SharedReadBlocks: 70}
	if r := n.SharedHitRatio(); r != 0.3 {
		t.Errorf("30/(30+70) = 0.3, got %v", r)
	}
}

func TestActualRowsTotal(t *testing.T) {
	if got := (&PlanNode{ActualRows: 5}).ActualRowsTotal(); got != 5 {
		t.Errorf("loops=0 -> treat as 1; got %v", got)
	}
	if got := (&PlanNode{ActualRows: 5, ActualLoops: 4}).ActualRowsTotal(); got != 20 {
		t.Errorf("5*4=20, got %v", got)
	}
}

func TestHasOnNil(t *testing.T) {
	var n *PlanNode
	if n.Has("anything") || n.HasActual() {
		t.Error("nil node should report Has=false")
	}
}

func TestFindByTypeAndRelation(t *testing.T) {
	root := &PlanNode{NodeType: "Seq Scan", RelationName: "a", Plans: []*PlanNode{
		{NodeType: "Index Scan", RelationName: "b"},
		{NodeType: "Seq Scan", RelationName: "a"}, // dup relation
	}}
	if got := len(FindByType(root, "Seq Scan")); got != 2 {
		t.Errorf("FindByType Seq Scan = %d, want 2", got)
	}
	if got := len(FindByRelation(root, "a")); got != 2 {
		t.Errorf("FindRelation a = %d, want 2", got)
	}
	if got := len(FindByRelation(root, "missing")); got != 0 {
		t.Errorf("missing relation -> 0, got %d", got)
	}
}

func TestRelationsDedup(t *testing.T) {
	root := &PlanNode{NodeType: "Aggregate", Plans: []*PlanNode{
		{NodeType: "Seq Scan", RelationName: "orders", Schema: "public"},
		{NodeType: "Hash Join", Plans: []*PlanNode{
			{NodeType: "Seq Scan", RelationName: "orders"}, // dup, no schema
			{NodeType: "Index Scan", RelationName: "users", Schema: "public"},
		}},
	}}
	rels := (&PlanResult{Root: root}).Relations()
	// Dedup is by qualified name; "public.orders" and "orders" are distinct.
	want := []string{"public.orders", "orders", "public.users"}
	if len(rels) != len(want) {
		t.Fatalf("Relations = %v, want %v", rels, want)
	}
	for i, w := range want {
		if rels[i] != w {
			t.Errorf("Relations[%d] = %q, want %q", i, rels[i], w)
		}
	}
	// nil-safe
	if (&PlanResult{}).Relations() != nil {
		t.Error("nil root -> nil")
	}
}

func TestTableStatHelpers(t *testing.T) {
	never := TableStat{Schema: "public", Relation: "t"}
	if never.Analyzed() {
		t.Error("never analyzed should be false")
	}
	if never.QualifiedName() != "public.t" {
		t.Errorf("QualifiedName = %q", never.QualifiedName())
	}
	analyzed := TableStat{Relation: "x", LastAutoAnalyze: mustTime(t, "2026-01-01T00:00:00Z")}
	if !analyzed.Analyzed() {
		t.Error("has LastAutoAnalyze -> Analyzed true")
	}
	analyzed2 := TableStat{Relation: "x", LastAnalyze: mustTime(t, "2026-01-01T00:00:00Z")}
	if !analyzed2.Analyzed() {
		t.Error("has LastAnalyze -> Analyzed true")
	}
}
