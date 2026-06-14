package plan

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON captures which raw keys were present before decoding into the
// typed struct, so callers can distinguish absent fields from zero values
// (most importantly "Actual Rows", whose absence means the plan was not run
// with ANALYZE). The type alias avoids infinite recursion.
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
