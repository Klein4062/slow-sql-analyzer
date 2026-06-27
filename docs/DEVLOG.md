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
12. **可插拔连接器（自定义连接串与客户端）**。实时分析原来只走内置 pgx 驱动，对内网受限
    环境（只允许 psql、需经堡垒机、有自定义包装脚本）不够灵活。新增 `command` 连接器：
    用户用 `--exec` 提供命令模板（支持 `{dsn}`/`{sql}` 占位符，等价地用 `$SSA_DSN`/
    `$SSA_SQL`/`$SSA_TIMEOUT` 环境变量避免 shell 引号陷阱），命令负责跑 EXPLAIN 并把
    `FORMAT JSON` 输出到 stdout，工具只解析其输出。默认仍是 pgx（直连，带只读事务/
    超时/写拦截安全网）；command 模式下这些安全控制交由用户的命令自行负责（文档已说明）。
    CLI/API/网页三处都支持切换；`source.PlanSource` 接口让两种连接器对分析层透明。
13. **统计信息过时检测（StaleStatistics）**。基数误估的头号根因是统计陈旧，但此前只能在计划
    里"事后"发现误估。新增主动检测：实时 pgx 模式下，`PostgresSource` 在拿计划的同时查
    `pg_stat_user_tables`（`n_mod_since_analyze`/`last_analyze`/`last_autoanalyze`），把每表
    新鲜度存进 `PlanResult.TableStats`；纯规则 `StaleStatistics` 消费它——修改占比 ≥ 10%
    （`--stale-mod-ratio` 可调，且绝对量 ≥ 1000 避免小表噪声）或从未 ANALYZE 即判过时，
    直接提示 `ANALYZE <table>`。保持规则纯函数（IO 留在 source 层）；离线/命令模式无此数据
    时静默跳过。**踩坑**：真实 PG 17 的 EXPLAIN JSON 常省略 `Schema` 字段（手写 fixture 带了
    schema，所以单测没暴露），导致按 "schema.relname" 匹配查不到表——改为按裸 `relname` 查询。
    已用真实库端到端验证（ANALYZE 后再插 60% 行不分析 → 检出 62% 过时 + 给出 ANALYZE 建议）。
14. **StaleStatistics 双信号 + 置信分级**。原版只在实时模式用 pg_stat 检测，离线/命令模式完全
    盲。改为两个互补信号：实时模式仍用 `n_mod_since_analyze`（**病因**，高置信），并**与计划里
    的基数偏差交叉印证**——若某表既统计过时又在计划里估算 vs 实际严重偏差，病因+症状吻合，
    直接升 critical 并在证据里标 `confirmed_by_cardinality`；离线/命令模式无系统视图，**退化为
    「该表 scan 节点估算 vs 实际偏差」推断**（`mode: inferred`，warning，明确标注为推断）。
    抽取共享 `cardinalityRatio` helper 供 Cardinality 与 Stale 复用，避免逻辑重复。注意偏差≠过时
    （列相关/表达式/JOIN 也会误估），故离线推断置信度低于实时，建议文案区分 ANALYZE 与
    CREATE STATISTICS。离线 fixture（无 DB）现也能给出"该 ANALYZE 了"的提示。
15. **结果中文化 + 离线获取示例 + 规则说明页（可用性打磨）**。
    - 把所有规则的 Problem/Recommendation、advise 动作描述、文本报告框架改为中文，
      **技术名词保留英文**（节点类型 Seq Scan/Index Scan/Hash Join/Nested Loop、SQL
      ANALYZE/CREATE INDEX/CREATE STATISTICS、参数 work_mem/shared_buffers/cost、规则名、
      severity）。`examples/sample-report.json` 重新生成；测试断言同步。
    - 离线模式补充「如何获取离线计划」可折叠提示（带可复制的 `psql -t -A` 命令），
      并把内置示例从极简 SeqScan 换成丰富的 disk_sort_and_hash 计划（触发多规则）。
    - 新增「分析规则说明」页 `GET /rules`，按**通用 / 实时独有 / 离线独有**三类展示全部规则。
      以 `rules.Catalog()` 作为单一数据源（页面与 `/v1/rules` 共用，避免与代码漂移）；
      StaleStatistics 因在实时/离线行为不同而归入两个独有类。测试断言三类齐全 + 9 个规则名全覆盖。
16. **openGauss 计划支持**。openGauss 基于 PG 9.2.4，`EXPLAIN FORMAT JSON` 结构与 PG 完全兼容
    （同名字段），故**行存计划本就能直接分析**。补的是**列存/向量化引擎**的节点识别：在 `plan.go`
    新增节点判定 helper（`IsSeqScan`/`IsSort`/`IsHashNode`/`IsHashAggregate`/`IsNestedLoop` 等，
    覆盖 `CStore Scan`、`Vec Seq Scan`、`Vec Sort`、`Vec Hash`、`VecAgg`、`Vec Nestloop`），4 条规则
    改用 helper 取代硬编码节点字符串——PG 与 openGauss 两种形态都匹配。两份真实结构 fixture
    （行存 + 列存）+ 节点 helper 表测试 + 规则触发断言。
    **本地部署踩坑**：openGauss enmotech 镜像在 Docker Desktop（Mac，cgroup v2）下因 `gs_cgroup`
    无法在 v2 生成 `gscgroup_omm.cfg` 而启动崩溃（5.0.0/latest 均如此）——这是已知问题，非参数
    可绕。故 openGauss 走**离线**（gsql 导出 FORMAT JSON 粘贴）或**实时 command 连接器 + gsql**
    连远程 Linux 实例（原生 cgroup 无此问题）。
