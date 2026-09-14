package core

import (
	"path/filepath"
	"strings"
)

func riskRank(level string) int {
	switch strings.ToLower(level) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func slashLower(path string) string {
	return strings.ToLower(filepath.ToSlash(path))
}

func pathHasDir(slashPath, dir string) bool {
	return strings.Contains(slashPath, "/"+dir+"/") || strings.HasPrefix(slashPath, dir+"/")
}

func PathFloorFor(path string, changePercent, mediumPercent float64) (floor, reason string) {
	if path == "" || IsSecretPath(path) {
		return "", ""
	}
	n := slashLower(path)
	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(base, "_test.go") {
		return "", "test file"
	}
	if strings.Contains(n, "/.github/workflows/") || strings.HasPrefix(n, ".github/workflows/") {
		return "high", "github workflow"
	}
	if base == "dockerfile" {
		return "high", "dockerfile"
	}
	if base == "go.mod" || base == "go.sum" {
		return "high", base
	}
	if strings.HasSuffix(base, ".csproj") {
		return "high", "csproj"
	}
	if strings.HasPrefix(base, "readme") || pathHasDir(n, "docs") {
		return "", "docs"
	}
	if pathHasDir(n, "cmd") || pathHasDir(n, "internal") {
		if changePercent >= mediumPercent {
			return "medium", "package code"
		}
		return "", ""
	}
	return "", ""
}

func ApplyPathFloor(impact *ChangeImpact, path string, thresholds RiskThresholds) {
	if impact == nil {
		return
	}
	if thresholds.MediumPercentage == 0 {
		thresholds = DefaultRiskThresholds()
	}
	floor, reason := PathFloorFor(path, impact.ChangePercentage, thresholds.MediumPercentage)
	impact.PathFloor = floor
	if reason != "" {
		impact.Reasons = append(impact.Reasons, reason)
	}
	if floor == "" {
		return
	}
	if riskRank(floor) > riskRank(impact.RiskLevel) {
		impact.RiskLevel = floor
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors, "path floor: "+reason)
	}
}
