package rules

// 规则分类：决定在「规则说明」页面中归入哪个分组。
const (
	// CategoryCommon：通用规则，离线与实时均生效（部分需计划带 ANALYZE 统计）。
	CategoryCommon = "common"
	// CategoryLive：实时分析独有（依赖连库查系统表等离线拿不到的数据）。
	CategoryLive = "live"
	// CategoryOffline：离线分析独有（实时模式不会触发的降级/推断行为）。
	CategoryOffline = "offline"
)

// Info is user-facing metadata about a rule, for the rules reference page/API.
// 规则的面向用户元数据，供「规则说明」页面与 /v1/rules 接口使用。
type Info struct {
	Name       string `json:"name"`       // 规则名（英文，稳定标识）
	Category   string `json:"category"`   // common | live | offline
	Title      string `json:"title"`      // 中文简述
	Trigger    string `json:"trigger"`    // 触发条件
	Suggestion string `json:"suggestion"` // 建议
	Note       string `json:"note"`       // 模式/阈值说明
}

// Catalog returns the rule reference. It is the single source of truth for the
// rules page so the UI cannot drift from the implemented rules.
//
// StaleStatistics 出现两次：它在实时模式用 pg_stat（live 独有），在离线模式退化为
// 估算 vs 实际推断（offline 独有），故分别归入两类。
func Catalog() []Info {
	return []Info{
		// —— 通用规则（离线 + 实时均生效）——
		{
			Name: "SeqScanLargeTable", Category: CategoryCommon,
			Title:      "大表全表扫描",
			Trigger:    "Seq Scan 且估算行数超过阈值（--seqscan-rows，默认 1000）",
			Suggestion: "在 WHERE 过滤列上建索引",
			Note:       "仅依赖计划字段，离线/实时均生效",
		},
		{
			Name: "DiskSort", Category: CategoryCommon,
			Title:      "排序溢出磁盘",
			Trigger:    "Sort 节点的 Sort Space Type 为 Disk（外部归并排序落盘）",
			Suggestion: "调大 work_mem；或按排序键建索引、跳过排序",
			Note:       "仅依赖计划字段，离线/实时均生效",
		},
		{
			Name: "HashSpill", Category: CategoryCommon,
			Title:      "Hash 溢出磁盘",
			Trigger:    "Hash 节点或 Hashed 聚合的 Hash Batches > 1",
			Suggestion: "调大 work_mem，使哈希表单批次完成",
			Note:       "仅依赖计划字段，离线/实时均生效",
		},
		{
			Name: "LowBufferHitRatio", Category: CategoryCommon,
			Title:      "缓冲命中率低",
			Trigger:    "节点共享块命中率 Hit/(Hit+Read) 低于阈值（--buffer-hit-ratio，默认 0.9）",
			Suggestion: "调大 shared_buffers；确保热点表驻留内存",
			Note:       "需计划带 BUFFERS；离线/实时均生效",
		},
		{
			Name: "CardinalityMisestimate", Category: CategoryCommon,
			Title:      "基数误估",
			Trigger:    "估算行数 vs 实际行数偏差超倍数（--cardinality-ratio，默认 10×）",
			Suggestion: "执行 ANALYZE；列相关时用 CREATE STATISTICS",
			Note:       "需计划带 ANALYZE 真实统计；实时默认启用，离线需导出时带 ANALYZE",
		},
		{
			Name: "InefficientFilter", Category: CategoryCommon,
			Title:      "低效过滤",
			Trigger:    "扫描未走索引且过滤丢弃行占比 ≥ 阈值（--filter-removal-ratio，默认 0.9）",
			Suggestion: "在过滤列上建索引",
			Note:       "需 ANALYZE（Rows Removed by Filter）；实时默认启用，离线需带 ANALYZE",
		},
		{
			Name: "NestedLoopExpensiveInner", Category: CategoryCommon,
			Title:      "Nested Loop 重扫昂贵内表",
			Trigger:    "Nested Loop 内表被重扫且本身昂贵（最坏为 Seq Scan）",
			Suggestion: "给内表连接键建索引，或改用 Hash/Merge Join",
			Note:       "需 ANALYZE；实时默认启用，离线需带 ANALYZE",
		},
		{
			Name: "Hotspot", Category: CategoryCommon,
			Title:      "耗时热点",
			Trigger:    "节点独占耗时占总执行时间过高（默认 50%）",
			Suggestion: "指出瓶颈，优先处理该节点或子树上的其它诊断",
			Note:       "需 ANALYZE；实时默认启用，离线需带 ANALYZE",
		},

		// —— 实时分析独有 ——
		{
			Name: "StaleStatistics", Category: CategoryLive,
			Title:      "统计过时（病因检测）",
			Trigger:    "查 pg_stat_user_tables：自上次 ANALYZE 以来修改占比 ≥ 10%（--stale-mod-ratio）或从未分析；并与计划基数偏差交叉印证",
			Suggestion: "执行 ANALYZE 刷新统计",
			Note:       "仅实时 pgx 模式（需连库查系统表）；病因+症状吻合时升 critical",
		},

		// —— 离线分析独有 ——
		{
			Name: "StaleStatistics", Category: CategoryOffline,
			Title:      "统计可能过时（推断）",
			Trigger:    "无系统表时，按 scan 节点「估算 vs 实际」严重偏差推断统计可能过时或不足",
			Suggestion: "执行 ANALYZE；谓词列相关时用 CREATE STATISTICS",
			Note:       "仅离线/命令连接器模式（无 DB 连接）；置信度低于实时，标注为 inferred",
		},
	}
}
