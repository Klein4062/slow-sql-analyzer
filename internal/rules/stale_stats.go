package rules

import (
	"fmt"
	"sort"
	"time"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// StaleStatistics flags relations whose planner statistics are likely stale or
// insufficient, and recommends ANALYZE. It uses two complementary signals:
//
//  1. Live (pgx) mode — PlanResult.TableStats is populated from
//     pg_stat_user_tables. A table is flagged when rows modified since the last
//     ANALYZE exceed a fraction of live tuples, or it was never analyzed. This
//     is the "cause" signal (high confidence). When the table ALSO shows a
//     cardinality misestimate in the plan, severity is bumped to critical
//     (cause + symptom agree).
//
//  2. Offline / command-connector mode — no catalog access, so we fall back to
//     the plan's estimated-vs-actual row mismatch at base-table scans as an
//     indirect "the stats are probably the problem" signal (lower confidence,
//     clearly labeled as inferred).
//
// 触发：实时模式按 n_mod_since_analyze（病因）+ 与基数偏差交叉印证；
// 离线模式退化为「scan 节点估算 vs 实际偏差」推断（低置信）。
type StaleStatistics struct{}

// Name implements analyzer.Rule.
func (StaleStatistics) Name() string { return "StaleStatistics" }

// Analyze implements analyzer.Rule.
func (StaleStatistics) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	if ctx.Result == nil || ctx.Result.Root == nil {
		return nil
	}
	misByRel := scanMisestimateByRelation(ctx.Result.Root, ctx.Thresholds)

	// 有 pg_stat 数据（实时 pgx 模式）：病因驱动 + 交叉印证。
	if len(ctx.Result.TableStats) > 0 {
		return liveStaleFindings(ctx.Result.TableStats, misByRel, ctx.Thresholds)
	}
	// 离线/命令模式：无系统视图，退化为基数偏差推断。
	return offlineStaleFindings(misByRel)
}

// --- live mode: pg_stat-driven (cause) + cardinality cross-reference ---

func liveStaleFindings(stats map[string]plan.TableStat, misByRel map[string]scanMis, t config.Thresholds) []analyzer.Finding {
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []analyzer.Finding
	for _, k := range keys {
		ts := stats[k]
		stale, ratio, reason := assessStale(ts, t)
		if !stale {
			continue
		}

		severity := analyzer.SeverityWarning
		if ratio >= 0.5 || (reason == "never" && ts.LiveTuples > 0) {
			severity = analyzer.SeverityCritical
		}

		problem := describeProblem(ts, ratio, reason)
		ev := map[string]any{
			"live_tuples":       ts.LiveTuples,
			"mod_since_analyze": ts.ModSinceAnalyze,
			"mod_ratio":         ratio,
			"last_analyze":      zeroTimeStr(ts.LastAnalyze),
			"last_autoanalyze":  zeroTimeStr(ts.LastAutoAnalyze),
			"reason":            reason,
			"mode":              "pg_stat",
		}

		// 交叉印证：该表在计划里也出现基数偏差 → 病因+症状吻合，升 critical。
		if mis, ok := misByRel[ts.Relation]; ok {
			severity = analyzer.SeverityCritical
			problem += fmt.Sprintf("；计划里的基数偏差（%.0f 倍）印证了这一点", mis.Ratio)
			ev["confirmed_by_cardinality"] = true
			ev["cardinality_ratio"] = mis.Ratio
		}

		out = append(out, analyzer.Finding{
			Severity:       severity,
			Rule:           "StaleStatistics",
			NodeLabel:      ts.QualifiedName() + " 的统计信息",
			NodePath:       ts.QualifiedName(),
			NodeType:       "TableStatistics",
			RelationName:   ts.QualifiedName(),
			Problem:        problem,
			Recommendation: fmt.Sprintf("执行 ANALYZE %s 刷新规划器统计", ts.QualifiedName()),
			Evidence:       ev,
		})
	}
	return out
}

// --- offline fallback: inferred from estimated-vs-actual mismatch ---