17. **新增 CLAUDE.md 与「文档同步」项目约束**。为仓库添加 `CLAUDE.md`（Claude Code 上下文），
    记录分层架构、命令、加规则清单与非显而易见的坑（present-map、规则须纯、analyzer 外部测试包、
    openGauss Vec/CStore 识别、GOPROXY、cgroup v2）；文案中文、技术名词保留英文。
    同时在 CLAUDE.md 新增「项目约束」一节：**每次代码修改后必须同步维护 README（用户可见行为/
    用法变化时）与 DEVLOG（任何实质性改动加一条里程碑）**，提交前自检两问。理由：过时文档比没
    文档更误导。属软约束（指导），若日后要确定性拦截/自动执行，再配 `.claude/settings.json` hook。
18. **新增「覆盖率 90%」项目约束 + `make cover`**。CLAUDE.md 项目约束再加一条：每次代码修改后用
    `make cover` 检查覆盖率，**改动涉及的包**应达 90%，未达标补测试。配套加 `make cover`（生成
    `coverage.out` + 打印总覆盖率）/`make cover-html`，`.gitignore` 忽略 `coverage.out`。
    **重要现实**：当前全局覆盖率远未到 90%（advise/report 80%+，plan/analyzer/rules/api/source ~40%，
    cli/config 0%）——故约束按「改动的包」算、增量达成，**未做 90% 硬门禁**（会立刻让 CI 变红）；
    CI 仍只带 `-cover` 信息性输出。日后想把覆盖率抬到 90% 再开硬门禁，是一项独立的大工作量。
19. **系统性抬升测试覆盖率（46.2% → 75.6%）**。按「低成本高收益」顺序补测试：
    - config 0→100%、analyzer 41→96%、advise 84→97%（达 90% 目标）；
    - rules 43→81%（补 5 条规则正向测试 + helpers + catalog）、plan 45→84%、report 84%；
    - api 45→79%（错误路径：bad json/missing query/no dsn/command 无 exec/未知 connector）；
    - source 19→54%（FileSource 路径/缺失/坏 JSON/stdin、guardWrite、timeoutOr、bareRelationNames、firstWord）、
      cli 0→37%（buildConfig flag 映射、version、buildLiveSource 各分支、runAnalysis 文本+json）。
    cmd/main 与 source.PostgresSource.Fetch（需真实 DB）、cli serve（阻塞监听）未单测——属集成测试范畴。
    总数 46.2%→75.6%。仍未到全局 90%：剩 source/cli/cmd 这类需 live-DB/集成才能覆盖，是后续抬升点。
20. **新增集成测试（`-tags=integration`）覆盖 live 路径**。`internal/source/integration_test.go` 带
    build tag，默认不跑（`make ci` 保持离线快速）；`make test-integration` 时连真实 PostgreSQL：
    建临时库 `ssa_itest` → 5 万行无索引表 → 跑 `PostgresSource.Fetch`（pgx 连接/只读事务/EXPLAIN/
    queryTableStats/回滚）、估算模式、写语句守卫+`--allow-writes` 回滚验证、`CommandSource`+psql。
    跑完自动删库。source 覆盖率 53.5% → **88.4%**（带 tag 时）。DSN 经 `SSA_TEST_ADMIN_DSN` 覆盖。
21. **serve/api 集成测试**。`internal/api/integration_test.go`（同 `//go:build integration`）起真实
    `api.Handler`（httptest）连本地 PG，打 `/v1/analyze`（覆盖之前单测够不着的实时成功路径：
    HTTP→analyzeQuery→PostgresSource→EXPLAIN→渲染）、`/v1/plan` 离线、`/healthz`/`/v1/rules`/`/rules`/`/`。
    用独立临时库 `ssa_itest_api`（与 source 包的 `ssa_itest` 区分，两包可并行）。`make test-integration`
    改为跑 `./internal/source/ ./internal/api/`。api 覆盖率 78.7% → **86.5%**。`cli serve` 的
    `http.ListenAndServe` 那行仍是入口胶水（Handler 已被 httptest 覆盖），未单测。
22. **支持 EXPLAIN 文本格式（非 JSON）解析**。之前只解析 FORMAT JSON。新增 `plan.ParseText`，
    `plan.Parse` 改为**自动识别**（首字符 `[`/`{` 走 JSON，否则走文本）。文本解析为启发式：
    用 `(cost=...)` 标记识别节点行、相对缩进栈建树（规避 PG 文本缩进不规则）、剥离 `->` 子节点
    箭头与 `Parallel ` 前缀（与 JSON 节点类型统一）、按 key 解析规则所需字段（Filter/Rows Removed/
    Index Cond/Hash Cond/Sort Key/Sort Method+Disk/Hash Buckets+Batches/Buffers 等）。
    **踩坑**：先用带 `->` 的手写 fixture 测试失败——查证发现**真实 PG 文本格式确实用 `->` 标记子节点**
    且并行节点带 `Parallel ` 前缀（之前以为纯缩进无箭头），于是从本地 PG 抓真实计划做 ground truth，
    据此修正解析器。另：初版 `indexWord` 的「词边界」判断写反导致 ` on `/` using ` 永远不匹配
    （NodeType 变成整串），改回纯子串搜索。用真实 PG 计划验证：树结构正确、规则触发
    （SeqScanLargeTable/InefficientFilter）。README/CLAUDE.md 去掉「文本不支持」、改为已支持。
23. **新增项目描述文档**。`docs/项目描述.md`（标题「执行计划自动分析工具」），按「摘要 - 创意详情
    （背景描述、解决的问题）- 方案描述」结构成文：背景讲计划晦涩/依赖专家经验/内网受限/多库多格式
    等痛点；方案讲分层纯函数架构、9 条规则、三连接器、可执行建议合成、三界面、安全设计、静态二进制
    部署、真实 PG/openGauss 验证、技术栈。README 顶部加了指向该文档的链接。

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
