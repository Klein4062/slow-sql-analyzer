package plan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PostgreSQL EXPLAIN (text) format is irregular, so this parser is heuristic:
// node lines are identified by the "(cost=...)" marker, the tree is built from
// relative indentation (robust to PG's per-level indent quirk), and the
// per-node property lines the rules need are parsed by key. Coverage is the
// common cases; exotic nodes degrade gracefully (still appear in the tree).
//
// 解析 PG 文本格式 EXPLAIN：用 cost= 标记识别节点行、相对缩进栈建树、按 key 解析规则所需字段。
// 适用于常见节点；冷门节点仍会出现在树里但不一定解析全部字段。

var (
	reCost   = regexp.MustCompile(`cost=([\d.]+)\.\.([\d.]+)\s+rows=(\d+)\s+width=(\d+)`)
	reActual = regexp.MustCompile(`actual time=([\d.]+)\.\.([\d.]+)\s+rows=(\d+)\s+loops=(\d+)`)
	reSort   = regexp.MustCompile(`(?i)(Disk|Memory):\s*(\d+)kB`)
	reBuffer = regexp.MustCompile(`shared hit=(\d+)(?:\s+read=(\d+))?`)
	reHash   = regexp.MustCompile(`Buckets:\s*(\d+)\s+Batches:\s*(\d+)(?:\s+Memory Usage:\s*(\d+)kB)?`)
)

// ParseText parses a PostgreSQL EXPLAIN (text) plan into a PlanResult.
func ParseText(data []byte) (*PlanResult, error) {
	res := &PlanResult{}
	var (
		stack   []*PlanNode // ancestor chain
		indents []int       // indent of each stack entry
		cur     *PlanNode   // nearest node (property-line target)
	)

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || trimmed == "QUERY PLAN" || isDashLine(trimmed) || isRowsFooter(trimmed) {
			continue
		}
		low := strings.ToLower(trimmed)

		if strings.HasPrefix(low, "planning time:") {
			res.PlanningTime = parseMs(trimmed[strings.IndexByte(trimmed, ':')+1:])
			continue
		}
		if strings.HasPrefix(low, "execution time:") {
			res.ExecutionTime = parseMs(trimmed[strings.IndexByte(trimmed, ':')+1:])
			continue
		}

		indent := len(line) - len(trimmed)

		// Node line: contains a (cost=...) block.
		if reCost.FindString(trimmed) != "" {
			node := parseNodeLine(trimmed)
			if node.HasActual() {
				res.IsAnalyze = true
			}
			for len(indents) > 0 && indents[len(indents)-1] >= indent {
				stack = stack[:len(stack)-1]
				indents = indents[:len(indents)-1]
			}
			if len(stack) == 0 {
				res.Root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Plans = append(parent.Plans, node)
			}
			stack = append(stack, node)
			indents = append(indents, indent)
			cur = node
			continue
		}

		// Property line -> attach to the nearest node.
		if cur != nil {
			parseProperty(cur, trimmed)
		}
	}

	if res.Root == nil {
		return nil, fmt.Errorf("text plan: no node found (expected lines containing \"(cost=...\")")
	}
	return res, nil
}

