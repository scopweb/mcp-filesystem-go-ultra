package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/mcp/filesystem-ultra/core"
)

// formatChangeAnalysis formats a ChangeAnalysis struct as human-readable text
func formatChangeAnalysis(analysis *core.ChangeAnalysis) string {
	var result strings.Builder

	// Header
	result.WriteString("Change Analysis (Plan Mode - Dry Run)\n")
	result.WriteString("---\n\n")

	// Basic info
	result.WriteString(fmt.Sprintf("File: %s\n", analysis.FilePath))
	result.WriteString(fmt.Sprintf("Operation: %s\n", analysis.OperationType))
	result.WriteString(fmt.Sprintf("File exists: %v\n", analysis.FileExists))

	// Risk assessment
	result.WriteString(fmt.Sprintf("\nRisk Level: %s\n", strings.ToUpper(analysis.RiskLevel)))

	// Risk factors
	if len(analysis.RiskFactors) > 0 {
		result.WriteString("\nRisk Factors:\n")
		for _, factor := range analysis.RiskFactors {
			result.WriteString(fmt.Sprintf("  - %s\n", factor))
		}
	}

	// Changes summary
	result.WriteString("\nChanges Summary:\n")
	if analysis.LinesAdded > 0 {
		result.WriteString(fmt.Sprintf("  + %d lines added\n", analysis.LinesAdded))
	}
	if analysis.LinesRemoved > 0 {
		result.WriteString(fmt.Sprintf("  - %d lines removed\n", analysis.LinesRemoved))
	}
	if analysis.LinesModified > 0 {
		result.WriteString(fmt.Sprintf("  ~ %d lines modified\n", analysis.LinesModified))
	}

	// Impact
	result.WriteString(fmt.Sprintf("\nImpact: %s\n", analysis.Impact))

	// Preview
	if analysis.Preview != "" {
		result.WriteString(fmt.Sprintf("\nPreview:\n%s\n", analysis.Preview))
	}

	// Suggestions
	if len(analysis.Suggestions) > 0 {
		result.WriteString("\nSuggestions:\n")
		for _, suggestion := range analysis.Suggestions {
			result.WriteString(fmt.Sprintf("  - %s\n", suggestion))
		}
	}

	// Additional info
	result.WriteString("\nAdditional Info:\n")
	result.WriteString(fmt.Sprintf("  - Backup would be created: %v\n", analysis.WouldCreateBackup))
	result.WriteString(fmt.Sprintf("  - Estimated time: %s\n", analysis.EstimatedTime))

	result.WriteString("\n---\n")
	result.WriteString("This is a DRY RUN - no changes were made\n")

	return result.String()
}

// toIfaceSlice converts []string to []interface{} (for building arguments)
func toIfaceSlice(in []string) []interface{} {
	out := make([]interface{}, 0, len(in))
	for _, v := range in {
		out = append(out, v)
	}
	return out
}

// parseSize parses size strings like "50MB", "1GB", etc.
func parseSize(sizeStr string) (int64, error) {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	var multiplier int64 = 1
	if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	}

	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}

	return size * multiplier, nil
}

// formatBatchResult formats a BatchResult as human-readable text
func formatBatchResult(result core.BatchResult) string {
	var sb strings.Builder

	if result.ValidationOnly {
		sb.WriteString("Batch Validation Results\n")
		sb.WriteString("---\n\n")
		if result.Success {
			sb.WriteString(fmt.Sprintf("All %d operations validated successfully\n", result.TotalOps))
			sb.WriteString("Ready to execute\n")
		} else {
			sb.WriteString("Validation failed\n")
			sb.WriteString(fmt.Sprintf("Errors: %v\n", result.Errors))
		}
		return sb.String()
	}

	// Execution results
	if result.Success {
		sb.WriteString("Batch Operations Completed Successfully\n")
	} else {
		sb.WriteString("Batch Operations Failed\n")
	}
	sb.WriteString("---\n\n")

	sb.WriteString("Summary:\n")
	sb.WriteString(fmt.Sprintf("  Total operations: %d\n", result.TotalOps))
	sb.WriteString(fmt.Sprintf("  Completed: %d\n", result.CompletedOps))
	sb.WriteString(fmt.Sprintf("  Failed: %d\n", result.FailedOps))
	sb.WriteString(fmt.Sprintf("  Execution time: %s\n", result.ExecutionTime))

	if result.BackupPath != "" {
		sb.WriteString(fmt.Sprintf("  Backup created: %s\n", result.BackupPath))
	}

	if result.RollbackDone {
		sb.WriteString("\nRollback performed - all changes reverted\n")
	}

	// Individual operation results
	sb.WriteString("\nOperation Details:\n")
	for _, opResult := range result.Results {
		status := "OK"
		if !opResult.Success {
			status = "FAIL"
		} else if opResult.Skipped {
			status = "SKIP"
		}

		sb.WriteString(fmt.Sprintf("  [%s] [%d] %s: %s", status, opResult.Index, opResult.Type, opResult.Path))

		if opResult.BytesAffected > 0 {
			sb.WriteString(fmt.Sprintf(" (%s)", formatSize(opResult.BytesAffected)))
		}

		if opResult.Error != "" {
			sb.WriteString(fmt.Sprintf(" - Error: %s", opResult.Error))
		}

		sb.WriteString("\n")
	}

	if len(result.Errors) > 0 {
		sb.WriteString("\nErrors:\n")
		for _, err := range result.Errors {
			sb.WriteString(fmt.Sprintf("  - %s\n", err))
		}
	}

	return sb.String()
}

