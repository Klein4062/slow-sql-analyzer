# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 这是什么

`slow-sql-analyzer` 分析 PostgreSQL / openGauss 的查询计划是否最优，并给出修复建议
（CREATE INDEX、work_mem、ANALYZE）。三种使用界面：CLI（cobra）、HTTP API + 网页
（chi，单个内嵌 HTML）、JSON。两种数据来源：离线（粘贴 EXPLAIN FORMAT JSON）与实时
（连库执行 EXPLAIN）。模块路径：`github.com/Klein4062/slow-sql-analyzer`（Go 1.26）。

## 项目约束

**每次完成代码修改后，必须同步维护文档**（这是硬性纪律，不是可选步骤）：

- `README.md`：当改动影响**用户可见的行为/用法**（新命令、新 flag、新规则、行为变化、
  兼容性）时，必须同步更新对应章节（特性表、快速开始、配置、限制等）。
- `docs/DEVLOG.md`：任何**实质性改动**都新增一条里程碑，记录做了什么、关键设计决策、
  以及踩到的坑（供后续复盘）。
- 纯重构、注释、格式化等不影响行为的改动可只更 DEVLOG 或跳过。

理由：README 是用户入口、DEVLOG 是演进记录；过时的文档比没有更误导。提交前自检
「这次改动需要更 README 吗？DEVLOG 加条了吗？」——两个都问到再提交。

**每次代码修改后，检查测试覆盖率**（用 `make cover`）：**本次改动涉及的包**应达到
**90%** 的语句覆盖，未达标就补测试用例再提交（新增/修改的逻辑必须有测试）。注意这是按「改动
的包」算，不是全局——当前基线 advise/report 已 80%+，但 plan/analyzer/rules/api/source 仍
~40%、cli/config 为 0%，改动这些包时顺手补测试，逐步抬升。CI 已带 `-cover` 输出各包覆盖率
（信息性，未做 90% 硬门禁——硬门禁需先把覆盖率抬到 90% 再开，否则会立刻让 CI 变红）。

## 常用命令

```bash
make ci                 # 完整门禁，与 GitHub Actions 完全一致：gofmt + vet + build + test。提交前必跑。
make build              # 当前平台二进制 -> dist/
make build-all          # 静态交叉编译 linux/darwin/windows × amd64/arm64 -> dist/
go test -count=1 ./...  # 跑测试（count=1 禁缓存）
go test -run TestName ./internal/rules/      # 跑单个测试
go run ./cmd/slow-sql-analyzer plan -f testdata/seqscan_large.json          # 离线分析
go run ./cmd/slow-sql-analyzer analyze --dsn "..." --query "SELECT ..."     # 实时分析
go run ./cmd/slow-sql-analyzer serve --addr :8080 --dsn "..."               # HTTP + 网页，根路径 /
```

版本号在构建时通过 `-ldflags -X .../cli.Version=$(git describe)` 注入，`make build` 已带。
普通 `go build` 会显示源码默认值 `0.1.0`。

CI（`.github/workflows/ci.yml`）在每次 push/PR 跑 `make ci` 门禁 + 交叉编译 smoke——
**这是硬门禁，不得弱化或删除既有测试。** 测试纪律与新增规则的清单见 `CONTRIBUTING.md`。

## 架构（大局）

严格分层——**分析层是纯函数、不做任何 IO、可用 fixture 计划完整单测**。数据单向流动：

```
source（取计划） → plan（解析 + 树） → analyzer（纯规则） → report（渲染）
```

- **`internal/source`** —— 实现 `PlanSource.Fetch() (*plan.PlanResult, error)`：
  `FileSource`（离线，stdin/文件）、`PostgresSource`（内置 **pgx** 驱动，实时）、
  `CommandSource`（shell 调 psql/gsql，用于内网）。三者对分析层透明。**只有 source 做 IO**
  ——实时统计新鲜度在这里查，附到 `PlanResult.TableStats`，再由纯规则消费。
- **`internal/plan`** —— `PlanNode` 映射 PostgreSQL EXPLAIN FORMAT JSON（json tag 带空格）。
  `Parse` 自动识别 JSON / 文本两种格式（文本经 `textparse.go` 启发式解析：`cost=` 标记识别节点行、
  相对缩进栈建树、剥离 `->`/`Parallel ` 前缀、按 key 解析规则所需字段）。自定义 `UnmarshalJSON`
  把原始存在的 key 记进 `present` map，从而区分**字段缺失 vs 零值**
  （如 `Actual Rows` 缺失 ⇒ 没用 ANALYZE 跑；见 `HasActual`/`IsAnalyze`）。节点判定 helper
  （`IsSeqScan`/`IsSort`/`IsHashNode`/`IsHashAggregate`/`IsNestedLoop`）同时覆盖 PostgreSQL
  与 **openGauss 向量化/列存**节点（`CStore Scan`、`Vec Seq Scan`、`Vec Hash`、`VecAgg`、
  `Vec Nestloop`…）。
