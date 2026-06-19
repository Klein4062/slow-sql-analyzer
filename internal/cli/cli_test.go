package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Klein4062/slow-sql-analyzer/internal/source"
)

// TestBuildConfigMapping exercises the global flag vars -> config mapping.
func TestBuildConfigMapping(t *testing.T) {
	flagFormat = "json"
	flagNoColor = true
	flagDisableRules = []string{"SeqScanLargeTable", "Hotspot"}
	flagSeqScanRows = 500
	flagCardRatio = 25
	flagFilterRemoval = 0.8
	flagBufferHitRatio = 0.95
	flagStaleModRatio = 0.2

	c := buildConfig()
	if c.Options.NoColor != true {
		t.Error("NoColor not mapped")
	}
	if !c.IsRuleEnabled("CardinalityMisestimate") {
		t.Error("non-disabled rule should be enabled")
	}
	if c.IsRuleEnabled("SeqScanLargeTable") || c.IsRuleEnabled("Hotspot") {
		t.Error("disabled rules should be disabled")
	}
	if c.Thresholds.SeqScanRowThreshold != 500 {
		t.Errorf("SeqScanRowThreshold = %v", c.Thresholds.SeqScanRowThreshold)
	}
	if c.Thresholds.CardinalityRatio != 25 {
		t.Errorf("CardinalityRatio = %v", c.Thresholds.CardinalityRatio)
	}
	if c.Thresholds.FilterRemovalRatio != 0.8 {
		t.Errorf("FilterRemovalRatio = %v", c.Thresholds.FilterRemovalRatio)
	}
	if c.Thresholds.BufferHitRatioMin != 0.95 {
		t.Errorf("BufferHitRatioMin = %v", c.Thresholds.BufferHitRatioMin)
	}
	if c.Thresholds.StaleModRatio != 0.2 {
		t.Errorf("StaleModRatio = %v", c.Thresholds.StaleModRatio)
	}
	// unexposed thresholds keep defaults.
	if c.Thresholds.NestedLoopMinLoops != 10 {
		t.Errorf("unexposed NestedLoopMinLoops should keep default 10, got %v", c.Thresholds.NestedLoopMinLoops)
	}
}

func TestVersionCmd(t *testing.T) {
	var b bytes.Buffer
	cmd := newVersionCmd()
	cmd.SetOut(&b)
	cmd.Run(cmd, nil)
	if !strings.Contains(b.String(), Version) {
		t.Errorf("version output %q missing %q", b.String(), Version)
	}
}

func TestBuildLiveSourceErrors(t *testing.T) {
	// pgx without dsn -> error.
	if _, err := buildLiveSource("pgx", "", "", "SELECT 1", false, false, 0); err == nil {
		t.Error("pgx without dsn -> error")
	}
	// command without exec -> error.
	if _, err := buildLiveSource("command", "", "", "SELECT 1", false, false, 0); err == nil {
		t.Error("command without exec -> error")
	}
	// unknown connector -> error.
	if _, err := buildLiveSource("weird", "", "", "SELECT 1", false, false, 0); err == nil {
		t.Error("unknown connector -> error")
	}
	// exec implies command connector even if connector=pgx (no error, source returned).
	src, err := buildLiveSource("pgx", "psql {dsn}", "", "SELECT 1", false, false, 0)
	if err != nil || src == nil {
		t.Fatalf("exec should select command connector (no err, non-nil); err=%v src=%v", err, src)
	}
}

// TestRunAnalysis exercises the CLI's analyze->render pipeline (text + json)
// via an offline FileSource, covering runAnalysis and the flag->config wiring.
func TestRunAnalysis(t *testing.T) {
	planPath := filepath.Join("..", "..", "testdata", "seqscan_large.json")

	// text output
	flagFormat = "text"
	flagDisableRules = nil
	flagNoColor = true
	var b bytes.Buffer
	if err := runAnalysis(&b, source.FileSource{Path: planPath}, "SELECT 1"); err != nil {
		t.Fatalf("runAnalysis text: %v", err)
	}
	if !strings.Contains(b.String(), "执行计划分析") || !strings.Contains(b.String(), "SEQSCANLARGETABLE") {
		t.Errorf("text output missing expected sections: %q", b.String())
	}

	// json output
	flagFormat = "json"
	b.Reset()
	if err := runAnalysis(&b, source.FileSource{Path: planPath}, "SELECT 1"); err != nil {
		t.Fatalf("runAnalysis json: %v", err)
	}
	if !strings.Contains(b.String(), `"findings"`) || !strings.Contains(b.String(), `"summary"`) {
		t.Errorf("json output missing expected keys: %q", b.String())
	}
}
