package plan

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON captures which raw keys were present before decoding into the
// typed struct, so callers can distinguish absent fields from zero values
// (most importantly "Actual Rows", whose absence means the plan was not run
// with ANALYZE). The type alias avoids infinite recursion.
//
// 自定义反序列化：先把原始 key 集合记进 present map，再用类型别名 alias 走默认反序列化。
// —— 用 alias 是为了避免 UnmarshalJSON 无限递归（alias 没有 UnmarshalJSON，故不会回调自身）。
// —— 子节点 Plans []*PlanNode 仍会用本方法递归解析，从而为每个节点都填好 present。
func (n *PlanNode) UnmarshalJSON(data []byte) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	type alias PlanNode
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*n = PlanNode(a)
	n.present = make(map[string]bool, len(keys))
	for k := range keys {
		n.present[k] = true
	}
	return nil
}

// Parse decodes PostgreSQL EXPLAIN output, auto-detecting format:
//
//   - first non-whitespace byte '[' or '{' -> EXPLAIN (FORMAT JSON);
//   - otherwise EXPLAIN text output (indentation-based).
//
// IsAnalyze is derived from whether nodes carry actual execution stats, which
// determines whether rules depending on runtime stats are enabled.
//
// 自动识别 JSON / 文本格式：首字符为 [ 或 { 走 JSON，否则走文本解析。
func Parse(data []byte) (*PlanResult, error) {
	for _, b := range data {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		if b == '[' || b == '{' {
			return parseJSON(data)
		}
		return ParseText(data)
	}
	return nil, fmt.Errorf("empty explain input")
}

// parseJSON decodes PostgreSQL EXPLAIN (FORMAT JSON) output.
func parseJSON(data []byte) (*PlanResult, error) {
	var statements []json.RawMessage
	if err := json.Unmarshal(data, &statements); err != nil {
		return nil, fmt.Errorf("explain output is not a JSON array: %w", err)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("explain JSON array is empty")
	}

	var stmt Statement
	if err := json.Unmarshal(statements[0], &stmt); err != nil {
		return nil, fmt.Errorf("unmarshal explain statement: %w", err)
	}
	if stmt.Plan == nil {
		return nil, fmt.Errorf("explain statement has no Plan node")
	}

	return &PlanResult{
		Root:          stmt.Plan,
		ExecutionTime: stmt.ExecutionTime,
		PlanningTime:  stmt.PlanningTime,
		IsAnalyze:     stmt.Plan.HasActual(),
	}, nil
}