// formatPipelineResult formats pipeline execution results for display
func formatPipelineResult(result *core.PipelineResult, compact bool) string {
	if result == nil {
		return "ERROR: No result returned"
	}

	if compact && !result.Verbose {
		// Compact mode: one-line summary
		status := "OK"
		if !result.Success {
			status = "FAIL"
		}
		riskInfo := ""
		if result.OverallRiskLevel != "" && result.OverallRiskLevel != "LOW" {
			riskInfo = fmt.Sprintf(" | %s risk", strings.ToLower(result.OverallRiskLevel))
		}
		// Include error details from failed steps (Bug #21: silent failures)
		errorInfo := ""
		if !result.Success {
			for _, sr := range result.Results {
				if sr.Error != "" {
					errorInfo = fmt.Sprintf(" | %s:%s: %s", sr.StepID, sr.Action, sr.Error)
					break // Show first error only in compact mode
				}
			}
			if errorInfo == "" && result.RollbackPerformed {
				errorInfo = " | rolled back"
			}
		}
		return fmt.Sprintf("%s: %d/%d steps | %d files | %d edits%s%s",
			status, result.CompletedSteps, result.TotalSteps,
			len(result.FilesAffected), result.TotalEdits, riskInfo, errorInfo)
	}

	// Verbose mode: detailed output
	var output strings.Builder

	if result.DryRun {
		output.WriteString("DRY RUN - No changes made\n")
	}

	output.WriteString("---\n")
	output.WriteString(fmt.Sprintf("Pipeline: %s\n", result.Name))

	if result.Success {
		output.WriteString("Success: true\n")
	} else {
		output.WriteString("Success: false\n")
	}

	output.WriteString(fmt.Sprintf("Steps: %d/%d completed\n", result.CompletedSteps, result.TotalSteps))
	output.WriteString(fmt.Sprintf("Duration: %v\n", result.TotalDuration))

	if result.BackupID != "" {
		output.WriteString(fmt.Sprintf("Backup: %s\n", result.BackupID))
	}

	if result.RollbackPerformed {
		output.WriteString("Rollback: performed\n")
	}

	output.WriteString("\nStep Results:\n\n")

	for i, stepResult := range result.Results {
		stepNum := i + 1
		status := "OK"
		if !stepResult.Success {
			status = "FAIL"
		}

		output.WriteString(fmt.Sprintf("%d. %s [%s] %s\n",
			stepNum, stepResult.StepID, stepResult.Action, status))
		output.WriteString(fmt.Sprintf("   Duration: %v\n", stepResult.Duration))

		if len(stepResult.FilesMatched) > 0 {
			output.WriteString(fmt.Sprintf("   Files: %d matched\n", len(stepResult.FilesMatched)))
			if len(stepResult.FilesMatched) <= 5 {
				for _, f := range stepResult.FilesMatched {
					output.WriteString(fmt.Sprintf("     - %s\n", f))
				}
			} else {
				for j := 0; j < 3; j++ {
					output.WriteString(fmt.Sprintf("     - %s\n", stepResult.FilesMatched[j]))
				}
				output.WriteString(fmt.Sprintf("     ... and %d more\n", len(stepResult.FilesMatched)-3))
			}
		}

		if stepResult.EditsApplied > 0 {
			output.WriteString(fmt.Sprintf("   Edits: %d replacements\n", stepResult.EditsApplied))
		}

		if len(stepResult.Counts) > 0 {
			totalCount := 0
			for _, count := range stepResult.Counts {
				totalCount += count
			}
			output.WriteString(fmt.Sprintf("   Counts: %d total occurrences\n", totalCount))

			// Verbose: per-file counts
			if result.Verbose {
				for file, count := range stepResult.Counts {
					output.WriteString(fmt.Sprintf("     %s: %d\n", file, count))
				}
			}
		}

		// Verbose: include file contents from read_ranges
		if result.Verbose && len(stepResult.Content) > 0 {
			for file, content := range stepResult.Content {
				output.WriteString(fmt.Sprintf("   --- %s ---\n", file))
				// Truncate content to avoid massive output
				lines := strings.Split(content, "\n")
				if len(lines) > 50 {
					for _, line := range lines[:25] {
						output.WriteString(fmt.Sprintf("   %s\n", line))
					}
					output.WriteString(fmt.Sprintf("   ... (%d lines omitted) ...\n", len(lines)-50))
					for _, line := range lines[len(lines)-25:] {
						output.WriteString(fmt.Sprintf("   %s\n", line))
					}
				} else {
					for _, line := range lines {
						output.WriteString(fmt.Sprintf("   %s\n", line))
					}
				}
			}
		}

		// Verbose: full file list when more than 5
		if result.Verbose && len(stepResult.FilesMatched) > 5 {
			output.WriteString("   All files:\n")
			for _, f := range stepResult.FilesMatched {
				output.WriteString(fmt.Sprintf("     - %s\n", f))
			}
		}

		if stepResult.RiskLevel != "" && stepResult.RiskLevel != "LOW" {
			output.WriteString(fmt.Sprintf("   Risk: %s\n", stepResult.RiskLevel))
		}

		if stepResult.Error != "" {
			output.WriteString(fmt.Sprintf("   Error: %s\n", stepResult.Error))
		}

		output.WriteString("\n")
	}

	output.WriteString("---\n")
	output.WriteString(fmt.Sprintf("Files affected: %d\n", len(result.FilesAffected)))
	output.WriteString(fmt.Sprintf("Total edits: %d\n", result.TotalEdits))

	if result.OverallRiskLevel != "" {
		output.WriteString(fmt.Sprintf("Overall risk: %s\n", result.OverallRiskLevel))
	}

	return output.String()
}

