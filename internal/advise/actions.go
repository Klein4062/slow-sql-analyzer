package advise

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
)

// ActionKind categorizes a recommended action.
type ActionKind string

const (
	ActionIndex   ActionKind = "index"
	ActionAnalyze ActionKind = "analyze"
	ActionConfig  ActionKind = "config"
)

// Action is a single recommended, usually executable, step.
type Action struct {
	Kind        ActionKind `json:"kind"`
	Description string     `json:"description"`
	SQL         string     `json:"sql,omitempty"`
}

// Actions derives a de-duplicated, ordered action list from findings:
// CREATE INDEX suggestions, ANALYZE table for stale statistics, and a
// work_mem bump when sorts/hashes spilled to disk.
func Actions(findings []analyzer.Finding) []Action {
	var actions []Action

	// Index suggestions.
	for _, s := range IndexSuggestions(findings) {
		actions = append(actions, Action{
			Kind:        ActionIndex,
			Description: fmt.Sprintf("加速 %s 的扫描", s.Relation),
			SQL:         s.SQL(),
		})
	}

	// ANALYZE for relations with misestimated cardinality OR stale statistics.
	analyzeRels := uniqueOrderedRelations(findings, "CardinalityMisestimate", "StaleStatistics")
	for _, rel := range analyzeRels {
		actions = append(actions, Action{
			Kind:        ActionAnalyze,
			Description: fmt.Sprintf("刷新 %s 的规划器统计", rel),
			SQL:         fmt.Sprintf("ANALYZE %s;", rel),
		})
	}

	// work_mem bump when anything spilled.
	if mem := workMemSuggestion(findings); mem != "" {
		actions = append(actions, Action{
			Kind:        ActionConfig,
			Description: "调大 work_mem，避免排序/哈希溢出到磁盘",
			SQL:         mem,
		})
	}

	return actions
}

// uniqueOrderedRelations returns relations (Finding.RelationName) touched by any
// of the given rules, in first-seen order, skipping empty/non-relation nodes.
func uniqueOrderedRelations(findings []analyzer.Finding, rules ...string) []string {
	want := make(map[string]bool, len(rules))
	for _, r := range rules {
		want[r] = true
	}
	var ordered []string
	seen := map[string]bool{}
	for _, f := range findings {
		if !want[f.Rule] || f.RelationName == "" {
			continue
		}
		// "ANALYZE public.orders" — relations only, not synthetic node names.
		if !seen[f.RelationName] {
			seen[f.RelationName] = true
			ordered = append(ordered, f.RelationName)
		}
	}
	sort.Strings(ordered)
	return ordered
}

// workMemSuggestion returns a "SET work_mem" statement sized to the worst spill
// observed, or "" if nothing spilled.
func workMemSuggestion(findings []analyzer.Finding) string {
	maxKB := 0
	for _, f := range findings {
		if f.Rule != "DiskSort" && f.Rule != "HashSpill" {
			continue
		}
		if kb, ok := f.Evidence["sort_space_kb"]; ok {
			if n := toInt(kb); n > maxKB {
				maxKB = n
			}
		}
	}
	if maxKB <= 0 {
		// Hash spill without byte info: give a generic advisory bump.
		for _, f := range findings {
			if f.Rule == "HashSpill" {
				return "SET work_mem = '64MB'; -- 调大直到 batches == 1"
			}
		}
		return ""
	}
	// Suggest ~1.5x the spill size, min 4MB, rounded to a sane value.
	// 按最大溢出量的约 1.5 倍建议 work_mem（留余量确保下次能放回内存），下限 4MB。
	mb := int(math.Ceil(float64(maxKB) / 1024.0 * 1.5))
	if mb < 4 {
		mb = 4
	}
	return fmt.Sprintf("SET work_mem = '%dMB'; -- 覆盖 %d KB 的溢出", mb, maxKB)
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// Describe renders an action kind as a header label.
func (k ActionKind) Describe() string {
	switch k {
	case ActionIndex:
		return "建索引"
	case ActionAnalyze:
		return "刷新统计"
	case ActionConfig:
		return "调整配置"
	}
	return strings.Title(string(k))
}