func parseNodeLine(line string) *PlanNode {
	idx := strings.Index(line, "(cost=")
	if idx < 0 {
		idx = len(line)
	}
	desc := strings.TrimSpace(line[:idx])
	desc = strings.TrimSpace(strings.TrimPrefix(desc, "->")) // strip child-node arrow
	rest := line[idx:]
	n := &PlanNode{present: map[string]bool{}}

	if i := indexWord(desc, " on "); i >= 0 {
		n.Schema, n.RelationName = splitRel(strings.TrimSpace(desc[i+4:]))
		desc = strings.TrimSpace(desc[:i])
	}
	if i := indexWord(desc, " using "); i >= 0 {
		n.IndexName = strings.TrimSpace(desc[i+7:])
		desc = strings.TrimSpace(desc[:i])
	}
	n.NodeType = strings.TrimSpace(strings.TrimPrefix(desc, "Parallel ")) // unify with JSON node types
	switch n.NodeType {                                                   // PG text names differ from JSON
	case "HashAggregate":
		n.NodeType, n.Strategy = "Aggregate", "Hashed"
	case "GroupAggregate":
		n.NodeType, n.Strategy = "Aggregate", "Sorted"
	}

	if m := reCost.FindStringSubmatch(rest); m != nil {
		n.StartupCost = atof(m[1])
		n.TotalCost = atof(m[2])
		n.PlanRows = atof(m[3])
		n.PlanWidth = atoi(m[4])
	}
	if m := reActual.FindStringSubmatch(rest); m != nil {
		n.ActualStartupTime = atof(m[1])
		n.ActualTotalTime = atof(m[2])
		n.ActualRows = atof(m[3])
		n.ActualLoops = atof(m[4])
		n.present["Actual Rows"] = true
	}
	return n
}

func parseProperty(n *PlanNode, line string) {
	ci := strings.Index(line, ": ")
	if ci < 0 {
		return
	}
	key := strings.TrimSpace(line[:ci])
	val := strings.TrimSpace(line[ci+2:])
	switch key {
	case "Filter":
		n.Filter = val
	case "Rows Removed by Filter":
		n.RowsRemovedByFilter = atof(val)
		n.present["Rows Removed by Filter"] = true
	case "Index Cond":
		n.IndexCond = val
	case "Hash Cond":
		n.HashCond = val
	case "Merge Cond":
		n.MergeCond = val
	case "Join Filter":
		n.JoinFilter = val
	case "Recheck Cond":
		n.RecheckCond = val
	case "Sort Key":
		n.SortKey = splitCSV(val)
	case "Sort Method":
		n.SortMethod, n.SortSpaceType, n.SortSpaceUsed = parseSortMethod(val)
	case "Strategy":
		n.Strategy = val
	case "Group Key":
		n.GroupKey = splitCSV(val)
	case "Buckets":
		if m := reHash.FindStringSubmatch(line); m != nil {
			n.HashBuckets = atoi(m[1])
			n.HashBatches = atoi(m[2])
			if m[3] != "" {
				n.PeakMemory = atoi(m[3])
			}
		}
	case "Buffers":
		if m := reBuffer.FindStringSubmatch(val); m != nil {
			n.SharedHitBlocks = atof(m[1])
			if m[2] != "" {
				n.SharedReadBlocks = atof(m[2])
			}
			n.present["Shared Hit Blocks"] = true
		}
	}
}

func parseSortMethod(val string) (method, spaceType string, kb int) {
	method = val
	if m := reSort.FindStringSubmatch(val); m != nil {
		spaceType = strings.ToLower(m[1])
		if spaceType == "disk" {
			spaceType = "Disk"
		} else {
			spaceType = "Memory"
		}
		kb = atoi(m[2])
		method = strings.TrimSpace(val[:strings.Index(val, m[0])])
	}
	return
}

// --- small helpers ---

func splitRel(s string) (schema, rel string) {
	// Text format: "<relname> [alias]" or "<schema>.<relname> [alias]"; take the
	// first whitespace-delimited token as the relation name (alias dropped).
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

func indexWord(s, word string) int {
	// " on " / " using " are already space-delimited tokens; a plain substring
	// search is sufficient (and avoids false negatives from a word-boundary
	// check that the space-delimited word doesn't need).
	return strings.Index(s, word)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseMs(s string) float64 {
	return atof(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "ms")))
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return f }
func atoi(s string) int     { i, _ := strconv.Atoi(strings.TrimSpace(s)); return i }

func isDashLine(s string) bool {
	if len(s) < 5 {
		return false
	}
	for _, r := range s {
		if r != '-' {
			return false
		}
	}
	return true
}

func isRowsFooter(s string) bool {
	// psql "(N rows)" / "(N 行记录)" footer line.
	return strings.HasPrefix(s, "(") && (strings.HasSuffix(s, " rows)") || strings.Contains(s, "行记录") || strings.Contains(s, "rows)"))
}
