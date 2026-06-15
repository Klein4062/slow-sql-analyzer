package rules

import (
	"fmt"
	"sort"
	"time"

	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// StaleStatistics flags relations whose planner statistics are stale: either a
// large fraction of rows changed since the last ANALYZE (so the planner's
// estimates are likely off), or the table has never been analyzed at all.
//
// 触发条件：自上次 ANALYZE 以来修改行数占比 >= 阈值（默认 10%，且绝对量 >= 1000），
// 或有数据却从未 ANALYZE。统计新鲜度数据仅在实时 pgx 模式下可用（PlanResult.TableStats）；
// 离线/命令连接器模式下 TableStats 为空，本规则不输出。
type StaleStatistics struct{}

// Name implements analyzer.Rule.
func (StaleStatistics) Name() string { return "StaleStatistics" }

// Analyze implements analyzer.Rule.
func (StaleStatistics) Analyze(ctx *analyzer.AnalysisContext) []analyzer.Finding {
	if ctx.Result == nil || len(ctx.Result.TableStats) == 0 {
		return nil // 无统计新鲜度数据（离线/命令模式），直接跳过。
	}
	var out []analyzer.Finding
	t := ctx.Thresholds

	// 按表名排序，保证输出稳定。
	keys := make([]string, 0, len(ctx.Result.TableStats))
	for k := range ctx.Result.TableStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		ts := ctx.Result.TableStats[k]
		stale, ratio, reason := assessStale(ts, t)
		if !stale {
			continue
		}

		severity := analyzer.SeverityWarning
		if ratio >= 0.5 || (reason == "never analyzed" && ts.LiveTuples > 0) {
			severity = analyzer.SeverityCritical
		}

		out = append(out, analyzer.Finding{
			Severity:       severity,
			Rule:           "StaleStatistics",
			NodeLabel:      "Statistics for " + ts.QualifiedName(),
			NodePath:       ts.QualifiedName(),
			NodeType:       "TableStatistics",
			RelationName:   ts.QualifiedName(),
			Problem:        describeProblem(ts, ratio, reason),
			Recommendation: fmt.Sprintf("run ANALYZE %s to refresh planner statistics", ts.QualifiedName()),
			Evidence: map[string]any{
				"live_tuples":       ts.LiveTuples,
				"mod_since_analyze": ts.ModSinceAnalyze,
				"mod_ratio":         ratio,
				"last_analyze":      zeroTimeStr(ts.LastAnalyze),
				"last_autoanalyze":  zeroTimeStr(ts.LastAutoAnalyze),
				"reason":            reason,
			},
		})
	}
	return out
}

// assessStale decides whether a table's stats are stale. Returns the (stale,
// modification-ratio, human-readable reason).
func assessStale(ts plan.TableStat, t config.Thresholds) (bool, float64, string) {
	never := !ts.Analyzed()
	if never && ts.LiveTuples > 0 {
		// 从未 ANALYZE 且表里有数据 → 统计必然缺失/不准。
		return true, 1.0, "never analyzed"
	}
	if ts.LiveTuples <= 0 {
		return false, 0, ""
	}
	ratio := float64(ts.ModSinceAnalyze) / float64(ts.LiveTuples)
	// 同时要求比例达标且绝对量过门槛，避免小表噪声。
	if ratio >= t.StaleModRatio && ts.ModSinceAnalyze >= int64(t.StaleMinMods) {
		return true, ratio, fmt.Sprintf("%.0f%% of rows changed since last ANALYZE", ratio*100)
	}
	return false, ratio, ""
}

func describeProblem(ts plan.TableStat, ratio float64, reason string) string {
	if reason == "never analyzed" {
		return fmt.Sprintf(
			"%s has never been ANALYZEd (live tuples ~%s); the planner is working without fresh statistics",
			ts.QualifiedName(), formatRows(float64(ts.LiveTuples)),
		)
	}
	return fmt.Sprintf(
		"%s statistics are stale: %s (~%s of %s live tuples modified); planner estimates are likely off",
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
