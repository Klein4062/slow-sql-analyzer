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

// Parse decodes PostgreSQL EXPLAIN (FORMAT JSON) output.
//
// The on-the-wire shape is a JSON array with one element per statement
// (a single EXPLAIN yields one element):
//
//	[ {"Plan": {...}, "Execution Time": 1.2, "Planning Time": 0.1} ]
//
// Parse uses the first statement. An error is returned if the input is not a
// JSON array or contains no plan.
//
// 解析 PG 的 EXPLAIN (FORMAT JSON) 输出。顶层是一个数组（每条语句一个元素，
// 一条 EXPLAIN 只产生一个元素）。取第一个元素。IsAnalyze 由根节点是否有
// "Actual Rows" key 判定——这决定依赖真实统计的规则是否启用。
func Parse(data []byte) (*PlanResult, error) {
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
