package advise

import (
	"reflect"
	"testing"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
)

func TestExtractColumnsFor(t *testing.T) {
	cases := []struct {
		name  string
		cond  string
		alias string
		want  []string
	}{
		{"cast and literal", "((status)::text = 'pending'::text)", "orders", []string{"status"}},
		{"join keeps inner", "(user_id = u.id)", "o", []string{"user_id"}},
		{"and chain", "((a = 1) AND (b = 2) AND (c > 5))", "t", []string{"a", "b", "c"}},
		{"function skipped", "lower(name) = 'x'", "t", []string{"name"}},
		{"empty", "", "t", nil},
		{"keywords skipped", "(active = true AND deleted_at IS NULL)", "t", []string{"active", "deleted_at"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractColumnsFor(c.cond, c.alias)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ExtractColumnsFor(%q,%q) = %v, want %v", c.cond, c.alias, got, c.want)
			}
		})
	}
}

func TestIndexSuggestionsDedupAcrossFindings(t *testing.T) {
	findings := []analyzer.Finding{
		{Rule: "SeqScanLargeTable", RelationName: "public.orders",
			Evidence: map[string]any{"index_relation": "public.orders", "index_columns": []string{"status"}}},
		{Rule: "InefficientFilter", RelationName: "public.orders",
			Evidence: map[string]any{"index_relation": "public.orders", "index_columns": []string{"status", "created_at"}}},
		{Rule: "SeqScanLargeTable", RelationName: "public.users",
			Evidence: map[string]any{"index_relation": "public.users", "index_columns": []string{"email"}}},
	}
	got := IndexSuggestions(findings)
	if len(got) != 2 {
		t.Fatalf("want 2 suggestions, got %d: %+v", len(got), got)
	}
	// orders should merge columns in first-seen order without duplicates.
	var orders *IndexSuggestion
	for i := range got {
		if got[i].Relation == "public.orders" {
			orders = &got[i]
		}
	}
	if orders == nil || !reflect.DeepEqual(orders.Columns, []string{"status", "created_at"}) {
		t.Errorf("orders columns = %v, want [status created_at]", orders)
	}
}

func TestActionsIncludesIndexAnalyzeAndWorkMem(t *testing.T) {
	findings := []analyzer.Finding{
		{Rule: "CardinalityMisestimate", RelationName: "public.orders",
			Evidence: map[string]any{"index_relation": "public.orders", "index_columns": []string{"status"}}},
		{Rule: "DiskSort", Evidence: map[string]any{"sort_space_kb": 20480}},
	}
	actions := Actions(findings)

	kinds := map[ActionKind]bool{}
	for _, a := range actions {
		kinds[a.Kind] = true
	}
	if !kinds[ActionIndex] {
		t.Error("missing index action")
	}
	if !kinds[ActionAnalyze] {
		t.Error("missing analyze action")
	}
	if !kinds[ActionConfig] {
		t.Error("missing config (work_mem) action")
	}
}
