package core

import (
	"strings"
	"testing"
)

func TestPathFloorFor_Table(t *testing.T) {
	medium := DefaultRiskThresholds().MediumPercentage
	cases := []struct {
		path string
		pct  float64
		want string
		why  string
	}{
		{`C:\proj\go.mod`, 1, "high", "go.mod"},
		{`C:\proj\go.sum`, 1, "high", "go.sum"},
		{`C:\proj\App.csproj`, 1, "high", "csproj"},
		{`C:\proj\Dockerfile`, 1, "high", "dockerfile"},
		{`C:\proj\.github\workflows\ci.yml`, 1, "high", "github workflow"},
		{`C:\proj\foo_test.go`, 1, "", "test file"},
		{`C:\proj\internal\pkg\x.go`, 25, "medium", "package code"},
		{`C:\proj\cmd\app\main.go`, 25, "medium", "package code"},
		{`C:\proj\internal\pkg\x.go`, 5, "", ""},
		{`C:\proj\README.md`, 25, "", "docs"},
		{`C:\proj\docs\guide.md`, 25, "", "docs"},
		{`C:\proj\.env`, 1, "", ""},
	}
	for _, tc := range cases {
		floor, reason := PathFloorFor(tc.path, tc.pct, medium)
		if floor != tc.want || reason != tc.why {
			t.Errorf("%s pct=%.0f: floor=%q reason=%q want %q %q", tc.path, tc.pct, floor, reason, tc.want, tc.why)
		}
	}
}

func lowImpact() *ChangeImpact {
	body := strings.Repeat("word ", 40) + "NEEDLE\n"
	return CalculateChangeImpact(body, "NEEDLE", "X", DefaultRiskThresholds())
}

func TestApplyPathFloor_RaisesLowToHigh(t *testing.T) {
	impact := lowImpact()
	if impact.RiskLevel != "low" {
		t.Fatalf("setup: risk=%s pct=%.1f", impact.RiskLevel, impact.ChangePercentage)
	}
	ApplyPathFloor(impact, `C:\proj\go.mod`, DefaultRiskThresholds())
	if impact.RiskLevel != "high" || impact.PathFloor != "high" || !impact.IsRisky {
		t.Fatalf("%+v", impact)
	}
}

func TestApplyPathFloor_TestFileDoesNotRaise(t *testing.T) {
	impact := lowImpact()
	ApplyPathFloor(impact, `C:\proj\foo_test.go`, DefaultRiskThresholds())
	if impact.RiskLevel != "low" || impact.PathFloor != "" {
		t.Fatalf("%+v", impact)
	}
	if len(impact.Reasons) != 1 || impact.Reasons[0] != "test file" {
		t.Fatalf("reasons=%v", impact.Reasons)
	}
}

func TestApplyPathFloor_SecretNoDoubleRisk(t *testing.T) {
	impact := lowImpact()
	ApplyPathFloor(impact, `C:\proj\.env`, DefaultRiskThresholds())
	if impact.PathFloor != "" || impact.RiskLevel != "low" {
		t.Fatalf("%+v", impact)
	}
}
