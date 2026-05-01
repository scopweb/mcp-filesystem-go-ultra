package core

import (
	"fmt"
	"strings"
)

// ChangeImpact analiza el impacto de un cambio en un archivo
type ChangeImpact struct {
	TotalLines        int      `json:"total_lines"`
	Occurrences       int      `json:"occurrences"`
	ChangePercentage  float64  `json:"change_percentage"`
	CharactersChanged int64    `json:"characters_changed"`
	IsRisky           bool     `json:"is_risky"`
	RiskLevel         string   `json:"risk_level"` // low, medium, high, critical
	RiskFactors       []string `json:"risk_factors"`
}

// RiskThresholds define los umbrales de riesgo configurables
type RiskThresholds struct {
	MediumPercentage  float64
	HighPercentage    float64
	MediumOccurrences int
	HighOccurrences   int
}

// DefaultRiskThresholds retorna los umbrales por defecto
func DefaultRiskThresholds() RiskThresholds {
	return RiskThresholds{
		MediumPercentage:  20.0,
		HighPercentage:    75.0,
		MediumOccurrences: 50,
		HighOccurrences:   100,
	}
}

// CalculateChangeImpact analiza el impacto de una operación de edición
func CalculateChangeImpact(content, oldText, newText string, thresholds RiskThresholds) *ChangeImpact {
	// Normalize line endings so CRLF files match LF search text (Bug #23)
	content = normalizeLineEndings(content)
	oldText = normalizeLineEndings(oldText)
	newText = normalizeLineEndings(newText)

	impact := &ChangeImpact{
		TotalLines:  len(strings.Split(content, "\n")),
		Occurrences: strings.Count(content, oldText),
		RiskFactors: []string{},
	}

	// Calcular caracteres afectados
	if impact.Occurrences > 0 {
		charsRemoved := len(oldText) * impact.Occurrences
		charsAdded := len(newText) * impact.Occurrences
		impact.CharactersChanged = int64(charsRemoved + charsAdded)
	}

	// Calcular porcentaje del archivo afectado
	if len(content) > 0 {
		impact.ChangePercentage = (float64(impact.CharactersChanged) / float64(len(content))) * 100.0
	}

	// Determinar nivel de riesgo
	impact.RiskLevel = "low"
	impact.IsRisky = false

	// CRITICAL: Cambio del archivo completo
	if impact.ChangePercentage >= 90.0 {
		impact.RiskLevel = "critical"
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("⚠️ Almost complete file rewrite (%.1f%%)", impact.ChangePercentage))
	} else if impact.ChangePercentage >= thresholds.HighPercentage {
		// HIGH: Más del 50% del archivo
		impact.RiskLevel = "high"
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("⚠️ Large portion of file affected (%.1f%%)", impact.ChangePercentage))
	} else if impact.ChangePercentage >= thresholds.MediumPercentage {
		// MEDIUM: Más del 30% del archivo
		impact.RiskLevel = "medium"
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("⚠️ Significant changes (%.1f%% of file)", impact.ChangePercentage))
	}

	// Validar número de ocurrencias
	if impact.Occurrences >= thresholds.HighOccurrences {
		if impact.RiskLevel == "low" || impact.RiskLevel == "medium" {
			impact.RiskLevel = "high"
		}
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("⚠️ Very high occurrence count (%d replacements)", impact.Occurrences))
	} else if impact.Occurrences >= thresholds.MediumOccurrences {
		if impact.RiskLevel == "low" {
			impact.RiskLevel = "medium"
		}
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("⚠️ High occurrence count (%d replacements)", impact.Occurrences))
	}

	// Factores adicionales de riesgo
	if impact.Occurrences == 0 {
		impact.RiskFactors = append(impact.RiskFactors, "⚠️ No matches found - operation will have no effect")
	}

	// Detectar cambios potencialmente peligrosos
	if len(oldText) > 0 && len(newText) == 0 {
		impact.RiskFactors = append(impact.RiskFactors, "⚠️ Deletion operation (replacing with empty string)")
	}

	if len(oldText) < 10 && impact.Occurrences > 100 {
		impact.RiskFactors = append(impact.RiskFactors, "⚠️ Short pattern with many matches - verify carefully")
	}

	return impact
}

