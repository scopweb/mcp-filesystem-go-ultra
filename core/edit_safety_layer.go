package core

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"strings"
)

// EditSafetyValidator implements the blindaje protocol to prevent file corruption
// This layer ensures that edits are safe before execution
type EditSafetyValidator struct {
	verbose bool // Enable detailed logging
}

// NewEditSafetyValidator creates a new safety validator
func NewEditSafetyValidator(verbose bool) *EditSafetyValidator {
	return &EditSafetyValidator{
		verbose: verbose,
	}
}

// ValidationResult contains the validation status and diagnostic info
type ValidationResult struct {
	IsValid              bool
	CanProceed           bool
	MatchFound           bool
	MatchCount           int
	Confidence           float64 // 0.0 to 1.0
	FileHash             string
	OldTextHash          string
	SuggestedAlternative string
	Diagnostics          ValidationDiagnostics
}

// ValidationDiagnostics contains detailed debugging information
type ValidationDiagnostics struct {
	FileSize              int64
	FileEncoding          string
	LineEndingType        string
	OldTextLength         int
	OldTextLineCount      int
	NewTextLength         int
	NewTextLineCount      int
	ExactMatches          int
	NormalizedMatches     int
	FuzzyMatches          int
	TextNormalizationNote string
	ContextFound          bool
	ContextMatches        int
	ErrorDetails          string
}

// ValidateEditSafety performs comprehensive validation before edit (REGLA 1, 2, 3)
// Returns true if edit is safe to proceed
func (esv *EditSafetyValidator) ValidateEditSafety(filePath, oldText, newText string) *ValidationResult {
	result := &ValidationResult{
		IsValid:    false,
		CanProceed: false,
		Diagnostics: ValidationDiagnostics{
			FuzzyMatches:     0,
			NormalizedMatches: 0,
		},
	}

	// REGLA 1: Read file and capture state
	content, err := os.ReadFile(filePath)
	if err != nil {
		result.Diagnostics.ErrorDetails = fmt.Sprintf("Could not read file: %v", err)
		return result
	}

	fileContent := string(content)
	result.Diagnostics.FileSize = int64(len(fileContent))
	result.OldTextHash = hashString(oldText)
	result.FileHash = hashString(fileContent)

	// Analyze file characteristics
	result.Diagnostics = esv.analyzeFileCharacteristics(fileContent, oldText, newText)

	// REGLA 2: Try exact match first
	exactMatches := strings.Count(fileContent, oldText)
	if exactMatches > 0 {
		result.MatchFound = true
		result.MatchCount = exactMatches
		result.Confidence = 1.0
		result.IsValid = true
		result.CanProceed = true
		result.Diagnostics.ExactMatches = exactMatches
		return result
	}

	// Try normalized match
	normalizedOld := normalizeLineEndings(strings.TrimSpace(oldText))
	normalizedMatches := strings.Count(normalizeLineEndings(fileContent), normalizedOld)
	if normalizedMatches > 0 {
		result.MatchFound = true
		result.MatchCount = normalizedMatches
		result.Confidence = 0.9
		result.IsValid = true
		result.CanProceed = true
		result.Diagnostics.NormalizedMatches = normalizedMatches
		result.Diagnostics.TextNormalizationNote = "Match found after normalizing line endings and whitespace"
		return result
	}

	// Try fuzzy match - look for context
	contextMatches := esv.findContextMatches(fileContent, oldText)
	if contextMatches > 0 {
		result.MatchFound = false
		result.Confidence = 0.5
		result.IsValid = false
		result.CanProceed = false
		result.Diagnostics.ContextMatches = contextMatches
		result.Diagnostics.ErrorDetails = "Context found but exact text not found. File may have been modified."
		result.SuggestedAlternative = esv.suggestAlternative(fileContent, oldText)
		return result
	}

	// No match found
	result.MatchFound = false
	result.Confidence = 0.0
	result.IsValid = false
	result.CanProceed = false
	result.Diagnostics.ErrorDetails = "old_text not found in file"
	result.SuggestedAlternative = esv.suggestAlternative(fileContent, oldText)

	return result
}