// truncateContent truncates content based on mode and max lines
func truncateContent(content string, maxLines int, mode string) string {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if maxLines <= 0 {
		maxLines = 100 // Default
	}

	var result []string
	var truncMsg string

	switch mode {
	case "head":
		if totalLines <= maxLines {
			return content
		}
		result = lines[:maxLines]
		truncMsg = fmt.Sprintf("\n[Truncated: showing first %d of %d lines. Use mode=all or increase max_lines to see more]", maxLines, totalLines)

	case "tail":
		if totalLines <= maxLines {
			return content
		}
		result = lines[totalLines-maxLines:]
		truncMsg = fmt.Sprintf("\n[Truncated: showing last %d of %d lines. Use mode=all or increase max_lines to see more]", maxLines, totalLines)

	default: // "all" or unspecified
		if maxLines > 0 && totalLines > maxLines {
			// Take half from head, half from tail
			half := maxLines / 2
			result = append(lines[:half], fmt.Sprintf("\n... [%d lines omitted] ...\n", totalLines-maxLines))
			result = append(result, lines[totalLines-half:]...)
			truncMsg = fmt.Sprintf("\n[Truncated: showing %d of %d lines (%d head + %d tail). Use mode=head/tail or increase max_lines]", maxLines, totalLines, half, half)
		} else {
			return content
		}
	}

	return strings.Join(result, "\n") + truncMsg
}

// formatSize formats bytes to human readable format
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// setupLogging configures logging based on configuration
func setupLogging(config *Configuration) {
	if config.DebugMode {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	} else {
		log.SetFlags(log.LstdFlags)
	}
}

// runBenchmark runs performance benchmarks
func runBenchmark(config *Configuration) {
	log.Printf("Running performance benchmark...")

	// This would run comprehensive benchmarks comparing:
	// 1. This ultra-fast server vs standard MCP
	// 2. Various cache sizes and parallel operation counts
	// 3. Different file sizes and operation types

	fmt.Printf("Benchmark results will be implemented in bench/ package\n")
	fmt.Printf("Run: cd bench && go run benchmark.go\n")
}
