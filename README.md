# slow-sql-analyzer

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
- **8 条检测规则**，覆盖最常见的计划反模式：
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
- **CREATE INDEX 建议**自动从 `Filter`/`Index Cond` 提取列、按表聚合去重（启发式）。
- **work_mem 建议值**按最大溢出量自动估算。
- 依赖实际统计的规则在「仅估算」计划下自动跳过并提示。
- 分析层为**纯函数、零 IO**，可完整单测，易于扩展（未来支持 MySQL 只需新增 source 适配器）。

## 安装

```bash
go install github.com/Klein4062/slow-sql-analyzer/cmd/slow-sql-analyzer@latest
```

或源码构建：

```bash
git clone https://github.com/Klein4062/slow-sql-analyzer.git
cd slow-sql-analyzer
go build -o slow-sql-analyzer ./cmd/slow-sql-analyzer
```

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

```bash
slow-sql-analyzer analyze \
    --dsn "postgres://user:pass@host:5432/db?sslmode=disable" \
    --query "SELECT * FROM orders WHERE status='pending'"

# 从文件读 SQL
slow-sql-analyzer analyze --dsn "..." -f query.sql
```

**安全设计**：默认在 `BEGIN READ ONLY` 事务内执行 EXPLAIN，并设置 `statement_timeout`：

- 写语句（`UPDATE`/`DELETE`/DDL）会被服务器直接拒绝；
- 需要分析写语句时用 `--allow-writes`（仍在 `ROLLBACK` 的事务内，永不提交）；
- 生产库可用 `--no-analyze` 只跑估算（不执行查询）。

### HTTP API（含可视化网页）

```bash
slow-sql-analyzer serve --addr :8080 --dsn "postgres://..."
```

启动后浏览器打开 `http://localhost:8080/` 即可使用**可视化网页**：

- **实时**模式：填 SQL（可选 DSN/超时/是否 ANALYZE/允许写）→ 服务端跑 EXPLAIN；
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
| `--analyze` 专属 | | `--no-analyze` / `--allow-writes` / `--timeout` |

## 项目结构

```
internal/
├── plan/       # EXPLAIN JSON 解析 + 计划树模型与遍历
├── source/     # 数据来源：FileSource（离线）/ PostgresSource（实时）
├── analyzer/   # 规则引擎（Finding / Severity / AnalysisContext / Report）
├── rules/      # 8 条检测规则
├── advise/     # CREATE INDEX / work_mem / ANALYZE 动作合成
├── report/     # text / json 渲染 + plan_tree 序列化
├── api/        # chi HTTP 路由 + 可视化网页
│   └── ui/     #   单页 Web UI（go:embed 提供于 GET /）
└── cli/        # cobra 命令（plan / analyze / serve / version）
testdata/       # 真实 EXPLAIN JSON 样例，供单测
```

## 测试

```bash
go test ./...
```

## 限制与说明

- 仅支持 PostgreSQL（v1）。规则针对 PG 执行计划字段设计。
- 索引建议为**启发式**：通过轻量词法提取列引用，非完整 SQL 解析，需人工复核。
- 文本格式 EXPLAIN（非 JSON）解析树状结构脆弱，主路径使用 `FORMAT JSON`。

## License

MIT