// analyzeFileCharacteristics detects file encoding and line ending types
func (esv *EditSafetyValidator) analyzeFileCharacteristics(content, oldText, newText string) ValidationDiagnostics {
	diags := ValidationDiagnostics{}

	diags.OldTextLength = len(oldText)
	diags.OldTextLineCount = strings.Count(oldText, "\n") + 1
	diags.NewTextLength = len(newText)
	diags.NewTextLineCount = strings.Count(newText, "\n") + 1

	// Detect line endings
	if strings.Contains(content, "\r\n") {
		diags.LineEndingType = "CRLF (Windows)"
	} else if strings.Contains(content, "\n") {
		diags.LineEndingType = "LF (Unix)"
	} else {
		diags.LineEndingType = "None (single line)"
	}

	// Detect encoding (simplified - Go defaults to UTF-8)
	if isValidUTF8([]byte(content)) {
		diags.FileEncoding = "UTF-8"
	} else {
		diags.FileEncoding = "Unknown/Binary"
	}

	return diags
}

// findContextMatches searches for partial matches in context
// This helps identify if the file was modified since the read
func (esv *EditSafetyValidator) findContextMatches(content, oldText string) int {
	lines := strings.Split(content, "\n")
	oldLines := strings.Split(oldText, "\n")

	if len(oldLines) < 2 {
		return 0 // Context matching only for multiline
	}

	firstLine := strings.TrimSpace(oldLines[0])

	matches := 0
	for i, line := range lines {
		if strings.Contains(strings.TrimSpace(line), firstLine) {
			// Found first line, check if subsequent lines match
			if i+len(oldLines)-1 < len(lines) {
				contextMatch := true
				for j := 1; j < len(oldLines); j++ {
					if !strings.Contains(lines[i+j], strings.TrimSpace(oldLines[j])) {
						contextMatch = false
						break
					}
				}
				if contextMatch {
					matches++
				}
			}
		}
	}

	return matches
}

// suggestAlternative tries to find what the text might have changed to
func (esv *EditSafetyValidator) suggestAlternative(content, oldText string) string {
	lines := strings.Split(oldText, "\n")
	if len(lines) == 0 {
		return ""
	}

	// Try to find first line
	firstLine := strings.TrimSpace(lines[0])
	contentLines := strings.Split(content, "\n")

	for i, line := range contentLines {
		if strings.Contains(line, firstLine) {
			// Found context, return surrounding lines
			start := i - 1
			if start < 0 {
				start = 0
			}
			end := i + 3
			if end > len(contentLines) {
				end = len(contentLines)
			}

			var suggestion strings.Builder
			suggestion.WriteString("Found similar context:\n")
			for j := start; j < end; j++ {
				suggestion.WriteString(fmt.Sprintf("  Line %d: %s\n", j, contentLines[j]))
			}
			return suggestion.String()
		}
	}

	return ""
}

// VerifyEditResult checks if an edit was applied correctly (REGLA 5)
func (esv *EditSafetyValidator) VerifyEditResult(filePath, oldText, newText string) (bool, string) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, fmt.Sprintf("Could not verify: %v", err)
	}

	fileContent := string(content)

	// Check if old text is gone
	if strings.Contains(fileContent, oldText) {
		return false, "old_text still present in file after edit"
	}

	// Check if new text is present
	if !strings.Contains(fileContent, newText) {
		return false, "new_text not found in file after edit"
	}

	return true, "✅ Edit verified successfully"
}

// RecommendedEditStrategy suggests the safest way to perform the edit
func (esv *EditSafetyValidator) RecommendedEditStrategy(validation *ValidationResult) string {
	if !validation.MatchFound {
		return fmt.Sprintf("❌ CANNOT PROCEED: %s\n\nSuggested: %s",
			validation.Diagnostics.ErrorDetails,
			validation.SuggestedAlternative)
	}

	if validation.Diagnostics.OldTextLineCount > 3 {
		return fmt.Sprintf("⚠️  Large multiline edit (%d lines) detected.\n"+
			"Recommended: Use batch_operations with atomic=true\n"+
			"Or split into smaller single-line edits for safety",
			validation.Diagnostics.OldTextLineCount)
	}

	if validation.Diagnostics.NormalizedMatches > 0 && validation.Diagnostics.ExactMatches == 0 {
		return fmt.Sprintf("⚠️  Match found only after normalization.\n"+
			"Confidence: %.0f%%\n"+
			"Recommended: Verify the matched text carefully before proceeding",
			validation.Confidence*100)
	}

	return "✅ Edit is safe to proceed"
}

