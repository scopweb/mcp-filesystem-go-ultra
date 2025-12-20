package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ChangeAnalysis represents the analysis of a proposed change
type ChangeAnalysis struct {
	FilePath          string                 `json:"file_path"`
	OperationType     string                 `json:"operation_type"`      // write, edit, delete, move, copy
	LinesAdded        int                    `json:"lines_added"`
	LinesRemoved      int                    `json:"lines_removed"`
	LinesModified     int                    `json:"lines_modified"`
	CharactersChanged int                    `json:"characters_changed"`
	RiskLevel         string                 `json:"risk_level"`          // low, medium, high, critical
	RiskFactors       []string               `json:"risk_factors"`
	Suggestions       []string               `json:"suggestions"`
	Preview           string                 `json:"preview"`             // Diff preview
	Impact            string                 `json:"impact"`              // Human-readable impact description
	FileExists        bool                   `json:"file_exists"`
	FileSize          int64                  `json:"file_size"`
	WouldCreateBackup bool                   `json:"would_create_backup"`
	EstimatedTime     string                 `json:"estimated_time"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// BatchChangeAnalysis represents analysis of multiple changes
type BatchChangeAnalysis struct {
	TotalChanges      int                `json:"total_changes"`
	TotalRisk         string             `json:"total_risk"`
	Changes           []*ChangeAnalysis  `json:"changes"`
	Summary           string             `json:"summary"`
	Recommendations   []string           `json:"recommendations"`
	TotalLinesAdded   int                `json:"total_lines_added"`
	TotalLinesRemoved int                `json:"total_lines_removed"`
	TotalFilesAffected int               `json:"total_files_affected"`
	EstimatedDuration string             `json:"estimated_duration"`
	Timestamp         time.Time          `json:"timestamp"`
}

// AnalyzeWriteChange analyzes a proposed write operation without executing it
func (e *UltraFastEngine) AnalyzeWriteChange(ctx context.Context, path, content string) (*ChangeAnalysis, error) {
	analysis := &ChangeAnalysis{
		FilePath:      path,
		OperationType: "write",
		RiskFactors:   []string{},
		Suggestions:   []string{},
		Metadata:      make(map[string]interface{}),
	}

	// Check if file exists
	existingContent := ""
	info, err := os.Stat(path)
	if err == nil {
		analysis.FileExists = true
		analysis.FileSize = info.Size()

		// Read existing content
		contentBytes, err := os.ReadFile(path)
		if err == nil {
			existingContent = string(contentBytes)
		}
	} else {
		analysis.FileExists = false
		analysis.Suggestions = append(analysis.Suggestions, "This will create a new file")
	}

	// Analyze changes
	if analysis.FileExists {
		analysis.LinesRemoved = strings.Count(existingContent, "\n") + 1
		analysis.LinesAdded = strings.Count(content, "\n") + 1
		analysis.LinesModified = analysis.LinesAdded
		analysis.CharactersChanged = len(content)

		// Generate diff preview
		analysis.Preview = e.generateSimpleDiff(existingContent, content)
		analysis.Impact = fmt.Sprintf("Will replace %d lines with %d new lines (%+d lines)",
			analysis.LinesRemoved, analysis.LinesAdded, analysis.LinesAdded-analysis.LinesRemoved)
		analysis.WouldCreateBackup = true
	} else {
		analysis.LinesAdded = strings.Count(content, "\n") + 1
		analysis.CharactersChanged = len(content)
		analysis.Preview = fmt.Sprintf("New file with %d lines:\n%s",
			analysis.LinesAdded, e.truncateForPreview(content, 500))
		analysis.Impact = fmt.Sprintf("Will create new file with %d lines", analysis.LinesAdded)
		analysis.WouldCreateBackup = false
	}

	// Assess risk
	analysis.RiskLevel = e.assessWriteRisk(analysis, path, content, existingContent)

	// Add risk factors
	if analysis.FileExists {
		analysis.RiskFactors = append(analysis.RiskFactors, "Overwrites existing file")
	}
	if analysis.LinesAdded > 1000 {
		analysis.RiskFactors = append(analysis.RiskFactors, "Large file (>1000 lines)")
	}
	if strings.Contains(path, ".env") || strings.Contains(path, "config") {
		analysis.RiskFactors = append(analysis.RiskFactors, "Configuration file")
	}

	// Estimate time
	analysis.EstimatedTime = e.estimateOperationTime(len(content))

	return analysis, nil
}

// AnalyzeEditChange analyzes a proposed edit operation without executing it
func (e *UltraFastEngine) AnalyzeEditChange(ctx context.Context, path, oldText, newText string) (*ChangeAnalysis, error) {
	analysis := &ChangeAnalysis{
		FilePath:      path,
		OperationType: "edit",
		RiskFactors:   []string{},
		Suggestions:   []string{},
		Metadata:      make(map[string]interface{}),
	}

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file does not exist: %s", path)
	}
	analysis.FileExists = true
	analysis.FileSize = info.Size()

	// Read current content
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(contentBytes)

	// Count occurrences of old text
	occurrences := strings.Count(content, oldText)
	if occurrences == 0 {
		// Try fuzzy matching
		analysis.RiskFactors = append(analysis.RiskFactors, "Exact match not found - may require fuzzy matching")
		analysis.Suggestions = append(analysis.Suggestions, "Consider using recovery_edit for better matching")
	}

	// Calculate changes
	oldLines := strings.Count(oldText, "\n") + 1
	newLines := strings.Count(newText, "\n") + 1

	analysis.LinesRemoved = occurrences * oldLines
	analysis.LinesAdded = occurrences * newLines
	analysis.LinesModified = occurrences * max(oldLines, newLines) // Go 1.21+ built-in
	analysis.CharactersChanged = occurrences * (len(newText) - len(oldText))

	// Generate preview
	if occurrences > 0 {
		analysis.Preview = fmt.Sprintf("Will replace %d occurrence(s):\n\nOLD:\n%s\n\nNEW:\n%s",
			occurrences,
			e.truncateForPreview(oldText, 200),
			e.truncateForPreview(newText, 200))
		analysis.Impact = fmt.Sprintf("Will modify %d occurrence(s) affecting %d lines",
			occurrences, analysis.LinesModified)
	} else {
		analysis.Preview = "No exact matches found. Edit may fail or require fuzzy matching."
		analysis.Impact = "No changes will be made with exact matching"
	}

	// Assess risk
	analysis.RiskLevel = e.assessEditRisk(analysis, occurrences, oldText, newText)

	// Add metadata
	analysis.Metadata["occurrences"] = occurrences
	analysis.Metadata["exact_match"] = occurrences > 0

	analysis.WouldCreateBackup = true
	analysis.EstimatedTime = e.estimateOperationTime(int(info.Size()))

	// Add suggestions
	if occurrences > 10 {
		analysis.Suggestions = append(analysis.Suggestions, fmt.Sprintf("Large number of replacements (%d) - consider reviewing carefully", occurrences))
	}
	if occurrences == 0 {
		analysis.Suggestions = append(analysis.Suggestions, "Use intelligent_edit or recovery_edit for better matching")
	}

	return analysis, nil
}

// AnalyzeDeleteChange analyzes a proposed delete operation without executing it
func (e *UltraFastEngine) AnalyzeDeleteChange(ctx context.Context, path string) (*ChangeAnalysis, error) {
	analysis := &ChangeAnalysis{
		FilePath:      path,
		OperationType: "delete",
		RiskFactors:   []string{},
		Suggestions:   []string{},
		Metadata:      make(map[string]interface{}),
	}

	// Check if file/directory exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file or directory does not exist: %s", path)
	}

	analysis.FileExists = true
	analysis.FileSize = info.Size()

	// Analyze based on type
	if info.IsDir() {
		// Count directory contents
		fileCount, dirCount, totalSize := e.countDirectoryContents(path)
		analysis.Impact = fmt.Sprintf("Will permanently delete directory with %d files and %d subdirectories (total: %s)",
			fileCount, dirCount, formatSize(totalSize))
		analysis.Metadata["file_count"] = fileCount
		analysis.Metadata["dir_count"] = dirCount
		analysis.Metadata["total_size"] = totalSize
		analysis.RiskLevel = "high"
		analysis.RiskFactors = append(analysis.RiskFactors, "Recursive directory deletion")
		if fileCount > 50 {
			analysis.RiskFactors = append(analysis.RiskFactors, fmt.Sprintf("Large number of files (%d)", fileCount))
		}
	} else {
		// Single file
		lines := 0
		contentBytes, err := os.ReadFile(path)
		if err == nil {
			lines = strings.Count(string(contentBytes), "\n") + 1
		}

		analysis.LinesRemoved = lines
		analysis.Impact = fmt.Sprintf("Will permanently delete file (%s, %d lines)",
			formatSize(info.Size()), lines)
		analysis.RiskLevel = e.assessDeleteRisk(path, info.Size())
	}

	// Add risk factors
	analysis.RiskFactors = append(analysis.RiskFactors, "Permanent deletion (cannot be undone)")

	// Check for critical files
	if e.isCriticalFile(path) {
		analysis.RiskLevel = "critical"
		analysis.RiskFactors = append(analysis.RiskFactors, "Critical or configuration file")
	}

	// Suggestions
	analysis.Suggestions = append(analysis.Suggestions, "Consider using soft_delete_file for safer deletion")
	analysis.WouldCreateBackup = false
	analysis.EstimatedTime = "< 1 second"

	return analysis, nil
}

// AnalyzeBatchChanges analyzes multiple changes at once
func (e *UltraFastEngine) AnalyzeBatchChanges(ctx context.Context, changes []map[string]interface{}) (*BatchChangeAnalysis, error) {
	batch := &BatchChangeAnalysis{
		Changes:       make([]*ChangeAnalysis, 0),
		Recommendations: []string{},
		Timestamp:     time.Now(),
	}

	// Analyze each change
	for _, change := range changes {
		opType := change["operation"].(string)
		var analysis *ChangeAnalysis
		var err error

		switch opType {
		case "write":
			analysis, err = e.AnalyzeWriteChange(ctx,
				change["path"].(string),
				change["content"].(string))
		case "edit":
			analysis, err = e.AnalyzeEditChange(ctx,
				change["path"].(string),
				change["old_text"].(string),
				change["new_text"].(string))
		case "delete":
			analysis, err = e.AnalyzeDeleteChange(ctx,
				change["path"].(string))
		default:
			continue
		}

		if err != nil {
			// Create error analysis
			analysis = &ChangeAnalysis{
				FilePath:      change["path"].(string),
				OperationType: opType,
				RiskLevel:     "critical",
				RiskFactors:   []string{fmt.Sprintf("Analysis failed: %v", err)},
			}
		}

		batch.Changes = append(batch.Changes, analysis)
	}

	// Calculate totals
	batch.TotalChanges = len(batch.Changes)
	filesAffected := make(map[string]bool)

	highestRisk := "low"
	for _, change := range batch.Changes {
		filesAffected[change.FilePath] = true
		batch.TotalLinesAdded += change.LinesAdded
		batch.TotalLinesRemoved += change.LinesRemoved

		// Determine highest risk
		if change.RiskLevel == "critical" {
			highestRisk = "critical"
		} else if change.RiskLevel == "high" && highestRisk != "critical" {
			highestRisk = "high"
		} else if change.RiskLevel == "medium" && highestRisk == "low" {
			highestRisk = "medium"
		}
	}

	batch.TotalFilesAffected = len(filesAffected)
	batch.TotalRisk = highestRisk

	// Generate summary
	batch.Summary = fmt.Sprintf("%d operations across %d files: %+d lines added, %d lines removed",
		batch.TotalChanges, batch.TotalFilesAffected, batch.TotalLinesAdded, batch.TotalLinesRemoved)

	// Add recommendations
	if batch.TotalRisk == "critical" || batch.TotalRisk == "high" {
		batch.Recommendations = append(batch.Recommendations, "Review all changes carefully before proceeding")
	}
	if batch.TotalFilesAffected > 10 {
		batch.Recommendations = append(batch.Recommendations, "Consider breaking into smaller batches")
	}
	if batch.TotalLinesAdded+batch.TotalLinesRemoved > 1000 {
		batch.Recommendations = append(batch.Recommendations, "Large number of changes - ensure tests are run")
	}

	// Estimate duration
	batch.EstimatedDuration = e.estimateBatchDuration(batch.TotalChanges)

	return batch, nil
}

// Helper functions

func (e *UltraFastEngine) assessWriteRisk(analysis *ChangeAnalysis, path, newContent, oldContent string) string {
	score := 0

	// File exists and will be overwritten
	if analysis.FileExists {
		score += 1
	}

	// Large file
	if analysis.LinesAdded > 1000 {
		score += 2
	}

	// Critical file
	if e.isCriticalFile(path) {
		score += 3
	}

	// Significant change
	if analysis.FileExists && analysis.CharactersChanged > len(oldContent)/2 {
		score += 1
	}

	if score >= 4 {
		return "critical"
	} else if score >= 2 {
		return "high"
	} else if score >= 1 {
		return "medium"
	}
	return "low"
}

func (e *UltraFastEngine) assessEditRisk(analysis *ChangeAnalysis, occurrences int, oldText, newText string) string {
	score := 0

	// No matches found
	if occurrences == 0 {
		score += 2
	}

	// Many occurrences
	if occurrences > 10 {
		score += 1
	}

	// Large text being replaced
	if len(oldText) > 500 {
		score += 1
	}

	// Significant change in size
	sizeChange := abs(len(newText) - len(oldText))
	if sizeChange > len(oldText)/2 {
		score += 1
	}

	if score >= 3 {
		return "high"
	} else if score >= 2 {
		return "medium"
	}
	return "low"
}

func (e *UltraFastEngine) assessDeleteRisk(path string, size int64) string {
	if e.isCriticalFile(path) {
		return "critical"
	}
	if size > 1024*1024 { // > 1MB
		return "high"
	}
	return "medium"
}

func (e *UltraFastEngine) isCriticalFile(path string) bool {
	criticalPatterns := []string{
		".env", "config", "credentials", "password", "secret",
		"key", ".pem", ".crt", ".key",
	}

	lowerPath := strings.ToLower(path)
	for _, pattern := range criticalPatterns {
		if strings.Contains(lowerPath, pattern) {
			return true
		}
	}
	return false
}

func (e *UltraFastEngine) generateSimpleDiff(oldContent, newContent string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var diff strings.Builder
	diff.WriteString("Diff Preview (first 20 changes):\n")
	diff.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	changesShown := 0
	maxChanges := 20
	maxLen := max(len(oldLines), len(newLines)) // Go 1.21+ built-in

	for i := 0; i < maxLen && changesShown < maxChanges; i++ {
		oldLine := ""
		newLine := ""

		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if oldLine != "" {
				diff.WriteString(fmt.Sprintf("- %s\n", e.truncateForPreview(oldLine, 80)))
			}
			if newLine != "" {
				diff.WriteString(fmt.Sprintf("+ %s\n", e.truncateForPreview(newLine, 80)))
			}
			changesShown++
		}
	}

	if maxLen > maxChanges {
		diff.WriteString(fmt.Sprintf("... (%d more lines)\n", maxLen-maxChanges))
	}

	return diff.String()
}

func (e *UltraFastEngine) truncateForPreview(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func (e *UltraFastEngine) countDirectoryContents(dirPath string) (fileCount, dirCount int, totalSize int64) {
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			dirCount++
		} else {
			fileCount++
			totalSize += info.Size()
		}
		return nil
	})
	return
}

func (e *UltraFastEngine) estimateOperationTime(contentSize int) string {
	if contentSize < 10*1024 {
		return "< 100ms"
	} else if contentSize < 100*1024 {
		return "< 500ms"
	} else if contentSize < 1024*1024 {
		return "< 1 second"
	} else {
		return "1-5 seconds"
	}
}

func (e *UltraFastEngine) estimateBatchDuration(numChanges int) string {
	if numChanges <= 5 {
		return "< 1 second"
	} else if numChanges <= 20 {
		return "1-5 seconds"
	} else {
		return "5-30 seconds"
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
