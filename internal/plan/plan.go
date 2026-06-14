// Package plan defines the data model for a PostgreSQL execution plan.
//
// The structs in this package map the JSON emitted by PostgreSQL's
// `EXPLAIN (FORMAT JSON)`. The analysis layer consumes this model and never
// touches a database, so it can be unit-tested with fixture plans.
package plan

// PlanNode is a single node in a PostgreSQL execution plan tree.
//
// Field names/tags follow the exact keys produced by EXPLAIN FORMAT JSON
// (note the spaces). Fields that may be absent on some node types are left as
// zero values; use Has/HasActual to distinguish "absent" from "zero".
type PlanNode struct {
	NodeType           string   `json:"Node Type"`
	ParentRelationship string   `json:"Parent Relationship"`
	ParallelAware      bool     `json:"Parallel Aware"`
	AsyncCapable       bool     `json:"Async Capable"`
	RelationName       string   `json:"Relation Name"`
	Schema             string   `json:"Schema"`
	Alias              string   `json:"Alias"`

	// Planner estimates
	StartupCost float64 `json:"Startup Cost"`
	TotalCost   float64 `json:"Total Cost"`
	PlanRows    float64 `json:"Plan Rows"`
	PlanWidth   int     `json:"Plan Width"`

	// Actual execution stats (present only when run with ANALYZE)
	ActualStartupTime float64 `json:"Actual Startup Time"`
	ActualTotalTime   float64 `json:"Actual Total Time"`
	ActualRows        float64 `json:"Actual Rows"`
	ActualLoops       float64 `json:"Actual Loops"`

	// Scans & filtering
	Filter              string  `json:"Filter"`
	RowsRemovedByFilter float64 `json:"Rows Removed by Filter"`
	IndexName           string  `json:"Index Name"`
	IndexCond           string  `json:"Index Cond"`
	ScanDirection       string  `json:"Scan Direction"`
	RecheckCond         string  `json:"Recheck Cond"`

	// Sort
	SortKey       []string `json:"Sort Key"`
	SortMethod    string   `json:"Sort Method"`
	SortSpaceUsed int      `json:"Sort Space Used"`
	SortSpaceType string   `json:"Sort Space Type"`

	// Joins
	JoinType     string `json:"Join Type"`
	HashCond     string `json:"Hash Cond"`
	MergeCond    string `json:"Merge Cond"`
	InnerUnique  bool   `json:"Inner Unique"`
	JoinFilter   string `json:"Join Filter"`

	// Hash / Hash Aggregate
	HashBuckets         int `json:"Hash Buckets"`
	HashBatches         int `json:"Hash Batches"`
	OriginalHashBuckets int `json:"Original Hash Buckets"`
	OriginalHashBatches int `json:"Original Hash Batches"`
	PeakMemory          int `json:"Peak Memory Usage"`

	// Aggregate
	Strategy    string   `json:"Strategy"`
	GroupKey    []string `json:"Group Key"`
	PartialMode string   `json:"Partial Mode"`
	Hashed      bool     `json:"Hashed"`

	// Buffers / I/O (present with BUFFERS)
	SharedHitBlocks     float64 `json:"Shared Hit Blocks"`
	SharedReadBlocks    float64 `json:"Shared Read Blocks"`
	SharedDirtiedBlocks float64 `json:"Shared Dirtied Blocks"`
	SharedWrittenBlocks float64 `json:"Shared Written Blocks"`
	LocalHitBlocks      float64 `json:"Local Hit Blocks"`
	LocalReadBlocks     float64 `json:"Local Read Blocks"`
	TempReadBlocks      float64 `json:"Temp Read Blocks"`
	TempWrittenBlocks   float64 `json:"Temp Written Blocks"`

	// Child nodes
	Plans []*PlanNode `json:"Plans"`

	// present tracks which keys were present in the source JSON, so callers
	// can distinguish an absent "Actual Rows" (plan not run with ANALYZE) from
	// a legitimate zero. Populated by UnmarshalJSON.
	present map[string]bool `json:"-"`
}

// Statement is one element of the top-level EXPLAIN JSON array
// (EXPLAIN of one statement yields exactly one element).
type Statement struct {
	Plan          *PlanNode `json:"Plan"`
	PlanningTime  float64   `json:"Planning Time"`
	ExecutionTime float64   `json:"Execution Time"`
}

// PlanResult is the parsed plan plus statement-level metadata.
type PlanResult struct {
	Root          *PlanNode
	ExecutionTime float64
	PlanningTime  float64
	// IsAnalyze is true when the plan carries actual execution statistics
	// (run with ANALYZE). Rules that depend on runtime numbers check this.
	IsAnalyze   bool
	SourceQuery string
}

// Has reports whether a raw EXPLAIN key was present on this node.
func (n *PlanNode) Has(key string) bool {
	if n == nil || n.present == nil {
		return false
	}
	return n.present[key]
}

// HasActual reports whether this node carries actual execution stats.
func (n *PlanNode) HasActual() bool {
	return n.Has("Actual Rows")
}

// ActualRowsTotal returns the total rows produced across all loops
// (ActualRows is the per-loop average in PostgreSQL).
func (n *PlanNode) ActualRowsTotal() float64 {
	loops := n.ActualLoops
	if loops <= 0 {
		loops = 1
	}
	return n.ActualRows * loops
}

// IsScan reports whether this node is a relation scan (heap or index access).
func (n *PlanNode) IsScan() bool {
	switch n.NodeType {
	case "Seq Scan", "Sample Scan", "Index Scan", "Index Only Scan",
		"Bitmap Heap Scan", "Bitmap Index Scan", "Tid Scan", "CTE Scan",
		"Subquery Scan", "Function Scan", "Foreign Scan", "Custom Scan":
		return true
	}
	return false
}

// UsesIndex reports whether this node reads via an index.
func (n *PlanNode) UsesIndex() bool {
	switch n.NodeType {
	case "Index Scan", "Index Only Scan", "Bitmap Index Scan":
		return true
	}
	return false
}

// QualifiedName returns schema.relation (or just relation when no schema).
func (n *PlanNode) QualifiedName() string {
	if n.Schema != "" {
		return n.Schema + "." + n.RelationName
	}
	return n.RelationName
}

// Label builds a human-readable single-line label for the node, e.g.
// "Seq Scan on public.users" or "Hash Join".
func (n *PlanNode) Label() string {
	if n.RelationName != "" {
		return n.NodeType + " on " + n.QualifiedName()
	}
	return n.NodeType
}

// SharedHitRatio returns the shared-block cache hit ratio in [0,1].
// Returns 1 when no shared blocks were read at all.
func (n *PlanNode) SharedHitRatio() float64 {
	total := n.SharedHitBlocks + n.SharedReadBlocks
	if total <= 0 {
		return 1
	}
	return n.SharedHitBlocks / total
}
