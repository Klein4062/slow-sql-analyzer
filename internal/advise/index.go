package advise

import (
	"fmt"
	"strings"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
)

// IndexSuggestion is a per-relation CREATE INDEX recommendation aggregated
// across all findings that requested an index.
type IndexSuggestion struct {
	Relation string   `json:"relation"`
	Columns  []string `json:"columns"`
}

// SQL renders the index suggestion as executable SQL. Index names are derived
// from the relation and leading column to keep output stable and idempotent.
func (s IndexSuggestion) SQL() string {
	name := indexName(s.Relation, s.Columns)
	return fmt.Sprintf("CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON %s (%s);",
		name, s.Relation, strings.Join(s.Columns, ", "))
}

// indexName builds a deterministic index identifier.
func indexName(relation string, cols []string) string {
	rel := relation
	if i := strings.LastIndex(rel, "."); i >= 0 {
		rel = rel[i+1:]
	}
	prefix := "idx"
	if len(cols) > 1 {
		prefix = "idx_multi"
	}
	return fmt.Sprintf("%s_%s_%s", prefix, rel, strings.Join(cols, "_"))
}

// IndexSuggestions aggregates index_columns/index_relation evidence across
// findings into one suggestion per relation, preserving column order and
// de-duplicating. Relations are returned in first-seen order.
func IndexSuggestions(findings []analyzer.Finding) []IndexSuggestion {
	ordered := []string{}
	columns := map[string][]string{}
	seen := map[string]map[string]bool{}

	for _, f := range findings {
		rel, _ := f.Evidence["index_relation"].(string)
		if rel == "" {
			continue
		}
		raw, _ := f.Evidence["index_columns"].([]string)
		if len(raw) == 0 {
			continue
		}
		if _, ok := columns[rel]; !ok {
			ordered = append(ordered, rel)
			seen[rel] = map[string]bool{}
		}
		for _, c := range raw {
			if !seen[rel][c] {
				seen[rel][c] = true
				columns[rel] = append(columns[rel], c)
			}
		}
	}

	out := make([]IndexSuggestion, 0, len(ordered))
	for _, rel := range ordered {
		out = append(out, IndexSuggestion{Relation: rel, Columns: columns[rel]})
	}
	return out
}
