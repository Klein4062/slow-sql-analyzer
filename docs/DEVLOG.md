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
9. **实时端到端验证（真实 PostgreSQL 17）**：本地建独立测试库（10 万行、`status` 列倾斜、
   无索引），跑 `analyze` 检出低效过滤 + 全表扫描 + 热点并给出 `idx (status)` 建议；
   建上索引后重跑，Seq Scan(14.97ms) → Index Scan(1.23ms)，findings 锐减——闭环验证
   工具建议确实解决问题。同时验证写语句守卫与 `--allow-writes` 回滚安全（数据未变）。
   **此阶段发现真实 bug**：`SET statement_timeout = $1` 报错（见下）。
10. **可视化 Web UI**：`GET /` 通过 `go:embed` 提供单页应用（实时/离线双模式），渲染带
    严重度标注的计划树（点击节点跳转诊断）、按严重度分色的 findings 卡片、可一键复制的
    建议动作。为此给 JSON 报告新增 `plan_tree` 字段（节点携带所属 finding 索引），CLI
    `--format json` 同步受益。
11. **内网部署：交叉编译 + 静态二进制 + GitHub Release**。一度考虑改用 Python 重写以适配
    内网，但澄清后发现 **Go 的依赖只在编译机装一次**——`CGO_ENABLED=0` 编出单个完全静态
    二进制（`file` 确认 `statically linked`），部署机零依赖，且实时连库的 pgx 已编译进二进制，
    目标机连 `psql`/`libpq` 都不用装，反而比 Python psql 子进程方案更干净。落地：
    - `Makefile`：`build` / `build-all`（交叉编译 linux/darwin/windows × amd64/arm64）、
      `test`/`vet`/`vendor`/`clean`，版本号经 `-ldflags -X` 注入；
    - `make vendor` + `go build -mod=vendor` 支持完全离线的 air-gapped 构建；
    - `docs/DEPLOY.md`：单文件部署、systemd 服务、只读账号建议；
    - 打 `v0.1.0` tag，`gh release create` 上传 6 个平台二进制 + `SHA256SUMS` 校验和。
    一个二进制对应一个平台（不跨平台跑），但一次为全平台各编一个即可。

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
- **`SET` 不接受参数绑定**：`SET statement_timeout = $1` 报 `syntax error at or near "$1"`——
  `SET` 是工具命令，不走 prepared statement，不支持 `$N`。改为内联计算好的毫秒整数
  `fmt.Sprintf("SET statement_timeout = %d", ms)`。**这个 bug 只有连真库才能暴露**，离线
  测试与编译都发现不了——印证了实时端到端验证的必要性。
- **`go:embed` 与 `[]byte` 需空白导入**：用 `var indexHTML []byte` + `//go:embed` 时，
  若不引用 `embed.FS`，普通 `import "embed"` 会报「imported and not used」，须改成
  空白导入 `_ "embed"`。
- **zsh 不做 IFS 分词**：批量改模块路径时 `for f in $files`（`$files` 含换行）在 zsh 下被当成
  单个文件名，sed 对一长串拼接名报 `No such file`。改用 `grep -rl ... | xargs sed`。

## 验证

- `go build ./...`、`go vet ./...`、`go test ./...` 全绿。
- 离线端到端：`seqscan_large.json` → SeqScan + InefficientFilter + CREATE INDEX(status)；
  `cardinality_misestimate.json` → 50000x 基数误估 + NestedLoop 重扫；
  `disk_sort_and_hash.json` → 92.8MB 磁盘排序 + 4 批次 Hash 溢出 + `work_mem=140MB` 建议。
- **实时端到端（PostgreSQL 17）**：无索引倾斜列查询 → 建议 `idx (status)`；建索引后
  14.97ms → 1.23ms（≈12×），findings 1 critical+2 warning → 0 critical+1 warning。
  写语句守卫生效；`--allow-writes` 在回滚事务内执行 UPDATE，前后数据未变。
- HTTP `/v1/plan`、`/v1/analyze`、`/healthz` 与 Web UI `GET /` 经 `httptest` + `curl` 覆盖。
- **打包与发布**：`make build-all` 产出 6 个平台二进制；`file` 确认 linux/amd64 为
  `statically linked`；版本号注入验证为 `v0.1.0`；`gh release create` 上传二进制 + SHA256SUMS
  到 [Release v0.1.0](https://github.com/Klein4062/slow-sql-analyzer/releases/tag/v0.1.0)。

## 后续可扩展

- MySQL / 其它数据库的 source 适配器与规则。
- 文本格式 EXPLAIN 的更健壮解析（目前主路径为 JSON）。
- 更多规则：并行度不足、分区裁剪缺失、JIT 开销、CTE 物化等。
- Web UI 增强：计划树的折叠/展开、diff（加索引前后对比）、历史记录。
