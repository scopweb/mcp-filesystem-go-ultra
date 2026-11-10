package core

import "strings"
import "testing"

func TestPercentageRisk_LineFloorNotPercentAlone(t *testing.T) {
	thresholds := DefaultRiskThresholds()

	four := "a\nb\nc\nd\n"
	tiny := CalculateChangeImpact(four, four, "w\nx\ny\nz\n", thresholds)
	if tiny.RiskLevel != "low" {
		t.Errorf("4-line 100%% rewrite: risk=%s, want low (pct=%.1f lines=%d)",
			tiny.RiskLevel, tiny.ChangePercentage, tiny.TotalLines)
	}
	for _, factor := range tiny.RiskFactors {
		if strings.Contains(factor, "Almost complete") || strings.Contains(factor, "No matches found") {
			t.Errorf("4-line rewrite must not carry a percentage alarm: %q", factor)
		}
	}

	// Same ratio, but the edit grows the file past the touched-line floor.
	grown := CalculateChangeImpact(four, "a\nb\nc\nd", strings.Repeat("line\n", 30), thresholds)
	if grown.RiskLevel != "critical" {
		t.Errorf("4-line file expanded by 30 lines: risk=%s, want critical (pct=%.1f)",
			grown.RiskLevel, grown.ChangePercentage)
	}

	long := strings.Repeat("line-of-text\n", 50)
	full := CalculateChangeImpact(long, strings.Repeat("line-of-text\n", 48), strings.Repeat("replaced-ok\n", 48), thresholds)
	if full.RiskLevel != "critical" {
		t.Errorf("50-line rewrite: risk=%s, want critical (pct=%.1f lines=%d)",
			full.RiskLevel, full.ChangePercentage, full.TotalLines)
	}
	joined := strings.Join(full.RiskFactors, "\n")
	if !strings.Contains(joined, "50 lines") {
		t.Errorf("critical factor should name the line count, got %q", joined)
	}

	// One-line buffer: bytes, not lines, are the backstop. 400/500 = 80% stays high.
	one := strings.Repeat("A", 50) + strings.Repeat("B", 400) + strings.Repeat("C", 50)
	high := CalculateChangeImpact(one, strings.Repeat("B", 400), strings.Repeat("D", 400), thresholds)
	if high.RiskLevel != "high" {
		t.Errorf("400-byte one-line rewrite: risk=%s, want high (pct=%.1f)", high.RiskLevel, high.ChangePercentage)
	}
}
