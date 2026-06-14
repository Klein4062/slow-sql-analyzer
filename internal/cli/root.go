// Package cli wires the cobra command tree for the slow-sql-analyzer binary.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Klein4062/slow-sql-analyzer/internal/advise"
	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/report"
	"github.com/Klein4062/slow-sql-analyzer/internal/rules"
	"github.com/Klein4062/slow-sql-analyzer/internal/source"
)

// Version is the binary version, overridable at build time with -ldflags.
var Version = "0.1.0"

// Global flag values, populated from the root command's persistent flags.
var (
	flagFormat       string
	flagNoColor      bool
	flagDisableRules []string

	flagSeqScanRows   float64
	flagCardRatio     float64
	flagFilterRemoval float64
	flagBufferHitRatio float64
)

// NewRootCmd builds the command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "slow-sql-analyzer",
		Short:         "Analyze PostgreSQL query plans for optimality and suggest fixes",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVar(&flagFormat, "format", "text", "output format: text|json")
	pf.BoolVar(&flagNoColor, "no-color", false, "disable ANSI color in text output")
	pf.StringSliceVar(&flagDisableRules, "disable-rule", nil, "disable a rule by name (repeatable)")

	pf.Float64Var(&flagSeqScanRows, "seqscan-rows", 1000, "row threshold to flag a Seq Scan as a large-table scan")
	pf.Float64Var(&flagCardRatio, "cardinality-ratio", 10, "actual-vs-estimated row ratio that flags a misestimate")
	pf.Float64Var(&flagFilterRemoval, "filter-removal-ratio", 0.9, "fraction of rows a filter must discard to flag an inefficient scan")
	pf.Float64Var(&flagBufferHitRatio, "buffer-hit-ratio", 0.9, "minimum shared-buffer hit ratio (below this is flagged)")

	root.AddCommand(newPlanCmd(), newAnalyzeCmd(), newServeCmd(), newVersionCmd())
	return root
}

// buildConfig maps the global flags to a config.Config, starting from defaults
// so unexposed thresholds keep their documented values.
func buildConfig() config.Config {
	c := config.Default()
	c.Thresholds.SeqScanRowThreshold = flagSeqScanRows
	c.Thresholds.CardinalityRatio = flagCardRatio
	c.Thresholds.FilterRemovalRatio = flagFilterRemoval
	c.Thresholds.BufferHitRatioMin = flagBufferHitRatio
	c.Options.NoColor = flagNoColor
	for _, r := range flagDisableRules {
		c.Options.DisabledRules[r] = true
	}
	return c
}

// runAnalysis fetches a plan from src, runs every enabled rule, derives
// actions, and writes the rendered report to out.
func runAnalysis(out io.Writer, src source.PlanSource, query string) error {
	result, err := src.Fetch()
	if err != nil {
		return err
	}
	cfg := buildConfig()
	a := analyzer.New(rules.Default())
	rep := a.Run(result, cfg)

	model := report.Model{
		Result:   result,
		Findings: rep.Findings,
		Actions:  advise.Actions(rep.Findings),
		NoColor:  cfg.Options.NoColor,
		Query:    query,
	}

	switch flagFormat {
	case "json":
		data, err := report.RenderJSON(model)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
	default:
		fmt.Fprint(out, report.RenderText(model))
	}
	return nil
}
