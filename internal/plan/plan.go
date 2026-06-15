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
	NodeType           string `json:"Node Type"`
	ParentRelationship string `json:"Parent Relationship"`
	ParallelAware      bool   `json:"Parallel Aware"`
	AsyncCapable       bool   `json:"Async Capable"`
	RelationName       string `json:"Relation Name"`
	Schema             string `json:"Schema"`
	Alias              string `json:"Alias"`

	// Planner estimates —— 规划器估算值（不需要 ANALYZE 就有）
	StartupCost float64 `json:"Startup Cost"` // 启动成本：产出第一行所需代价
	TotalCost   float64 `json:"Total Cost"`   // 总成本：产出全部行的代价
	PlanRows    float64 `json:"Plan Rows"`    // 估算行数（注意：扫描节点这里是过滤后的输出行数，非扫描输入行数）
	PlanWidth   int     `json:"Plan Width"`   // 估算每行平均字节数

	// Actual execution stats —— 真实执行统计，仅当用 ANALYZE 跑过时才存在
	ActualStartupTime float64 `json:"Actual Startup Time"`
	ActualTotalTime   float64 `json:"Actual Total Time"` // 含子节点的累计耗时（ms）
	ActualRows        float64 `json:"Actual Rows"`       // 每轮循环的平均行数（loops>1 时需乘以 loops 才是总量）
	ActualLoops       float64 `json:"Actual Loops"`      // 该节点被执行的次数

	// Scans & filtering —— 扫描与过滤
	Filter              string  `json:"Filter"`                 // 残留过滤条件（扫描后再剔除行的谓词）
	RowsRemovedByFilter float64 `json:"Rows Removed by Filter"` // 被过滤丢掉的行数（判断低效过滤的关键）
	IndexName           string  `json:"Index Name"`
	IndexCond           string  `json:"Index Cond"` // 走索引时的索引条件
	ScanDirection       string  `json:"Scan Direction"`
	RecheckCond         string  `json:"Recheck Cond"`

	// Sort —— 排序节点
	SortKey       []string `json:"Sort Key"`        // 排序键
	SortMethod    string   `json:"Sort Method"`     // 排序方法（如 "external merge" 即溢出磁盘）
	SortSpaceUsed int      `json:"Sort Space Used"` // 占用空间（kB）
	SortSpaceType string   `json:"Sort Space Type"` // "Disk" 表示落盘，"Memory" 表示内存排序

	// Joins —— 连接节点
	JoinType    string `json:"Join Type"`  // Inner / Left / ...
	HashCond    string `json:"Hash Cond"`  // Hash Join 的连接条件
	MergeCond   string `json:"Merge Cond"` // Merge Join 的连接条件
	InnerUnique bool   `json:"Inner Unique"`
	JoinFilter  string `json:"Join Filter"`

	// Hash / Hash Aggregate —— 哈希表与哈希聚合
	HashBuckets         int `json:"Hash Buckets"`          // 实际桶数
	HashBatches         int `json:"Hash Batches"`          // 批次数：>1 表示内存放不下、溢出到磁盘
	OriginalHashBuckets int `json:"Original Hash Buckets"` // 规划器预估桶数
	OriginalHashBatches int `json:"Original Hash Batches"` // 规划器预估批次数
	PeakMemory          int `json:"Peak Memory Usage"`     // 峰值内存（kB）

	// Aggregate —— 聚合节点
	Strategy    string   `json:"Strategy"`  // Plain / Hashed / Sorted / Mixed
	GroupKey    []string `json:"Group Key"` // 分组键
	PartialMode string   `json:"Partial Mode"`
	Hashed      bool     `json:"Hashed"`

	// Buffers / I/O —— 缓冲与 IO 统计（仅当带 BUFFERS 时存在）。命中率 = Hit / (Hit + Read)
	SharedHitBlocks     float64 `json:"Shared Hit Blocks"`  // 命中共享缓冲池的块数
	SharedReadBlocks    float64 `json:"Shared Read Blocks"` // 未命中、从磁盘读的块数（命中率低则此项大）
	SharedDirtiedBlocks float64 `json:"Shared Dirtied Blocks"`
	SharedWrittenBlocks float64 `json:"Shared Written Blocks"`
	LocalHitBlocks      float64 `json:"Local Hit Blocks"`
	LocalReadBlocks     float64 `json:"Local Read Blocks"`
	TempReadBlocks      float64 `json:"Temp Read Blocks"`    // 临时文件读（排序/哈希溢出时出现）
	TempWrittenBlocks   float64 `json:"Temp Written Blocks"` // 临时文件写

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
// Has 返回某个原始 EXPLAIN key 是否出现在该节点上。
// 关键用途：区分「字段缺失」与「零值」——例如 Actual Rows=0 是合法值（确实返回 0 行），
// 而字段缺失意味着计划没用 ANALYZE 跑。present map 在 UnmarshalJSON 中填充。
func (n *PlanNode) Has(key string) bool {
	if n == nil || n.present == nil {
		return false
	}
	return n.present[key]
}

// HasActual reports whether this node carries actual execution stats.
// HasActual 报告该节点是否带真实执行统计（即用 ANALYZE 跑过）。
func (n *PlanNode) HasActual() bool {
	return n.Has("Actual Rows")
}

// ActualRowsTotal returns the total rows produced across all loops
// (ActualRows is the per-loop average in PostgreSQL).
// ActualRowsTotal 返回跨所有循环的总行数（PG 的 ActualRows 是「每轮循环平均」）。
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
