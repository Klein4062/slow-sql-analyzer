# 开发日志 (Development Log)

记录 `slow-sql-analyzer` 的设计与实现过程。日期为 2026/06/15。

## 目标

运维/DBA 拿到一份 `EXPLAIN` 计划后，难以快速判断「当前计划是否最优、问题在哪、怎么改」。
本项目用 Go 实现一个工具：输入 SQL 或执行计划，输出反模式诊断 + 可执行建议。
v1 支持 PostgreSQL，CLI + HTTP 两种界面，离线与实时两种数据来源。

## 关键设计决策

### 1. 分层解耦：source → plan → analyzer → report

最重要的架构选择是把**数据获取**（连库 / 读文件）与**分析**彻底分开。分析层是
纯函数：`Analyze(*AnalysisContext) []Finding`，零 IO、零数据库依赖，因此：

- 整个分析引擎可用 `testdata/` 下的真实计划样例完整单测，无需起数据库；
- 未来支持 MySQL / 其它数据库只需新增一个 `source` 适配器，规则引擎不动。

### 2. 规则引擎：每条规则一个文件 + `Rule` 接口

`Rule{Name(), Analyze(ctx)}` 接口让规则可独立开发、可按名禁用（`--disable-rule`）、
可单测。`AnalysisContext` 携带计划根、阈值与 `IsAnalyze` 标志；依赖实际统计的规则
（Cardinality / NestedLoop / InefficientFilter / Hotspot）在仅估算计划下自行跳过。

### 3. 区分「字段缺失」与「零值」

PG 的 `Actual Rows` 为 0 是合法值（确实返回 0 行），而字段缺失意味着计划没带
ANALYZE。用自定义 `UnmarshalJSON` 记录每个节点原始存在的 key 集合（`present map`），
`HasActual()` 据此判定 `IsAnalyze`。这是准确启用/跳过规则的前提。

### 4. 安全的实时模式

默认在 `BEGIN READ ONLY` 事务内执行 `EXPLAIN (ANALYZE)`：写语句被服务器拒绝而非
执行；设置会话级 `statement_timeout` 防失控；`--allow-writes` 才放开写语句且仍在
`ROLLBACK` 的事务里。`--no-analyze` 用于完全不允许执行的生产库。

### 5. 启发式索引建议

不做完整 SQL 解析，而是用正则 tokenizer 从 `Filter`/`Index Cond` 提取列引用：剥除
字符串字面量与 `::type` 类型转换、跳过函数调用与保留字、按 alias 过滤 join 的另一侧。
按表聚合去重后合成 `CREATE INDEX ...`，并在报告里明确标注为启发式、需人工复核。

## 实现里程碑

1. **plan 包**：`PlanNode` 结构体映射 EXPLAIN JSON（含空格的 key tag），JSON 解析 +
   `Walk` / `WalkPath`（带祖先路径的遍历，用于 finding 的 breadcrumb）。
2. **analyzer 框架**：`Finding` / `Severity` / `AnalysisContext` / `Rule` / `Analyzer.Run`
   （按严重度排序）。先落地 `SeqScanLargeTable`、`CardinalityMisestimate` 两条规则打通链路。
3. **剩余规则 + advise**：`DiskSort` / `HashSpill` / `NestedLoopExpensiveInner` /
   `InefficientFilter` / `LowBufferHitRatio` / `Hotspot`；`advise` 合成索引、ANALYZE、
   `work_mem`（按最大溢出量估算 MB）三类动作。
4. **report**：`text`（带严重度标注的计划树 + findings + 建议动作清单）与 `json`
   两种渲染；`Severity.MarshalJSON` 让 JSON 输出 `"critical"` 而非整数。
5. **CLI**：cobra，`plan`（离线）端到端跑通；`analyze`（实时）、`serve`、`version`。
6. **实时 source**：pgx/v5 连接 + 只读事务 + statement_timeout + 写语句守卫。
7. **HTTP API**：chi 路由 `POST /v1/plan`、`POST /v1/analyze`、`GET /healthz`。
8. **测试 + 文档**：parser / rules / advise / report / api 单测，三个 testdata 样例，
   README 与本开发日志。

## 踩过的坑（值得记录）

- **optional capture group 的 submatch 索引为 -1**：列提取正则
  `([a-zA-Z_]\w*\.)?([a-zA-Z_]\w*)` 第一个组可选，不匹配时 `FindAllStringSubmatchIndex`
  返回 `-1, -1`，直接 `s[m[2]:m[3]]` 触发 `slice bounds out of range [:-1]`。需先判
  `m[2] >= 0`。
- **测试包循环导入**：`internal/analyzer` 的测试若用包内测试（`package analyzer`）并
  import `internal/rules`，而 rules 又 import analyzer，形成循环。改为外部测试包
  `package analyzer_test` 解决。
- **Go 模块代理**：默认 `proxy.golang.org` 在国内超时，切换 `GOPROXY=https://goproxy.cn,direct`
  后正常拉取 cobra / pgx / chi / color。
- **PG Hash 节点字段名**：实际是 `Peak Memory Usage` 而非 `Peak Memory`，json tag 需对齐。

## 验证

- `go build ./...`、`go vet ./...`、`go test ./...` 全绿。
- 离线端到端：`seqscan_large.json` → SeqScan + InefficientFilter + CREATE INDEX(status)；
  `cardinality_misestimate.json` → 50000x 基数误估 + NestedLoop 重扫；
  `disk_sort_and_hash.json` → 92.8MB 磁盘排序 + 4 批次 Hash 溢出 + `work_mem=140MB` 建议。
- HTTP `/v1/plan` 与 `/healthz` 经 `httptest` 覆盖。

## 后续可扩展

- MySQL / 其它数据库的 source 适配器与规则。
- 文本格式 EXPLAIN 的更健壮解析（目前主路径为 JSON）。
- 更多规则：并行度不足、分区裁剪缺失、JIT 开销、CTE 物化等。
- 把计划树渲染为 ASCII 或交互式 HTML 报告。