func offlineStaleFindings(misByRel map[string]scanMis) []analyzer.Finding {
	if len(misByRel) == 0 {
		return nil
	}
	keys := make([]string, 0, len(misByRel))
	for k := range misByRel {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []analyzer.Finding
	for _, k := range keys {
		mis := misByRel[k]
		out = append(out, analyzer.Finding{
			Severity:     analyzer.SeverityWarning,
			Rule:         "StaleStatistics",
			NodeLabel:    mis.Qualified + " 的统计信息",
			NodePath:     mis.Qualified,
			NodeType:     "TableStatistics",
			RelationName: mis.Qualified,
			Problem: fmt.Sprintf(
				"%s：估算 %s 行，实际 %s 行（偏差约 %.0f 倍）——统计可能已过时或不足",
				mis.Qualified, formatRows(mis.Estimate), formatRows(mis.Actual), mis.Ratio,
			),
			Recommendation: "执行 ANALYZE 刷新统计；若谓词列相关，再用 CREATE STATISTICS" +
				"（由计划推断——无实时系统表数据）",
			Evidence: map[string]any{
				"mode":              "inferred",
				"estimated_rows":    mis.Estimate,
				"actual_rows":       mis.Actual,
				"cardinality_ratio": mis.Ratio,
			},
		})
	}
	return out
}

// scanMisestimateByRelation walks base-table scan nodes and, for each relation
// whose estimated-vs-actual rows cross the cardinality threshold, records the
// worst mismatch. Keyed by bare relation name (to cross-reference TableStats).
// 遍历基础表 scan 节点，按表记录最严重的基数偏差（用于交叉印证与离线回退）。
func scanMisestimateByRelation(root *plan.PlanNode, t config.Thresholds) map[string]scanMis {
	m := map[string]scanMis{}
	plan.ForEach(root, func(node, parent *plan.PlanNode, depth int) {
		if node.RelationName == "" || !node.IsScan() {
			return
		}
		ratio, ok := cardinalityRatio(node, t)
		if !ok {
			return
		}
		if prev, exists := m[node.RelationName]; !exists || ratio > prev.Ratio {
			m[node.RelationName] = scanMis{
				Ratio:     ratio,
				Qualified: node.QualifiedName(),
				Estimate:  node.PlanRows,
				Actual:    node.ActualRows,
			}
		}
	})
	return m
}

// scanMis carries the worst cardinality mismatch observed for a relation.
type scanMis struct {
	Ratio     float64
	Qualified string
	Estimate  float64
	Actual    float64
}

// assessStale decides whether a table's stats are stale (live mode). Returns
// (stale, modification-ratio, reason). The reason "never" is a sentinel for
// never-analyzed; otherwise it is a human-readable Chinese summary.
// reason 用 "never" 作为「从未分析」的哨兵值，其余为中文摘要。
func assessStale(ts plan.TableStat, t config.Thresholds) (bool, float64, string) {
	never := !ts.Analyzed()
	if never && ts.LiveTuples > 0 {
		return true, 1.0, "never"
	}
	if ts.LiveTuples <= 0 {
		return false, 0, ""
	}
	ratio := float64(ts.ModSinceAnalyze) / float64(ts.LiveTuples)
	if ratio >= t.StaleModRatio && ts.ModSinceAnalyze >= int64(t.StaleMinMods) {
		return true, ratio, fmt.Sprintf("自上次 ANALYZE 以来 %.0f%% 的行已变更", ratio*100)
	}
	return false, ratio, ""
}

func describeProblem(ts plan.TableStat, ratio float64, reason string) string {
	if reason == "never" {
		return fmt.Sprintf(
			"%s 从未执行过 ANALYZE（活元组约 %s）；规划器在没有新鲜统计的情况下工作",
			ts.QualifiedName(), formatRows(float64(ts.LiveTuples)),
		)
	}
	return fmt.Sprintf(
		"%s 统计已过时：%s（约 %s / %s 活元组被修改）；规划器估算很可能不准",
		ts.QualifiedName(), reason,
		formatRows(float64(ts.ModSinceAnalyze)), formatRows(float64(ts.LiveTuples)),
	)
}

// zeroTimeStr renders a time as RFC3339, or "never" for the zero value.
func zeroTimeStr(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format(time.RFC3339)
}
