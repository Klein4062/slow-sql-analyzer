// Package rules implements the individual detection rules. Each rule is a
// concrete type satisfying analyzer.Rule; internal/rules.Default returns the
// full ordered set wired into the CLI/API.
package rules

import (
	"fmt"
	"strings"

	"github.com/Klein4062/slow-sql-analyzer/internal/advise"
)

// joinPath renders an ancestor breadcrumb as "A > B > C".
func joinPath(path []string) string {
	return strings.Join(path, " > ")
}

// formatRows renders a row count compactly (1.2k / 3.4M / 5.6B).
func formatRows(n float64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", n/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", n/1e3)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

// formatPct renders a fraction as a percentage string.
func formatPct(frac float64) string {
	return fmt.Sprintf("%.0f%%", frac*100)
}

// formatBytes renders a size given in kilobytes as a human string (KB/MB/GB).
func formatBytes(kb int) string {
	switch {
	case kb >= 1024*1024:
		return fmt.Sprintf("%.1f GB", float64(kb)/(1024*1024))
	case kb >= 1024:
		return fmt.Sprintf("%.1f MB", float64(kb)/1024)
	default:
		return fmt.Sprintf("%d KB", kb)
	}
}

// indexEvidence builds the Evidence map for an index-worthy finding. It always
// carries estimated_rows / filter / index_cond, and — when columns could be
// extracted — the index_relation / index_columns keys that advise consumes to
// synthesize CREATE INDEX statements.
func indexEvidence(relation, alias, filter, indexCond string, estRows float64) map[string]any {
	ev := map[string]any{
		"estimated_rows": estRows,
		"filter":         filter,
		"index_cond":     indexCond,
	}
	cond := filter
	if cond == "" {
		cond = indexCond
	}
	cols := advise.ExtractColumnsFor(cond, alias)
	if relation != "" && len(cols) > 0 {
		ev["index_relation"] = relation
		ev["index_columns"] = cols
	}
	return ev
}

// mergeEvidence returns a new map combining all keys of a and b (b wins on
// conflict). Avoids mutating the inputs.
func mergeEvidence(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