// hashString computes MD5 hash of a string (for comparison, not security)
func hashString(s string) string {
	h := md5.New()
	io.WriteString(h, s)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// isValidUTF8 checks if bytes are valid UTF-8
func isValidUTF8(data []byte) bool {
	for i := 0; i < len(data); {
		if data[i] < 0x80 {
			i++
		} else if data[i] < 0xE0 {
			if i+1 >= len(data) {
				return false
			}
			i += 2
		} else if data[i] < 0xF0 {
			if i+2 >= len(data) {
				return false
			}
			i += 3
		} else {
			if i+3 >= len(data) {
				return false
			}
			i += 4
		}
	}
	return true
}

// DetailedEditLog provides comprehensive logging for debugging
type DetailedEditLog struct {
	Timestamp           string
	Operation           string
	FilePath            string
	ValidationResult    *ValidationResult
	PreEditHash         string
	PostEditHash        string
	BackupID            string
	Success             bool
	ErrorMessage        string
	ExecutionTimeMs     int64
	RecommendedFallback string
}

// FormatDetailedLog creates a detailed log for debugging
func (del *DetailedEditLog) Format() string {
	var sb strings.Builder

	sb.WriteString("═══════════════════════════════════════════════════\n")
	sb.WriteString("              DETAILED EDIT LOG\n")
	sb.WriteString("═══════════════════════════════════════════════════\n")
	sb.WriteString(fmt.Sprintf("Timestamp:     %s\n", del.Timestamp))
	sb.WriteString(fmt.Sprintf("Operation:     %s\n", del.Operation))
	sb.WriteString(fmt.Sprintf("File:          %s\n", del.FilePath))
	sb.WriteString(fmt.Sprintf("Status:        %v\n", del.Success))
	sb.WriteString(fmt.Sprintf("Execution:     %dms\n", del.ExecutionTimeMs))

	if del.ValidationResult != nil {
		sb.WriteString("\n📊 VALIDATION:\n")
		sb.WriteString(fmt.Sprintf("  Match Found:    %v\n", del.ValidationResult.MatchFound))
		sb.WriteString(fmt.Sprintf("  Match Count:    %d\n", del.ValidationResult.MatchCount))
		sb.WriteString(fmt.Sprintf("  Confidence:     %.0f%%\n", del.ValidationResult.Confidence*100))
		sb.WriteString(fmt.Sprintf("  File Hash:      %s\n", del.ValidationResult.FileHash[:8]))
		sb.WriteString(fmt.Sprintf("  Old Text Hash:  %s\n", del.ValidationResult.OldTextHash[:8]))

		diags := del.ValidationResult.Diagnostics
		sb.WriteString("\n📈 DIAGNOSTICS:\n")
		sb.WriteString(fmt.Sprintf("  File Size:      %d bytes\n", diags.FileSize))
		sb.WriteString(fmt.Sprintf("  Encoding:       %s\n", diags.FileEncoding))
		sb.WriteString(fmt.Sprintf("  Line Endings:   %s\n", diags.LineEndingType))
		sb.WriteString(fmt.Sprintf("  Old Text:       %d bytes, %d lines\n", diags.OldTextLength, diags.OldTextLineCount))
		sb.WriteString(fmt.Sprintf("  New Text:       %d bytes, %d lines\n", diags.NewTextLength, diags.NewTextLineCount))
		sb.WriteString(fmt.Sprintf("  Exact Matches:  %d\n", diags.ExactMatches))
		sb.WriteString(fmt.Sprintf("  Normalized:     %d\n", diags.NormalizedMatches))
		sb.WriteString(fmt.Sprintf("  Context Matches:%d\n", diags.ContextMatches))
	}

	if del.ErrorMessage != "" {
		sb.WriteString(fmt.Sprintf("\n❌ ERROR: %s\n", del.ErrorMessage))
	}

	if del.RecommendedFallback != "" {
		sb.WriteString(fmt.Sprintf("\n💡 RECOMMENDATION: %s\n", del.RecommendedFallback))
	}

	sb.WriteString("═══════════════════════════════════════════════════\n")

	return sb.String()
}
