# 贡献与测试纪律

本项目已建立**测试基线**，所有后续修改必须通过这些测试——这是不可跳过的强制要求。

## 测试基线

| 包 | 测试文件 | 覆盖 |
|---|---|---|
| `internal/plan` | `parse_test.go` | EXPLAIN JSON 解析、`IsAnalyze` 判定（字段缺失 vs 零值）、树遍历、错误输入 |
| `internal/analyzer` | `analyzer_test.go` | 规则编排、严重度排序、端到端 fixture |
| `internal/rules` | `rules_test.go` | DiskSort/HashSpill/NestedLoop/Cardinality 触发与跳过 |
| `internal/advise` | `advise_test.go` | 列提取正则、索引建议去重、动作合成 |
| `internal/report` | `report_test.go` | text/json 渲染、关键字段 |
| `internal/api` | `server_test.go` | `/v1/plan`、`/healthz`、错误输入 |
| `internal/source` | `command_test.go` | command 连接器：外部命令、占位符、坏输出、超时 |

共 **7 个测试文件、25 个测试函数**，覆盖率核心包 advise 84% / report 83%。
`testdata/*.json` 是配套的真实计划样例，测试与规则共用。

## 修改前必须做的事

```bash
make ci   # 与 GitHub Actions 完全一致：gofmt + vet + build + test（禁缓存）
```

或单独跑测试：

```bash
go test -count=1 ./...
```

## 硬性要求（违反即被 CI 拦下 / 不予合入）

1. **不得删除或弱化既有测试**。重构可以改测试实现，但覆盖的场景必须保留。
2. **新增规则 / 新增功能必须配测试**：加一条规则就在 `rules_test.go` 加一个触发的 fixture 与断言；加一个连接器/接口就在对应包加测试。
3. **修复 bug 先加复现测试**（红 → 绿），再改代码。
4. **gofmt / go vet / go build / go test 全绿**才能提交。
5. 提 PR 后 CI 自动跑全套；红了必须修。

## 跑单个测试

```bash
go test -run TestSeqScanLargeTableFlagsLargeScan ./internal/analyzer/
go test -run TestCommandSource -v ./internal/source/
```

## 添加新规则（清单）

1. `internal/rules/<name>.go` 实现 `Rule` 接口。
2. 在 `internal/rules/default.go` 注册。
3. `testdata/` 加一个能触发的真实计划样例。
4. `internal/rules/rules_test.go` 加断言（规则名 / 严重度 / 关键 evidence）。
5. `make ci` 全绿后提交。
