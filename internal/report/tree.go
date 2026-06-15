package report

import (
	"strings"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// treeNode is a serializable slice of the plan tree for JSON/UI consumption.
// Findings holds indices into the report's findings[] for nodes that were
// flagged, so the UI can annotate the tree.
type treeNode struct {
	Label         string     `json:"label"`
	NodeType      string     `json:"node_type"`
	Depth         int        `json:"depth"`
	EstimatedRows float64    `json:"estimated_rows,omitempty"`
	ActualRows    *float64   `json:"actual_rows,omitempty"`
	Loops         float64    `json:"loops,omitempty"`
	TimeMs        float64    `json:"time_ms,omitempty"`
	Cost          float64    `json:"cost,omitempty"`
	Findings      []int      `json:"findings,omitempty"`
	Children      []treeNode `json:"children,omitempty"`
}

// buildPlanTree produces the serializable plan tree, attaching the indices of
// findings whose NodePath matches each node.
func buildPlanTree(result *plan.PlanResult, findings []analyzer.Finding) *treeNode {
	if result == nil || result.Root == nil {
		return nil
	}
	byPath := map[string][]int{}
	for i, f := range findings {
		byPath[f.NodePath] = append(byPath[f.NodePath], i)
	}

	var build func(node *plan.PlanNode, depth int, path []string) treeNode
	build = func(node *plan.PlanNode, depth int, path []string) treeNode {
		current := append(path, node.Label())
		p := strings.Join(current, " > ")
		n := treeNode{
			Label:         node.Label(),
			NodeType:      node.NodeType,
			Depth:         depth,
			EstimatedRows: node.PlanRows,
			Cost:          node.TotalCost,
			Findings:      byPath[p],
		}
		if node.HasActual() {
			ar := node.ActualRows
			n.ActualRows = &ar
			n.Loops = node.ActualLoops
			n.TimeMs = node.ActualTotalTime
		}
		for _, c := range node.Plans {
			n.Children = append(n.Children, build(c, depth+1, current))
		}
		return n
	}
	t := build(result.Root, 0, nil)
	return &t
}
