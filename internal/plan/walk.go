package plan

// Visitor is called for every node during a Walk. The visitor receives the
// node, its parent (nil for the root) and its depth. Returning false skips the
// node's children.
type Visitor func(node, parent *PlanNode, depth int) bool

// Walk performs a pre-order traversal of the plan tree starting at root.
func Walk(root *PlanNode, visit Visitor) {
	walk(root, nil, 0, visit)
}

func walk(node, parent *PlanNode, depth int, visit Visitor) {
	if node == nil {
		return
	}
	if !visit(node, parent, depth) {
		return
	}
	for _, child := range node.Plans {
		walk(child, node, depth+1, visit)
	}
}

// ForEach calls fn on every node in the tree (full traversal, no skipping).
func ForEach(root *PlanNode, fn func(node, parent *PlanNode, depth int)) {
	Walk(root, func(node, parent *PlanNode, depth int) bool {
		fn(node, parent, depth)
		return true
	})
}

// FindByType returns all nodes whose Node Type matches nodeType.
func FindByType(root *PlanNode, nodeType string) []*PlanNode {
	var out []*PlanNode
	ForEach(root, func(node, parent *PlanNode, depth int) {
		if node.NodeType == nodeType {
			out = append(out, node)
		}
	})
	return out
}

// FindByRelation returns all scan nodes touching the given relation name.
func FindByRelation(root *PlanNode, relation string) []*PlanNode {
	var out []*PlanNode
	ForEach(root, func(node, parent *PlanNode, depth int) {
		if node.RelationName == relation {
			out = append(out, node)
		}
	})
	return out
}

// PathVisitor is like Visitor but also receives the breadcrumb of node labels
// from the root down to and including the current node.
type PathVisitor func(node, parent *PlanNode, depth int, path []string) bool

// WalkPath performs a pre-order traversal, passing each node its ancestor
// label path (including itself). Returning false skips children.
func WalkPath(root *PlanNode, visit PathVisitor) {
	walkPath(root, nil, 0, nil, visit)
}

func walkPath(node, parent *PlanNode, depth int, path []string, visit PathVisitor) {
	if node == nil {
		return
	}
	cur := append(path, node.Label())
	// Defensive copy so each visitor gets a stable slice.
	p := make([]string, len(cur))
	copy(p, cur)
	if !visit(node, parent, depth, p) {
		return
	}
	for _, child := range node.Plans {
		walkPath(child, node, depth+1, p, visit)
	}
}
