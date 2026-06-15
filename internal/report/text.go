package report

import (
	"fmt"
	"os"
	"strings"

	"github.com/Klein4062/slow-sql-analyzer/internal/advise"
	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
	"github.com/fatih/color"
	isatty "github.com/mattn/go-isatty"
)

// isColorEnabled reports whether stdout is a terminal (so ANSI codes are
// appropriate). fatih/color honors color.NoColor, set by the caller.
func isColorEnabled() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

// severity colors / styles.
var (
	criticalStyle = color.New(color.FgRed, color.Bold)
	warningStyle  = color.New(color.FgYellow)
	infoStyle     = color.New(color.FgCyan)
	titleStyle    = color.New(color.Bold)
	headerStyle   = color.New(color.FgGreen, color.Bold)
	dimStyle      = color.New(color.Faint)
	sqlStyle      = color.New(color.FgGreen)
)

// RenderText produces a human-readable analysis report.
func RenderText(m Model) string {
	color.NoColor = m.NoColor || !isColorEnabled()
	var b strings.Builder

	renderHeader(&b, m)
	renderSummary(&b, m)
	renderPlanTree(&b, m)
	renderFindings(&b, m)
	renderActions(&b, m)

	return b.String()
}

func renderHeader(b *strings.Builder, m Model) {
	titleStyle.Fprintf(b, "PostgreSQL Plan Analysis")
	b.WriteString("\n")
	if m.Query != "" {
		dimStyle.Fprintf(b, "query: %s\n", truncate(singleLine(m.Query), 120))
	}
	b.WriteString(strings.Repeat("─", 60))
	b.WriteString("\n")
}

func renderSummary(b *strings.Builder, m Model) {
	if m.Result == nil {
		return
	}
	var exec, planTime string
	if m.Result.ExecutionTime > 0 {
		exec = fmt.Sprintf("%.2f ms", m.Result.ExecutionTime)
	}
	if m.Result.PlanningTime > 0 {
		planTime = fmt.Sprintf("%.2f ms", m.Result.PlanningTime)
	}

	fmt.Fprintf(b, "execution time: %s   planning time: %s\n", exec, planTime)
	if m.HasAnalyze() {
		dimStyle.Fprintf(b, "plan includes actual runtime stats (ANALYZE)\n")
	} else {
		warningStyle.Fprintf(b, "plan has ESTIMATES ONLY — rules needing runtime stats are disabled\n")
	}

	counts := map[analyzer.Severity]int{}
	for _, f := range m.Findings {
		counts[f.Severity]++
	}
	fmt.Fprintf(b, "findings: %s critical, %s warning, %s info\n\n",
		criticalStyle.Sprint(counts[analyzer.SeverityCritical]),
		warningStyle.Sprint(counts[analyzer.SeverityWarning]),
		infoStyle.Sprint(counts[analyzer.SeverityInfo]),
	)
}

// findingsByPath indexes findings by their node path for tree annotation.
func findingsByPath(findings []analyzer.Finding) map[string][]analyzer.Finding {
	idx := map[string][]analyzer.Finding{}
	for _, f := range findings {
		idx[f.NodePath] = append(idx[f.NodePath], f)
	}
	return idx
}

func renderPlanTree(b *strings.Builder, m Model) {
	headerStyle.Fprintf(b, "Plan tree\n")
	if m.Result == nil || m.Result.Root == nil {
		dimStyle.Fprintf(b, "  (no plan)\n\n")
		return
	}
	idx := findingsByPath(m.Findings)
	plan.WalkPath(m.Result.Root, func(node, parent *plan.PlanNode, depth int, path []string) bool {
		indent := strings.Repeat("  ", depth)
		glyph := " "
		if fs := idx[strings.Join(path, " > ")]; len(fs) > 0 {
			glyph = topSeverity(fs).Glyph()
		}
		fmt.Fprintf(b, "%s%s %s %s\n", indent, glyph, dimStyle.Sprint("→"), node.Label())
		dimStyle.Fprintf(b, "%s    %s\n", indent, nodeStats(node))
		return true
	})
	b.WriteString("\n")
}

// topSeverity returns the highest severity among findings.
func topSeverity(fs []analyzer.Finding) analyzer.Severity {
	top := analyzer.SeverityInfo
	for _, f := range fs {
		if f.Severity > top {
			top = f.Severity
		}
	}
	return top
}

func nodeStats(n *plan.PlanNode) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("rows est=%s", fmtRows(n.PlanRows)))
	if n.HasActual() {
		parts = append(parts, fmt.Sprintf("act=%s", fmtRows(n.ActualRows)))
		if n.ActualLoops > 1 {
			parts = append(parts, fmt.Sprintf("loops=%s", fmtRows(n.ActualLoops)))
		}
		if n.ActualTotalTime > 0 {
			parts = append(parts, fmt.Sprintf("%.2fms", n.ActualTotalTime))
		}
	}
	if n.TotalCost > 0 {
		parts = append(parts, fmt.Sprintf("cost=%.1f", n.TotalCost))
	}
	return strings.Join(parts, "  ")
}

func renderFindings(b *strings.Builder, m Model) {
	headerStyle.Fprintf(b, "Findings (%d)\n", len(m.Findings))
	if len(m.Findings) == 0 {
		dimStyle.Fprintf(b, "  none — no anti-patterns detected\n\n")
		return
	}
	for i, f := range m.Findings {
		var style *color.Color
		switch f.Severity {
		case analyzer.SeverityCritical:
			style = criticalStyle
		case analyzer.SeverityWarning:
			style = warningStyle
		default:
			style = infoStyle
		}
		fmt.Fprintf(b, "%s [%s] %s\n",
			style.Sprint(f.Severity.Glyph()),
			style.Sprint(strings.ToUpper(f.Rule)),
			f.NodeLabel,
		)
		dimStyle.Fprintf(b, "  path: %s\n", or(f.NodePath, "(root)"))
		fmt.Fprintf(b, "  problem:        %s\n", f.Problem)
		fmt.Fprintf(b, "  recommendation: %s\n", f.Recommendation)
		if i < len(m.Findings)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
}

func renderActions(b *strings.Builder, m Model) {
	if len(m.Actions) == 0 {
		return
	}
	headerStyle.Fprintf(b, "Suggested actions\n")
	lastKind := advise.ActionKind("")
	for _, a := range m.Actions {
		if a.Kind != lastKind {
			dimStyle.Fprintf(b, "# %s\n", a.Kind.Describe())
			lastKind = a.Kind
		}
		sqlStyle.Fprintf(b, "  %s\n", a.SQL)
	}
	b.WriteString("\n")
	dimStyle.Fprintf(b, "Index suggestions are heuristic — verify against your schema and workload.\n")
}

// --- small helpers ---

func fmtRows(n float64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", n/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", n/1e3)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func singleLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
