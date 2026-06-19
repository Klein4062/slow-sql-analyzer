# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`slow-sql-analyzer` analyzes PostgreSQL / openGauss query plans for optimality and
suggests fixes (CREATE INDEX, work_mem, ANALYZE). Three surfaces: CLI (cobra),
HTTP API + web UI (chi, single embedded HTML), JSON. Two data sources: offline
(paste EXPLAIN FORMAT JSON) and live (connect & run EXPLAIN). Module path:
`github.com/Klein4062/slow-sql-analyzer` (Go 1.26).

## Commands

```bash
make ci                 # FULL gate, identical to GitHub Actions: gofmt + vet + build + test. Run before committing.
make build              # current-platform binary -> dist/
make build-all          # cross-compile static linux/darwin/windows × amd64/arm64 -> dist/
go test -count=1 ./...  # tests (count=1 bypasses cache)
go test -run TestName ./internal/rules/      # single test
go run ./cmd/slow-sql-analyzer plan -f testdata/seqscan_large.json          # offline analysis
go run ./cmd/slow-sql-analyzer analyze --dsn "..." --query "SELECT ..."     # live analysis
go run ./cmd/slow-sql-analyzer serve --addr :8080 --dsn "..."               # HTTP + web UI at /
```

Version is injected at build via `-ldflags -X .../cli.Version=$(git describe)`;
`make build` does this. A plain `go build` shows the source default `0.1.0`.

CI (`.github/workflows/ci.yml`) runs the `make ci` gates + a cross-compile smoke
on every push/PR — **it is a hard gate; do not weaken or delete existing tests.**
Test discipline and the add-a-rule checklist are in `CONTRIBUTING.md`.

## Architecture (the big picture)

Strict layering — **the analysis layer is pure, does no I/O, and is fully unit-testable
with fixture plans**. Data flows one direction:

```
source (get a plan) → plan (parse + tree) → analyzer (pure rules) → report (render)
```

- **`internal/source`** — implements `PlanSource.Fetch() (*plan.PlanResult, error)`:
  `FileSource` (offline, stdin/file), `PostgresSource` (built-in **pgx** driver, live),
  `CommandSource` (shell out to psql/gsql for intranet). All three are transparent to
  the analyzer. **Only source does I/O** — live stats freshness is fetched here and
  attached to `PlanResult.TableStats`, then consumed by a pure rule.
- **`internal/plan`** — `PlanNode` maps PostgreSQL EXPLAIN FORMAT JSON (json tags have
  spaces). Custom `UnmarshalJSON` records which keys were *present* in a `present` map
  so callers can distinguish **absent vs zero** (e.g. `Actual Rows` missing ⇒ not run
  with ANALYZE; see `HasActual`/`IsAnalyze`). Node-classification helpers
  (`IsSeqScan`/`IsSort`/`IsHashNode`/`IsHashAggregate`/`IsNestedLoop`) cover both
  PostgreSQL and **openGauss vectorized/columnar** nodes (`CStore Scan`, `Vec Seq Scan`,
  `Vec Hash`, `VecAgg`, `Vec Nestloop`…).
- **`internal/analyzer`** — the `Rule` interface (`Name()`, `Analyze(*AnalysisContext)`)
  and `Analyzer.Run` (runs enabled rules, sorts findings by severity). `AnalysisContext`
  holds the plan + thresholds + `IsAnalyze`.
- **`internal/rules`** — one file per rule, all 9 registered in `default.go`. Rules are
  **pure functions** of `*AnalysisContext`; rules needing runtime stats
  (Cardinality/NestedLoop/InefficientFilter/Hotspot) self-skip when `!IsAnalyze`.
- **`internal/advise`** — turns findings into copy-pasteable SQL actions: CREATE INDEX
  (heuristic column extraction from Filter/Index Cond, not a SQL parser), work_mem
  (sized from worst spill), ANALYZE.
- **`internal/report`** — `text` (colored) and `json` renderers sharing one `Model`;
  JSON includes a serialized `plan_tree` with finding indices for the UI.
- **`internal/api`** (chi) + **`internal/cli`** (cobra) — thin surfaces. Web UI is a
  single `internal/api/ui/index.html` served via `go:embed` at `GET /`; rules reference
  page at `GET /rules` fed by `rules.Catalog()` (single source of truth).

### Adding a rule (the common change)
1. Implement `Rule` in `internal/rules/<name>.go` — use the `plan.PlanNode` helpers, not
   hardcoded node-type strings (so openGauss Vec/CStore forms match).
2. Register it in `internal/rules/default.go`.
3. Add a triggering fixture under `testdata/` and an assertion in `internal/rules/rules_test.go`.
4. If user-facing, add a row to `rules.Catalog()` (`internal/rules/catalog.go`).
5. `make ci` green.

## Conventions & gotchas

- **Rules stay pure.** Anything needing the DB (e.g. stats freshness) is fetched by a
  `source`, attached to `PlanResult`, then read by the rule.
- **`testdata/*.json` are shared by tests and the CLI demo** — keep them realistic.
  `examples/sample-report.json` is regenerated from `testdata/disk_sort_and_hash.json`;
  `TestExampleReportIsUpToDate` fails (with the regen command) if it drifts.
- **analyzer tests use the external package `analyzer_test`** to avoid an import cycle
  with `internal/rules` (rules imports analyzer).
- Go gotchas hit here: a composite literal in an `if`-init needs parens
  (`if x := (Rule{}).Analyze(ctx); …`); `FindAllStringSubmatchIndex` returns `-1` for
  non-matching optional groups — guard before slicing.
- **Network**: this environment needs `GOPROXY=https://goproxy.cn,direct` (default proxy
  times out). For air-gapped builds: `make vendor` then `go build -mod=vendor`.
- **Live openGauss**: use the `command` connector + `gsql` (pgx can't auth openGauss's
  default sha256), and `SET explain_perf_mode=normal` before FORMAT JSON. Local openGauss
  in Docker Desktop (Mac) is blocked by a cgroup-v2 incompatibility — offline/gsql paths
  are the supported ones.
- Output text is **Chinese with technical terms in English** (node types, SQL, params,
  rule names, severity). Match the surrounding style when editing.

## Key surfaces

- CLI commands: `plan` (offline), `analyze` (`--connector pgx|command`, `--exec`),
  `serve`, `version`. Global flags include `--format text|json`, `--disable-rule`, and
  per-rule thresholds (e.g. `--seqscan-rows`, `--stale-mod-ratio`).
- HTTP: `POST /v1/plan`, `POST /v1/analyze`, `GET /v1/rules`, `GET /rules`, `GET /healthz`.
- Releases are static single binaries; `gh release create` with `dist/*` + `SHA256SUMS`.
