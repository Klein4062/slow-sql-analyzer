// Package advise turns analysis findings into concrete, copy-pasteable SQL
// actions (CREATE INDEX, SET work_mem, ANALYZE). It is heuristic — column
// references are extracted by lightweight tokenization, not a full SQL parser —
// so suggestions are clearly advisory.
package advise

import (
	"regexp"
	"strings"
)

var (
	// stringLitRe matches single-quoted SQL string literals.
	stringLitRe = regexp.MustCompile(`'(?:[^']|'')*'`)
	// castRe matches PostgreSQL type casts like "::text" or "::numeric(10,2)".
	castRe = regexp.MustCompile(`::[a-zA-Z_][\w]*(?:\s*\([^)]*\))?`)
	// identRe matches an optional "alias." qualifier plus a column identifier.
	// Capture 1 = qualifier incl. trailing dot (or ""), capture 2 = column.
	identRe = regexp.MustCompile(`(?i)([a-zA-Z_]\w*\.)?([a-zA-Z_]\w*)`)
)

// reserved are SQL tokens that are not column references.
var reserved = map[string]bool{
	"and": true, "or": true, "not": true, "is": true, "in": true, "any": true,
	"all": true, "between": true, "like": true, "ilike": true, "null": true,
	"true": true, "false": true, "as": true, "case": true, "when": true,
	"then": true, "else": true, "end": true, "exists": true, "select": true,
	"cast": true, "coalesce": true,
}

// ExtractColumnsFor pulls column references out of a PostgreSQL condition
// string (a Filter or Index Cond). Only unqualified columns and columns
// qualified with keepAlias are returned; columns qualified with other aliases
// (the other side of a join) are dropped. Order of first appearance is kept.
// This is intentionally heuristic.
func ExtractColumnsFor(cond, keepAlias string) []string {
	if cond == "" {
		return nil
	}
	s := stringLitRe.ReplaceAllString(cond, " ")
	s = castRe.ReplaceAllString(s, " ")

	var cols []string
	seen := map[string]bool{}
	for _, m := range identRe.FindAllStringSubmatchIndex(s, -1) {
		fullEnd := m[1]
		// Group 1 (optional qualifier) has index -1 when it did not match.
		var qual string
		if m[2] >= 0 {
			qual = s[m[2]:m[3]]
		}
		col := s[m[4]:m[5]]

		// Skip function calls (identifier immediately followed by '(').
		if fullEnd < len(s) && s[fullEnd] == '(' {
			continue
		}
		if reserved[strings.ToLower(col)] {
			continue
		}
		if qual != "" {
			alias := strings.TrimSuffix(qual, ".")
			if !strings.EqualFold(alias, keepAlias) {
				continue
			}
		}
		if col == "" || seen[col] {
			continue
		}
		seen[col] = true
		cols = append(cols, col)
	}
	return cols
}
