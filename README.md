# slow-sql-analyzer

![CI](https://github.com/Klein4062/slow-sql-analyzer/actions/workflows/ci.yml/badge.svg)
[![Release](https://img.shields.io/github/v/release/Klein4062/slow-sql-analyzer)](https://github.com/Klein4062/slow-sql-analyzer/releases/latest)

通过 **SQL 与执行计划**分析 PostgreSQL 查询计划是否最优，并给出**可执行的优化建议**（缺失索引、`work_mem` 调优、`ANALYZE` 统计、查询改写提示）。

提供 **CLI**、**HTTP API** 与**可视化网页**三种使用方式，支持 **离线解析**已有 EXPLAIN 计划与 **实时连库** 执行 EXPLAIN 两种数据来源。已用真实 PostgreSQL 17 闭环验证（无索引查询建议建索引后，14.97ms → 1.23ms，≈12×）。

```
PostgreSQL Plan Analysis
────────────────────────────────────────────────────────────
execution time: 982.39 ms   planning time: 0.09 ms
findings: 2 critical, 2 warning, 0 info

🔴 [SEQSCANLARGETABLE] Seq Scan on public.orders
  problem:        sequential scan ... estimated to read ~1.0M rows; filter removes most of them
  recommendation: add an index on the columns referenced in the WHERE condition ...

Suggested actions
# Add indexes
  CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_status ON public.orders (status);
```

## 特性

- **三种界面**：CLI、HTTP JSON API（`/v1/plan`、`/v1/analyze`）、浏览器**可视化网页**（`GET /`，带可点击跳转的标注计划树）。
- **9 条检测规则**，覆盖最常见的计划反模式：
  | 规则 | 触发 | 建议 |
  |---|---|---|
  | `SeqScanLargeTable` | 大表全表扫描 | 在过滤列上建索引 |
  | `CardinalityMisestimate` | 估算行数 vs 实际偏差过大 | `ANALYZE` / `CREATE STATISTICS` |
  | `DiskSort` | 排序溢出到磁盘 | 调大 `work_mem` / 建排序索引 |
  | `HashSpill` | Hash 批次 > 1（溢出） | 调大 `work_mem` |
  | `NestedLoopExpensiveInner` | Nested Loop 重扫昂贵内表 | 给内表建索引 / 换 join |
  | `InefficientFilter` | 过滤丢弃绝大多数行却未走索引 | 建索引到过滤列 |
  | `LowBufferHitRatio` | 缓冲命中率低 | 调大 `shared_buffers` |
  | `Hotspot` | 节点独占执行时间过高 | 指出瓶颈 |
  | `StaleStatistics` | 统计信息过时（实时模式，查 `pg_stat_user_tables`） | `ANALYZE` 刷新统计 |
- **CREATE INDEX 建议**自动从 `Filter`/`Index Cond` 提取列、按表聚合去重（启发式）。
- **work_mem 建议值**按最大溢出量自动估算。
- 依赖实际统计的规则在「仅估算」计划下自动跳过并提示。
- 分析层为**纯函数、零 IO**，可完整单测，易于扩展（未来支持 MySQL 只需新增 source 适配器）。

## 安装

**方式一：直接下载预编译二进制（最省事，无需装 Go）**

从 [Releases](https://github.com/Klein4062/slow-sql-analyzer/releases/latest) 下载对应平台的单文件（完全静态、零依赖），例如 x86_64 Linux：

```bash
curl -L -o slow-sql-analyzer \
  https://github.com/Klein4062/slow-sql-analyzer/releases/download/v0.2.0/slow-sql-analyzer-linux-amd64
chmod +x slow-sql-analyzer
./slow-sql-analyzer version   # v0.2.0
```

**方式二：从源码构建**

```bash
go install github.com/Klein4062/slow-sql-analyzer/cmd/slow-sql-analyzer@latest
# 或
git clone https://github.com/Klein4062/slow-sql-analyzer.git
cd slow-sql-analyzer && make build
```

### 内网部署（零依赖单文件）

编译为**单个完全静态的二进制**，拷到内网目标机直接跑，不需要 Go/Python/`libpq`/`psql`：
（实时连库用 pgx，已编译进二进制。）

- 没装 Go？直接用上面的「方式一」从 Releases 下载，内网机零安装。
- 有 Go 的构建机：`make build-all` 一次性交叉编译 Linux/macOS/Windows × amd64/arm64 → `dist/`，再 `scp` 到内网机。

```bash
make build-all
scp dist/slow-sql-analyzer-linux-amd64 user@host:~/slow-sql-analyzer
```

构建机也无法联网时用 `make vendor` 后 `go build -mod=vendor` 离线构建。
完整步骤（air-gapped 构建、systemd 服务、只读账号建议）见 [docs/DEPLOY.md](docs/DEPLOY.md)。

## 快速开始

### 离线分析（无需数据库）

把 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` 的输出喂给工具：

```bash
psql -d mydb -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT * FROM orders WHERE status='pending'" \
    -t -A | slow-sql-analyzer plan

# 或从文件
slow-sql-analyzer plan -f explain.json

# JSON 输出（便于集成）
slow-sql-analyzer plan -f explain.json --format json
```

### 实时分析（连库执行 EXPLAIN）

默认用内置 pgx 驱动直连：

```bash
slow-sql-analyzer analyze \
    --dsn "postgres://user:pass@host:5432/db?sslmode=disable" \
    --query "SELECT * FROM orders WHERE status='pending'"

# 从文件读 SQL
slow-sql-analyzer analyze --dsn "..." -f query.sql
```

**自定义客户端**（`--connector command`）：在内网受限环境，可改用自己的客户端（psql、堡垒机/ssh 包装、自定义脚本）来跑 EXPLAIN，工具只解析其 stdout 的 JSON：

```bash
# 用 psql 作为客户端
slow-sql-analyzer analyze \
    --connector command \
    --dsn "host=10.0.0.5 port=5432 user=app dbname=prod sslmode=disable" \
    --query "SELECT * FROM orders WHERE status='pending'" \
    --exec 'psql "{dsn}" -At -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) {sql}"'

# 经堡垒机执行（占位符 {dsn}/{sql}，或用 $SSA_DSN/$SSA_SQL/$SSA_TIMEOUT 环境变量）
slow-sql-analyzer analyze --connector command \
    --exec 'ssh db-bastion "psql \$SSA_DSN -At -c \"EXPLAIN (ANALYZE, FORMAT JSON) \$SSA_SQL\""' \
    --query "SELECT ..."
```

HTTP API 同样支持：`POST /v1/analyze` 传 `{"connector":"command","exec":"...","dsn":"...","query":"..."}`；网页 UI 实时模式的「连接器」下拉可切换。

**安全设计**：默认在 `BEGIN READ ONLY` 事务内执行 EXPLAIN，并设置 `statement_timeout`：

- 写语句（`UPDATE`/`DELETE`/DDL）会被服务器直接拒绝；
- 需要分析写语句时用 `--allow-writes`（仍在 `ROLLBACK` 的事务内，永不提交）；
- 生产库可用 `--no-analyze` 只跑估算（不执行查询）。

### HTTP API（含可视化网页）

```bash
slow-sql-analyzer serve --addr :8080 --dsn "postgres://..."
```

启动后浏览器打开 `http://localhost:8080/` 即可使用**可视化网页**：

- **实时**模式：填 SQL（可选 DSN/超时/是否 ANALYZE/允许写）→ 服务端跑 EXPLAIN；连接器可在「pgx 内置驱动」与「command 自定义客户端」间切换；
- **离线**模式：粘贴 `EXPLAIN (FORMAT JSON)` 输出 → 无需数据库。
- 结果页渲染：带严重度标注的**计划树**（点击节点跳转诊断）、按严重度分色的 findings 卡片、可一键复制的建议动作（CREATE INDEX / ANALYZE / SET work_mem）。

JSON API 端点同时可用：

```bash
# 离线：提交一份 EXPLAIN JSON
curl -s localhost:8080/v1/plan \
    -H 'Content-Type: application/json' \
    -d '{"plan": <EXPLAIN JSON 数组>, "query": "SELECT ..."}'

# 实时：提交 SQL，由服务端跑 EXPLAIN
curl -s localhost:8080/v1/analyze \
    -H 'Content-Type: application/json' \
    -d '{"query": "SELECT ...", "dsn": "postgres://..."}'
```

端点：`POST /v1/plan`、`POST /v1/analyze`、`GET /healthz`。

## 配置

| Flag | 默认 | 说明 |
|---|---|---|
| `--format` | `text` | `text` 或 `json` |
| `--no-color` | `false` | 关闭终端彩色 |
| `--disable-rule` | — | 按名禁用规则（可重复） |
| `--seqscan-rows` | `1000` | 标记大表全表扫描的行数阈值 |
| `--cardinality-ratio` | `10` | 标记基数误估的倍数 |
| `--filter-removal-ratio` | `0.9` | 标记低效过滤的丢弃比例 |
| `--buffer-hit-ratio` | `0.9` | 缓冲命中率下限 |
| `--stale-mod-ratio` | `0.1` | 统计过时阈值：自 ANALYZE 以来修改占比（实时模式） |
| `--analyze` 专属 | | `--no-analyze` / `--allow-writes` / `--timeout` / `--connector` / `--exec` |

## 项目结构

```
internal/
├── plan/       # EXPLAIN JSON 解析 + 计划树模型与遍历
├── source/     # 数据来源：FileSource（离线）/ PostgresSource（实时）
├── analyzer/   # 规则引擎（Finding / Severity / AnalysisContext / Report）
├── rules/      # 9 条检测规则
├── advise/     # CREATE INDEX / work_mem / ANALYZE 动作合成
├── report/     # text / json 渲染 + plan_tree 序列化
├── api/        # chi HTTP 路由 + 可视化网页
│   └── ui/     #   单页 Web UI（go:embed 提供于 GET /）
└── cli/        # cobra 命令（plan / analyze / serve / version）
testdata/       # 真实 EXPLAIN JSON 样例，供单测
```

## 测试

```bash
go test ./...      # 跑测试
make ci            # 与 GitHub Actions 一致的完整门禁：gofmt + vet + build + test
```

所有改动必须保持测试全绿（CI 会自动强制）。测试纪律、基线清单、新增规则清单见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 限制与说明

- 仅支持 PostgreSQL（v1）。规则针对 PG 执行计划字段设计。
- 索引建议为**启发式**：通过轻量词法提取列引用，非完整 SQL 解析，需人工复核。
- 文本格式 EXPLAIN（非 JSON）解析树状结构脆弱，主路径使用 `FORMAT JSON`。

## License

MIT