// CalculateBatchImpact analiza el impacto de múltiples operaciones
func CalculateBatchImpact(operations []BatchImpactInfo, thresholds RiskThresholds) *BatchChangeImpact {
	batchImpact := &BatchChangeImpact{
		TotalFiles:           len(operations),
		TotalOccurrences:     0,
		AverageChangePercent: 0.0,
		RiskLevel:            "low",
		IsRisky:              false,
		RiskFactors:          []string{},
		HighRiskFiles:        []string{},
	}

	if len(operations) == 0 {
		return batchImpact
	}

	var totalChangePercent float64
	highRiskCount := 0

	for _, op := range operations {
		impact := CalculateChangeImpact(op.Content, op.OldText, op.NewText, thresholds)

		batchImpact.TotalOccurrences += impact.Occurrences
		totalChangePercent += impact.ChangePercentage

		if impact.RiskLevel == "high" || impact.RiskLevel == "critical" {
			highRiskCount++
			batchImpact.HighRiskFiles = append(batchImpact.HighRiskFiles, op.FilePath)
		}
	}

	batchImpact.AverageChangePercent = totalChangePercent / float64(len(operations))

	// Determinar riesgo del batch
	if highRiskCount > len(operations)/2 {
		// Más del 50% son archivos de alto riesgo
		batchImpact.RiskLevel = "critical"
		batchImpact.IsRisky = true
		batchImpact.RiskFactors = append(batchImpact.RiskFactors,
			fmt.Sprintf("⚠️ %d of %d files are high risk", highRiskCount, len(operations)))
	} else if highRiskCount > 0 {
		batchImpact.RiskLevel = "high"
		batchImpact.IsRisky = true
		batchImpact.RiskFactors = append(batchImpact.RiskFactors,
			fmt.Sprintf("⚠️ %d high-risk files detected", highRiskCount))
	} else if len(operations) > 50 {
		// Muchos archivos afectados
		batchImpact.RiskLevel = "medium"
		batchImpact.IsRisky = true
		batchImpact.RiskFactors = append(batchImpact.RiskFactors,
			fmt.Sprintf("⚠️ Large batch operation (%d files)", len(operations)))
	} else if batchImpact.TotalOccurrences > 200 {
		// Muchas ocurrencias totales
		batchImpact.RiskLevel = "medium"
		batchImpact.IsRisky = true
		batchImpact.RiskFactors = append(batchImpact.RiskFactors,
			fmt.Sprintf("⚠️ High total occurrence count (%d)", batchImpact.TotalOccurrences))
	}

	return batchImpact
}

// BatchImpactInfo contiene información para analizar impacto de batch
type BatchImpactInfo struct {
	FilePath string
	Content  string
	OldText  string
	NewText  string
}

// BatchChangeImpact representa el impacto de operaciones batch
type BatchChangeImpact struct {
	TotalFiles           int      `json:"total_files"`
	TotalOccurrences     int      `json:"total_occurrences"`
	AverageChangePercent float64  `json:"average_change_percent"`
	IsRisky              bool     `json:"is_risky"`
	RiskLevel            string   `json:"risk_level"`
	RiskFactors          []string `json:"risk_factors"`
	HighRiskFiles        []string `json:"high_risk_files"`
}

// FormatRiskWarning genera un mensaje de advertencia formateado
func (ci *ChangeImpact) FormatRiskWarning() string {
	if !ci.IsRisky {
		return ""
	}

	var warning strings.Builder

	warning.WriteString(fmt.Sprintf("⚠️  RISK LEVEL: %s\n", strings.ToUpper(ci.RiskLevel)))
	warning.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"))
	warning.WriteString(fmt.Sprintf("Impact Analysis:\n"))
	warning.WriteString(fmt.Sprintf("  • %.1f%% of file will change\n", ci.ChangePercentage))
	warning.WriteString(fmt.Sprintf("  • %d occurrence(s) to replace\n", ci.Occurrences))
	warning.WriteString(fmt.Sprintf("  • ~%d characters affected\n\n", ci.CharactersChanged))

	if len(ci.RiskFactors) > 0 {
		warning.WriteString("Risk Factors:\n")
		for _, factor := range ci.RiskFactors {
			warning.WriteString(fmt.Sprintf("  %s\n", factor))
		}
		warning.WriteString("\n")
	}

	warning.WriteString("Recommended Actions:\n")
	warning.WriteString("  1. Use 'analyze_edit' first to see full preview\n")
	warning.WriteString("  2. Verify the change is intentional\n")
	warning.WriteString("  3. Add 'force: true' to confirm if certain\n")
	warning.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	return warning.String()
}

