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
	// 匹配单引号字符串字面量（先剥除，避免把字面量里的内容误当列名）
	stringLitRe = regexp.MustCompile(`'(?:[^']|'')*'`)
	// castRe matches PostgreSQL type casts like "::text" or "::numeric(10,2)".
	// 匹配 PG 类型转换（::text / ::numeric(10,2)），剥除以免把类型名误当列名
	castRe = regexp.MustCompile(`::[a-zA-Z_][\w]*(?:\s*\([^)]*\))?`)
	// identRe matches an optional "alias." qualifier plus a column identifier.
	// Capture 1 = qualifier incl. trailing dot (or ""), capture 2 = column.
	// 匹配「可选的 alias. 前缀 + 列名」。捕获组1=前缀（含点），捕获组2=列名。
	identRe = regexp.MustCompile(`(?i)([a-zA-Z_]\w*\.)?([a-zA-Z_]\w*)`)
)

// reserved are SQL tokens that are not column references.
// 这些是 SQL 关键字而非列名，提取时需排除。
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
//
// 从 PG 条件串（Filter / Index Cond）里提取列引用。只返回「无前缀的列」和
// 「带 keepAlias 前缀的列」；join 另一侧（带别的别名前缀）的列会被丢弃。
// 保留首次出现顺序。这是有意为之的启发式（非完整 SQL 解析）。
func ExtractColumnsFor(cond, keepAlias string) []string {
	if cond == "" {
		return nil
	}
	// 第一步：剥除字符串字面量与类型转换，避免污染后续匹配
	s := stringLitRe.ReplaceAllString(cond, " ")
	s = castRe.ReplaceAllString(s, " ")

	var cols []string
	seen := map[string]bool{}
	for _, m := range identRe.FindAllStringSubmatchIndex(s, -1) {
		fullEnd := m[1]
		// Group 1 (optional qualifier) has index -1 when it did not match.
		// 注意：可选捕获组（前缀）不匹配时，其 submatch 索引为 -1，
		// 直接 s[m[2]:m[3]] 会触发越界 panic，必须先判 m[2] >= 0。
		var qual string
		if m[2] >= 0 {
			qual = s[m[2]:m[3]]
		}
		col := s[m[4]:m[5]]

		// Skip function calls (identifier immediately followed by '(').
		// 跳过函数调用：标识符紧跟着 '(' 的视为函数名而非列（如 lower(name) 的 lower）
		if fullEnd < len(s) && s[fullEnd] == '(' {
			continue
		}
		if reserved[strings.ToLower(col)] {
			continue
		}
		// 有前缀时，只保留属于 keepAlias 的列（join 另一侧的列丢弃）
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
