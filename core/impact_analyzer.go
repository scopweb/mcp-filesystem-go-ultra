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

	// Bytes affected by the edit. Honest metric: per occurrence, the
	// scope of the change is bounded by max(len(oldText), len(newText))
	// — that is the size of the region this edit intentionally touches.
	//
	// The previous formula used (oldLen + newLen) * occurrences, which
	// double-counted every byte and produced inflated percentages. For
	// example, replacing 100 bytes with 100 bytes was reported as 200
	// bytes "changed" — exactly twice the real footprint. Combined with
	// the byte-by-byte alignment count in calculateMultiEditImpact (which
	// counts every shifted byte after an insertion as "changed"), small
	// targeted edits routinely tripped the CRITICAL threshold and emitted
	// alarmist warnings that did not reflect the actual edit scope.
	if impact.Occurrences > 0 {
		perOccurrence := len(oldText)
		if len(newText) > perOccurrence {
			perOccurrence = len(newText)
		}
		impact.CharactersChanged = int64(perOccurrence * impact.Occurrences)
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

// IsSmallFile returns true when the file is too small for percentage-based risk metrics.
// For small files, percentage changes are meaningless (e.g., 1 line → 36 lines = 3500% but trivial).
func (ci *ChangeImpact) IsSmallFile() bool {
	return ci.TotalLines < 10 && ci.CharactersChanged < 200
}

// Visibility threshold for FormatRiskNotice. Edits whose scope is below
// this percentage of the file are silenced entirely — the backup still
// exists, but routine targeted edits no longer emit any notice.
const noticeMinPercent = 10.0

// Verify-hint threshold. At or above this percentage of the file, the
// notice appends a non-imperative "verify with read_file(mode:\"tail\")
// if needed" suggestion. Below this, the suggestion is omitted to avoid
// seeding doubt about edits whose footprint is moderate.
const noticeVerifyHintPercent = 40.0

// FormatRiskNotice generates a non-blocking, INFORMATIONAL note appended to
// success responses. Tone: factual, lowercase, no panic vocabulary.
//
// Design notes (the user explicitly chose this shape):
//   - Word "RISK" and emoji ⚠️ are deliberately gone. They produced reflexive
//     undo behaviour against edits that were perfectly fine — the previous
//     formula counted every shifted byte as "changed" and tripped CRITICAL
//     for routine 3-replacement edits.
//   - The verify hint is preserved (option C in the discussion) but reframed
//     as a conditional suggestion ("if needed") and only at >=40%, so it
//     stops appearing on every medium-sized edit.
//   - Below 10% the notice is silenced entirely. The backup record is still
//     written; absence of a notice is not absence of a backup.
func (ci *ChangeImpact) FormatRiskNotice(backupID string, filePath ...string) string {
	if !ci.IsRisky || ci.RiskLevel == "low" {
		return ""
	}

	var notice strings.Builder

	if ci.IsSmallFile() {
		// Small file: percentage is meaningless. Report counts honestly.
		notice.WriteString(fmt.Sprintf("\nnote: edit on small file (%d lines, ~%d bytes affected, %d replacements)",
			ci.TotalLines, ci.CharactersChanged, ci.Occurrences))
		if backupID != "" {
			notice.WriteString(fmt.Sprintf(". backup:%s", backupID))
		}
		notice.WriteString("\n")
		return notice.String()
	}

	// Below the visibility threshold the notice is suppressed entirely.
	if ci.ChangePercentage < noticeMinPercent {
		return ""
	}

	// Pick a neutral magnitude word from the percentage. We deliberately do
	// NOT echo the internal RiskLevel ("critical", "high") because those
	// strings carry pre-existing alarm semantics for both human and AI
	// readers. The percentage and byte count speak for themselves.
	magnitude := "edit"
	switch {
	case ci.ChangePercentage >= 80.0:
		magnitude = "very large edit"
	case ci.ChangePercentage >= noticeVerifyHintPercent:
		magnitude = "large edit"
	}

	notice.WriteString(fmt.Sprintf("\nnote: %s (~%d bytes affected, %d replacements, %.0f%% of file)",
		magnitude, ci.CharactersChanged, ci.Occurrences, ci.ChangePercentage))

	if backupID != "" {
		notice.WriteString(fmt.Sprintf(". backup:%s", backupID))
	}

	// Verify hint only at or above the configured threshold. Conditional
	// phrasing ("if needed"), not imperative.
	if ci.ChangePercentage >= noticeVerifyHintPercent {
		if len(filePath) > 0 && filePath[0] != "" {
			notice.WriteString(fmt.Sprintf(" — verify with read_file(\"%s\", mode:\"tail\") if needed", filePath[0]))
		} else {
			notice.WriteString(" — verify with read_file(mode:\"tail\") if needed")
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