// FormatBatchRiskWarning genera un mensaje de advertencia para batch operations
func (bci *BatchChangeImpact) FormatBatchRiskWarning() string {
	if !bci.IsRisky {
		return ""
	}

	var warning strings.Builder

	warning.WriteString(fmt.Sprintf("⚠️  BATCH RISK LEVEL: %s\n", strings.ToUpper(bci.RiskLevel)))
	warning.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"))
	warning.WriteString(fmt.Sprintf("Batch Impact Analysis:\n"))
	warning.WriteString(fmt.Sprintf("  • %d files affected\n", bci.TotalFiles))
	warning.WriteString(fmt.Sprintf("  • %d total replacements\n", bci.TotalOccurrences))
	warning.WriteString(fmt.Sprintf("  • %.1f%% average change per file\n\n", bci.AverageChangePercent))

	if len(bci.RiskFactors) > 0 {
		warning.WriteString("Risk Factors:\n")
		for _, factor := range bci.RiskFactors {
			warning.WriteString(fmt.Sprintf("  %s\n", factor))
		}
		warning.WriteString("\n")
	}

	if len(bci.HighRiskFiles) > 0 {
		warning.WriteString("High-Risk Files:\n")
		for i, file := range bci.HighRiskFiles {
			if i >= 5 {
				warning.WriteString(fmt.Sprintf("  ... and %d more\n", len(bci.HighRiskFiles)-5))
				break
			}
			warning.WriteString(fmt.Sprintf("  • %s\n", file))
		}
		warning.WriteString("\n")
	}

	warning.WriteString("Recommended Actions:\n")
	warning.WriteString("  1. Use 'validate_only: true' to preview all changes\n")
	warning.WriteString("  2. Review high-risk files carefully\n")
	warning.WriteString("  3. Add 'force: true' to proceed if certain\n")
	warning.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	return warning.String()
}

// ShouldBlockOperation determines if an operation should be blocked.
// Returns false for all risk levels — blocking is disabled (Bug #22: always warn, never block).
// MEDIUM, HIGH, and CRITICAL risk auto-proceed with backup + warning.
// Use force: true only to bypass interactive confirmation prompts, not to override blocking.
func (ci *ChangeImpact) ShouldBlockOperation(force bool) bool {
	return false
}

// FormatRiskNotice generates a non-blocking, actionable warning appended to success responses.
// Used for MEDIUM and HIGH risk operations that auto-proceed with backup (Bug #16).
// For HIGH/CRITICAL risk: add actionable VERIFY instruction
func (ci *ChangeImpact) FormatRiskNotice(backupID string, filePath ...string) string {
	if !ci.IsRisky || ci.RiskLevel == "low" {
		return ""
	}

	var notice strings.Builder

	notice.WriteString(fmt.Sprintf("\n⚠️ %s RISK (%.0f%% changed)", strings.ToUpper(ci.RiskLevel), ci.ChangePercentage))

	// For HIGH/CRITICAL risk: add actionable VERIFY instruction
	if ci.RiskLevel == "high" || ci.RiskLevel == "critical" {
		if len(filePath) > 0 && filePath[0] != "" {
			notice.WriteString(fmt.Sprintf("\n⚠️ VERIFY: read_file(\"%s\", mode:\"tail\") to confirm file is complete", filePath[0]))
		} else {
			notice.WriteString("\n⚠️ VERIFY: read_file with mode:\"tail\" to confirm file is complete")
		}
	}
	notice.WriteString("\n")

	return notice.String()
}

// GetRecommendation returns a recommendation based on risk level
func (ci *ChangeImpact) GetRecommendation() string {
	switch ci.RiskLevel {
	case "critical":
		return "CRITICAL risk - auto-backup created. Use backup(action:\"undo_last\") to revert if needed. VERIFY: read_file(mode:\"tail\") to confirm file is complete."
	case "high":
		return "HIGH risk - auto-backup created. Use backup(action:\"undo_last\") to revert if needed. VERIFY: read_file(mode:\"tail\") to confirm file is complete."
	case "medium":
		return "MEDIUM risk - auto-backup created. Use backup(action:\"undo_last\") to revert if needed."
	default:
		return "Low risk operation."
	}
}