- **`internal/analyzer`** —— `Rule` 接口（`Name()`、`Analyze(*AnalysisContext)`）与
  `Analyzer.Run`（跑启用的规则、按严重度排序 findings）。`AnalysisContext` 携带计划 + 阈值
  + `IsAnalyze`。
- **`internal/rules`** —— 每条规则一个文件，9 条全部在 `default.go` 注册。规则是
  `*AnalysisContext` 的**纯函数**；依赖运行时统计的规则（Cardinality/NestedLoop/
  InefficientFilter/Hotspot）在 `!IsAnalyze` 时自行跳过。
- **`internal/advise`** —— 把 findings 转成可复制执行的 SQL 动作：CREATE INDEX（从
  Filter/Index Cond 启发式提取列，非完整 SQL 解析）、work_mem（按最大溢出量估算）、ANALYZE。
- **`internal/report`** —— `text`（彩色）与 `json` 渲染器共用一个 `Model`；JSON 含序列化的
  `plan_tree`（带 finding 索引，供网页用）。
- **`internal/api`**（chi）+ **`internal/cli`**（cobra）—— 薄界面层。网页是单个
  `internal/api/ui/index.html`，经 `go:embed` 在 `GET /` 提供；规则说明页 `GET /rules`
  由 `rules.Catalog()`（单一数据源）驱动。

### 新增一条规则（最常见的改动）
1. 在 `internal/rules/<name>.go` 实现 `Rule` —— 用 `plan.PlanNode` 的 helper，别硬编码
   节点字符串（这样 openGauss 的 Vec/CStore 形态也能匹配）。
2. 在 `internal/rules/default.go` 注册。
3. 在 `testdata/` 加一个能触发的 fixture，并在 `internal/rules/rules_test.go` 加断言。
4. 若面向用户，在 `rules.Catalog()`（`internal/rules/catalog.go`）加一行。
5. `make ci` 全绿。

## 约定与坑

- **规则保持纯函数。** 凡是需要 DB 的（如统计新鲜度），由 `source` 取数据、附到
  `PlanResult`，再由规则读取。
- **`testdata/*.json` 被测试和 CLI 演示共用** —— 保持真实。`examples/sample-report.json`
  由 `testdata/disk_sort_and_hash.json` 重新生成；漂移时 `TestExampleReportIsUpToDate`
  会失败并给出重生成命令。
- **analyzer 的测试用外部包 `analyzer_test`**，以避免与 `internal/rules` 的循环导入
  （rules 依赖 analyzer）。
- 这里的 Go 坑：`if` 初始化语句里的复合字面量要加括号（`if x := (Rule{}).Analyze(ctx); …`）；
  `FindAllStringSubmatchIndex` 对不匹配的可选组返回 `-1`，切片前要先判。
- **网络**：本环境需 `GOPROXY=https://goproxy.cn,direct`（默认代理超时）。air-gapped 构建：
  `make vendor` 后 `go build -mod=vendor`。
- **实时 openGauss**：用 `command` 连接器 + `gsql`（pgx 无法认证 openGauss 默认的 sha256），
  且 FORMAT JSON 前要先 `SET explain_perf_mode=normal`。本地 Docker Desktop（Mac）跑
  openGauss 受 cgroup v2 不兼容所限——离线/gsql 是支持的路径。
- 输出文案为**中文、技术名词保留英文**（节点类型、SQL、参数、规则名、severity）。改文案时
  沿用此风格。

## 关键界面

- CLI 命令：`plan`（离线）、`analyze`（`--connector pgx|command`、`--exec`）、`serve`、
  `version`。全局 flag 含 `--format text|json`、`--disable-rule`，以及各规则阈值
  （如 `--seqscan-rows`、`--stale-mod-ratio`）。
- HTTP：`POST /v1/plan`、`POST /v1/analyze`、`GET /v1/rules`、`GET /rules`、`GET /healthz`。
- 发布为静态单二进制；`gh release create` 上传 `dist/*` + `SHA256SUMS`。
