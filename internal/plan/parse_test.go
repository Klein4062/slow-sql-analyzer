package plan

import (
	"os"
	"path/filepath"
	"testing"
)

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseDetectsAnalyzeFlag(t *testing.T) {
	res, err := Parse(mustRead(t, "cardinality_misestimate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsAnalyze {
		t.Error("expected IsAnalyze=true for a plan with Actual Rows")
	}
	if res.Root.NodeType != "Nested Loop" {
		t.Errorf("root node type = %q, want Nested Loop", res.Root.NodeType)
	}
}

func TestParseRejectsNonArray(t *testing.T) {
	if _, err := Parse([]byte(`{"Plan": {}}`)); err == nil {
		t.Error("expected error for non-array input")
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse([]byte(`[]`)); err == nil {
		t.Error("expected error for empty array")
	}
}

func TestHasDistinguishesAbsentFromZero(t *testing.T) {
	// A node that legitimately has zero Actual Rows must still report HasActual.
	res, err := Parse([]byte(`[
      {"Plan": {"Node Type":"Seq Scan","Actual Rows":0,"Actual Loops":1},
       "Execution Time": 0.1}
    ]`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsAnalyze {
		t.Error("IsAnalyze should be true when 'Actual Rows' key is present (even if 0)")
	}
	if !res.Root.HasActual() {
		t.Error("HasActual should be true when 'Actual Rows' key is present")
	}
}

func TestWalkVisitsAllNodes(t *testing.T) {
	res, err := Parse(mustRead(t, "disk_sort_and_hash.json"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	ForEach(res.Root, func(node, parent *PlanNode, depth int) { count++ })
	// GroupAggregate > Sort > HashJoin > (SeqScan, Hash > SeqScan) = 6 nodes
	if count != 6 {
		t.Errorf("visited %d nodes, want 6", count)
	}
}

// TestNodeKindHelpers covers the openGauss-aware node classification used by
// the rules (PG forms + Vec/CStore forms).
func TestNodeKindHelpers(t *testing.T) {
	cases := []struct {
		nodeType                 string
		strategy                 string
		scan, seqScan, usesIndex bool
		sort, hashNode, hashAgg  bool
		nestedLoop               bool
	}{
		{"Seq Scan", "", true, true, false, false, false, false, false},
		{"Index Scan", "", true, false, true, false, false, false, false},
		{"CStore Scan", "", true, true, false, false, false, false, false}, // openGauss 列存
		{"Vec Seq Scan", "", true, true, false, false, false, false, false},
		{"Vec Index Scan", "", true, false, true, false, false, false, false},
		{"Sort", "", false, false, false, true, false, false, false},
		{"Vec Sort", "", false, false, false, true, false, false, false},
		{"Hash", "", false, false, false, false, true, false, false},
		{"Vec Hash", "", false, false, false, false, true, false, false},
		{"Aggregate", "Hashed", false, false, false, false, false, true, false},
		{"VecAgg", "", false, false, false, false, false, true, false},
		{"Nested Loop", "", false, false, false, false, false, false, true},
		{"Vec Nestloop", "", false, false, false, false, false, false, true},
		{"Hash Join", "", false, false, false, false, false, false, false}, // not a hash BUILD node
	}
	for _, c := range cases {
		n := &PlanNode{NodeType: c.nodeType, Strategy: c.strategy}
		if got := n.IsScan(); got != c.scan {
			t.Errorf("%s: IsScan=%v want %v", c.nodeType, got, c.scan)
		}
		if got := n.IsSeqScan(); got != c.seqScan {
			t.Errorf("%s: IsSeqScan=%v want %v", c.nodeType, got, c.seqScan)
		}
		if got := n.UsesIndex(); got != c.usesIndex {
			t.Errorf("%s: UsesIndex=%v want %v", c.nodeType, got, c.usesIndex)
		}
		if got := n.IsSort(); got != c.sort {
			t.Errorf("%s: IsSort=%v want %v", c.nodeType, got, c.sort)
		}
		if got := n.IsHashNode(); got != c.hashNode {
			t.Errorf("%s: IsHashNode=%v want %v", c.nodeType, got, c.hashNode)
		}
		if got := n.IsHashAggregate(); got != c.hashAgg {
			t.Errorf("%s: IsHashAggregate=%v want %v", c.nodeType, got, c.hashAgg)
		}
		if got := n.IsNestedLoop(); got != c.nestedLoop {
			t.Errorf("%s: IsNestedLoop=%v want %v", c.nodeType, got, c.nestedLoop)
		}
	}
}
