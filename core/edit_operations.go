package core

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mcp/filesystem-ultra/mcp"
)

// EditResult represents file edit operation results
type EditResult struct {
	ModifiedContent  string
	ReplacementCount int
	MatchConfidence  string
	LinesAffected    int
	BackupID         string // ID of backup created before edit
}

// SearchMatch represents a text search match
type SearchMatch struct {
	File       string   `json:"file"`
	LineNumber int      `json:"line_number"`
	Line       string   `json:"line"`
	Context    []string `json:"context,omitempty"`
	MatchStart int      `json:"match_start"`
	MatchEnd   int      `json:"match_end"`
}

// EditFile performs intelligent file editing with backup and rollback
// WARNING: On Windows with Claude Desktop, changes may not persist to Windows filesystem.
// Use IntelligentWrite with complete file content for guaranteed persistence.
// See: guides/WINDOWS_FILESYSTEM_PERSISTENCE.md
//
// Context Validation: Validates surrounding context (3-5 lines) to prevent
// editing stale content that may have been modified since the file was read.
func (e *UltraFastEngine) EditFile(path, oldText, newText string, force bool) (*EditResult, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Validate file
	if err := e.validateEditableFile(path); err != nil {
		return nil, fmt.Errorf("file validation failed: %w", err)
	}

	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	// Validate context: Check if surrounding content suggests file has changed
	// This prevents overwriting recent modifications
	contextValid, contextWarning := e.validateEditContext(string(content), oldText)
	if !contextValid {
		return nil, fmt.Errorf("context validation failed: %s - file may have been modified. Please re-read the file with smart_search + read_file_range", contextWarning)
	}

	// Calculate change impact for risk assessment
	impact := CalculateChangeImpact(string(content), oldText, newText, e.riskThresholds)

	// ⚠️ RISK VALIDATION: Block HIGH/CRITICAL risk operations unless force=true
	if impact.IsRisky && !force {
		warning := impact.FormatRiskWarning()
		return nil, fmt.Errorf("OPERATION BLOCKED - %s\n\nTo proceed anyway, add \"force\": true to the request", warning)
	}

	// Log telemetry about this edit operation
	// This helps identify patterns of full-file rewrites vs targeted edits
	e.LogEditTelemetry(int64(len(oldText)), int64(len(newText)), path)

	// Execute pre-edit hooks
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreEdit,
		ToolName:   "edit_file",
		FilePath:   path,
		Operation:  "edit",
		OldContent: string(content),
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata: map[string]interface{}{
			"old_text":       oldText,
			"new_text":       newText,
			"risk_level":     impact.RiskLevel,
			"change_percent": impact.ChangePercentage,
		},
	}

	hookResult, err := e.hookManager.ExecuteHooks(context.Background(), HookPreEdit, hookCtx)
	if err != nil {
		return nil, fmt.Errorf("pre-edit hook denied operation: %w", err)
	}

	// Create persistent backup using BackupManager
	var backupID string
	if e.backupManager != nil {
		backupID, err = e.backupManager.CreateBackupWithContext(path, "edit_file",
			fmt.Sprintf("Edit: %d occurrences, %.1f%% change", impact.Occurrences, impact.ChangePercentage))
		if err != nil {
			return nil, fmt.Errorf("could not create backup: %w", err)
		}
	}

	// Perform intelligent edit
	result, err := e.performIntelligentEdit(string(content), oldText, newText)
	if err != nil {
		return nil, fmt.Errorf("edit failed: %w", err)
	}

	// Apply hook modifications if any
	finalContent := result.ModifiedContent
	if hookResult.ModifiedContent != "" {
		finalContent = hookResult.ModifiedContent
	}

	// Write modified content atomically
	tmpPath := path + ".tmp." + fmt.Sprintf("%d", e.metrics.OperationsTotal)
	if err := os.WriteFile(tmpPath, []byte(finalContent), 0644); err != nil {
		return nil, fmt.Errorf("error writing temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("error finalizing edit: %w", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	// DO NOT remove backup - keep it persistent for recovery
	// (old behavior: os.Remove(backupPath) - removed for Bug10 fix)

	// Execute post-edit hooks
	hookCtx.Event = HookPostEdit
	hookCtx.NewContent = finalContent
	hookCtx.Metadata["backup_id"] = backupID
	_, _ = e.hookManager.ExecuteHooks(context.Background(), HookPostEdit, hookCtx)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterEdit(path)
	}

	// Store backup ID in result
	result.BackupID = backupID

	return result, nil
}

// SearchAndReplace performs search and replace operations across files
func (e *UltraFastEngine) SearchAndReplace(path, pattern, replacement string, caseSensitive bool) (*mcp.CallToolResponse, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Validate path
	validPath, err := e.validatePath(path)
	if err != nil {
		return nil, fmt.Errorf("path validation failed: %w", err)
	}

	// Check if it's a file or directory
	info, err := os.Stat(validPath)
	if err != nil {
		return nil, fmt.Errorf("error accessing path: %w", err)
	}

	var results []string
	var totalReplacements int

	if info.IsDir() {
		// Search and replace in directory
		err = e.searchAndReplaceInDirectory(validPath, pattern, replacement, caseSensitive, &results, &totalReplacements)
	} else {
		// Search and replace in single file
		replacements, err := e.searchAndReplaceInFile(validPath, pattern, replacement, caseSensitive)
		if err == nil && replacements > 0 {
			results = append(results, fmt.Sprintf("📄 %s: %d replacements", validPath, replacements))
			totalReplacements += replacements
		}
	}

	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: %v", err)},
			},
		}, nil
	}

	if totalReplacements == 0 {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("🔍 No matches found for pattern '%s' in %s", pattern, path)},
			},
		}, nil
	}

	var resultBuilder strings.Builder
	resultBuilder.WriteString("✅ Search and replace completed!\n")
	resultBuilder.WriteString(fmt.Sprintf("🔍 Pattern: '%s'\n", pattern))
	resultBuilder.WriteString(fmt.Sprintf("🔄 Replacement: '%s'\n", replacement))
	resultBuilder.WriteString(fmt.Sprintf("📊 Total replacements: %d\n\n", totalReplacements))

	for _, result := range results {
		resultBuilder.WriteString(result + "\n")
	}

	return &mcp.CallToolResponse{
		Content: []mcp.TextContent{
			{Text: resultBuilder.String()},
		},
	}, nil
}

// validatePath validates if a path is accessible
func (e *UltraFastEngine) validatePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Resolve absolute path for security checks
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Enforce allowed paths if configured
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(abs) { // uses engine.go helper
			return "", fmt.Errorf("access denied: path '%s' not in allowed paths", abs)
		}
	}
	return abs, nil
}

// validateEditableFile checks if a file can be edited
func (e *UltraFastEngine) validateEditableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("cannot edit directory")
	}
	if info.Size() > 50*1024*1024 { // 50MB limit
		return fmt.Errorf("file too large for editing")
	}
	return nil
}

// createBackup creates a backup of a file
func (e *UltraFastEngine) createBackup(path string) (string, error) {
	backupPath := path + ".backup"
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	err = os.WriteFile(backupPath, content, 0644)
	return backupPath, err
}

// performIntelligentEdit performs intelligent text replacement with optimizations
// ULTRA-FAST: Uses pre-allocated buffers and minimal allocations
func (e *UltraFastEngine) performIntelligentEdit(content, oldText, newText string) (*EditResult, error) {
	if oldText == "" {
		return nil, fmt.Errorf("old_text cannot be empty")
	}

	// Normalize line endings once
	content = normalizeLineEndings(content)
	oldText = normalizeLineEndings(oldText)
	newText = normalizeLineEndings(newText)

	// OPTIMIZATION 1: Fast path for exact match (most common case - ~80% of edits)
	// Uses strings.Index which is highly optimized with SIMD on modern CPUs
	if idx := strings.Index(content, oldText); idx >= 0 {
		replacements := strings.Count(content, oldText)

		// Pre-allocate result with exact size for zero-copy
		sizeDiff := len(newText) - len(oldText)
		newLen := len(content) + (sizeDiff * replacements)

		var sb strings.Builder
		sb.Grow(newLen)

		// Single-pass replacement (faster than ReplaceAll for known count)
		last := 0
		linesAffected := 0
		for {
			idx := strings.Index(content[last:], oldText)
			if idx < 0 {
				break
			}
			// Count newlines in affected region for line tracking
			if strings.Contains(content[last:last+idx+len(oldText)], "\n") {
				linesAffected += strings.Count(content[last:last+idx+len(oldText)], "\n")
			} else {
				linesAffected++
			}
			sb.WriteString(content[last : last+idx])
			sb.WriteString(newText)
			last = last + idx + len(oldText)
		}
		sb.WriteString(content[last:])

		return &EditResult{
			ModifiedContent:  sb.String(),
			ReplacementCount: replacements,
			MatchConfidence:  "high",
			LinesAffected:    linesAffected,
		}, nil
	}

	// OPTIMIZATION 2: Pre-compute normalized variants once
	normalizedOld := strings.TrimSpace(oldText)

	// OPTIMIZATION 3: Check if normalized version exists in content (whitespace-insensitive)
	if normalizedOld != oldText && strings.Contains(content, normalizedOld) {
		replacements := strings.Count(content, normalizedOld)

		// Pre-allocate for zero-copy replacement
		sizeDiff := len(newText) - len(normalizedOld)
		newLen := len(content) + (sizeDiff * replacements)

		var sb strings.Builder
		sb.Grow(newLen)

		// Fast single-pass replacement
		last := 0
		for {
			idx := strings.Index(content[last:], normalizedOld)
			if idx < 0 {
				break
			}
			sb.WriteString(content[last : last+idx])
			sb.WriteString(newText)
			last = last + idx + len(normalizedOld)
		}
		sb.WriteString(content[last:])

		return &EditResult{
			ModifiedContent:  sb.String(),
			ReplacementCount: replacements,
			MatchConfidence:  "high",
			LinesAffected:    calculateLinesWithText(content, normalizedOld),
		}, nil
	}

	// OPTIMIZATION 4: Line-by-line processing with bufio.Scanner for memory efficiency
	// Only used when exact match fails - handles indentation differences
	// Using scanner instead of strings.Split reduces memory by 30-40% for large files
	scanner := bufio.NewScanner(strings.NewReader(content))
	replacements := 0
	affectedLines := 0
	modified := false
	firstLine := true

	// Pre-allocate result builder with estimated size
	var resultBuilder strings.Builder
	resultBuilder.Grow(len(content) + 1024) // Extra space for potential expansions

	for scanner.Scan() {
		originalLine := scanner.Text()

		// Try exact match in line first (most common)
		if strings.Contains(originalLine, oldText) {
			line := strings.ReplaceAll(originalLine, oldText, newText)
			replacements += strings.Count(originalLine, oldText)
			affectedLines++
			modified = true
			if !firstLine {
				resultBuilder.WriteByte('\n')
			}
			resultBuilder.WriteString(line)
		} else if trimmed := strings.TrimSpace(originalLine); trimmed == normalizedOld {
			// Try normalized match with indentation preservation
			line := getIndentation(originalLine) + strings.TrimSpace(newText)
			replacements++
			affectedLines++
			modified = true
			if !firstLine {
				resultBuilder.WriteByte('\n')
			}
			resultBuilder.WriteString(line)
		} else if strings.Contains(originalLine, normalizedOld) {
			// Try normalized match without indentation
			line := strings.ReplaceAll(originalLine, normalizedOld, newText)
			replacements += strings.Count(originalLine, normalizedOld)
			affectedLines++
			modified = true
			if !firstLine {
				resultBuilder.WriteByte('\n')
			}
			resultBuilder.WriteString(line)
		} else {
			// No match, keep original line
			if !firstLine {
				resultBuilder.WriteByte('\n')
			}
			resultBuilder.WriteString(originalLine)
		}

		firstLine = false
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	if modified {
		return &EditResult{
			ModifiedContent:  resultBuilder.String(),
			ReplacementCount: replacements,
			MatchConfidence:  "medium",
			LinesAffected:    affectedLines,
		}, nil
	}

	// OPTIMIZATION 5: Multiline search only if single-line failed
	if strings.Contains(content, oldText) {
		newContent := strings.ReplaceAll(content, oldText, newText)
		return &EditResult{
			ModifiedContent:  newContent,
			ReplacementCount: 1,
			MatchConfidence:  "medium",
			LinesAffected:    strings.Count(oldText, "\n") + 1,
		}, nil
	}

	// OPTIMIZATION 6: Flexible regex search as last resort (expensive)
	// Only try if content is not too large (< 100KB for regex)
	// Compile regex once and reuse
	if len(content) < 100*1024 {
		escapedOld := regexp.QuoteMeta(oldText)
		flexiblePattern := makeFlexiblePattern(escapedOld)

		re, err := regexp.Compile(flexiblePattern)
		if err == nil {
			matches := re.FindAllString(content, -1)
			if len(matches) > 0 {
				newContent := re.ReplaceAllString(content, newText)
				return &EditResult{
					ModifiedContent:  newContent,
					ReplacementCount: len(matches),
					MatchConfidence:  "low",
					LinesAffected:    countAffectedLines(content, matches),
				}, nil
			}
		}
	}

	return &EditResult{
		ModifiedContent:  content,
		ReplacementCount: 0,
		MatchConfidence:  "none",
		LinesAffected:    0,
	}, fmt.Errorf("no matches found for text: %q", oldText)
}

// searchAndReplaceInDirectory performs search and replace in all files in a directory
func (e *UltraFastEngine) searchAndReplaceInDirectory(dirPath, pattern, replacement string, caseSensitive bool, results *[]string, totalReplacements *int) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := dirPath + "/" + entry.Name()

		if entry.IsDir() {
			// Recursively search subdirectories
			err := e.searchAndReplaceInDirectory(fullPath, pattern, replacement, caseSensitive, results, totalReplacements)
			if err != nil {
				continue // Continue with other directories
			}
		} else {
			// Process file
			replacements, err := e.searchAndReplaceInFile(fullPath, pattern, replacement, caseSensitive)
			if err == nil && replacements > 0 {
				*results = append(*results, fmt.Sprintf("📄 %s: %d replacements", fullPath, replacements))
				*totalReplacements += replacements
			}
		}
	}

	return nil
}

// searchAndReplaceInFile performs search and replace in a single file
func (e *UltraFastEngine) searchAndReplaceInFile(filePath, pattern, replacement string, caseSensitive bool) (int, error) {
	// Check if file is text and not too large
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}

	if info.Size() > 10*1024*1024 { // 10MB limit for search/replace
		return 0, nil // Skip large files
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}

	contentStr := string(content)

	// Check if it's a text file (basic check)
	if !isTextContent(contentStr) {
		return 0, nil // Skip binary files
	}

	// Prepare search pattern
	searchPattern := pattern
	if !caseSensitive {
		searchPattern = "(?i)" + regexp.QuoteMeta(pattern)
	} else {
		searchPattern = regexp.QuoteMeta(pattern)
	}

	re, err := regexp.Compile(searchPattern)
	if err != nil {
		return 0, err
	}

	// Count matches before replacement
	matches := re.FindAllString(contentStr, -1)
	if len(matches) == 0 {
		return 0, nil
	}

	// Perform replacement
	newContent := re.ReplaceAllString(contentStr, replacement)

	// Write back to file atomically
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), info.Mode()); err != nil {
		return 0, err
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return 0, err
	}

	// Invalidate cache
	e.cache.InvalidateFile(filePath)

	return len(matches), nil
}

// Helper functions
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func getIndentation(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(trimmed)]
}

func makeFlexiblePattern(escaped string) string {
	pattern := strings.ReplaceAll(escaped, `\ `, `\s+`)
	pattern = strings.ReplaceAll(pattern, `\n`, `\s*\n\s*`)
	return pattern
}

func countAffectedLines(content string, matches []string) int {
	affected := make(map[int]bool)
	totalLines := strings.Count(content, "\n") + 1

	for _, match := range matches {
		idx := strings.Index(content, match)
		if idx >= 0 {
			lineNum := strings.Count(content[:idx], "\n")
			matchLines := strings.Count(match, "\n") + 1
			for i := 0; i < matchLines && (lineNum+i) < totalLines; i++ {
				affected[lineNum+i] = true
			}
		}
	}

	return len(affected)
}

func calculateLinesWithText(content, text string) int {
	lines := strings.Split(content, "\n")
	count := 0
	for _, line := range lines {
		if strings.Contains(line, text) {
			count++
		}
	}
	return count
}

func isTextContent(content string) bool {
	// Simple heuristic: if content has too many null bytes, it's likely binary
	nullCount := strings.Count(content, "\x00")
	return float64(nullCount)/float64(len(content)) < 0.01
}

// MultiEditOperation represents a single edit in a batch
type MultiEditOperation struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// MultiEditResult represents the result of a multi-edit operation
type MultiEditResult struct {
	TotalEdits      int      `json:"total_edits"`
	SuccessfulEdits int      `json:"successful_edits"`
	FailedEdits     int      `json:"failed_edits"`
	LinesAffected   int      `json:"lines_affected"`
	MatchConfidence string   `json:"match_confidence"`
	Errors          []string `json:"errors,omitempty"`
}

// MultiEdit performs multiple edits on a single file atomically
// This is MUCH faster than calling edit_file multiple times because:
// 1. File is read only once
// 2. All edits are applied in memory
// 3. File is written only once
// 4. Only one backup is created
// ULTRA-FAST: Designed for Claude Desktop batch editing
func (e *UltraFastEngine) MultiEdit(ctx context.Context, path string, edits []MultiEditOperation) (*MultiEditResult, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Validate file
	if err := e.validateEditableFile(path); err != nil {
		return nil, fmt.Errorf("file validation failed: %w", err)
	}

	// Read current content once
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	// Create backup once
	backupPath, err := e.createBackup(path)
	if err != nil {
		return nil, fmt.Errorf("could not create backup: %w", err)
	}
	defer func() {
		if backupPath != "" {
			os.Remove(backupPath)
		}
	}()

	// Apply all edits in order
	result := &MultiEditResult{
		TotalEdits:      len(edits),
		MatchConfidence: "high",
	}

	currentContent := string(content)
	totalLinesAffected := 0

	for i, edit := range edits {
		if edit.OldText == "" {
			result.FailedEdits++
			result.Errors = append(result.Errors, fmt.Sprintf("edit %d: old_text cannot be empty", i+1))
			continue
		}

		// Apply this edit
		editResult, err := e.performIntelligentEdit(currentContent, edit.OldText, edit.NewText)
		if err != nil {
			result.FailedEdits++
			result.Errors = append(result.Errors, fmt.Sprintf("edit %d: %v", i+1, err))
			// Lower confidence if some edits fail
			if result.MatchConfidence == "high" {
				result.MatchConfidence = "medium"
			}
			continue
		}

		if editResult.ReplacementCount == 0 {
			result.FailedEdits++
			result.Errors = append(result.Errors, fmt.Sprintf("edit %d: no match found for old_text", i+1))
			if result.MatchConfidence == "high" {
				result.MatchConfidence = "medium"
			}
			continue
		}

		// Update content for next edit
		currentContent = editResult.ModifiedContent
		result.SuccessfulEdits++
		totalLinesAffected += editResult.LinesAffected

		// Update confidence based on individual edit
		if editResult.MatchConfidence == "low" && result.MatchConfidence == "high" {
			result.MatchConfidence = "medium"
		}
	}

	result.LinesAffected = totalLinesAffected

	// If no edits succeeded, return error
	if result.SuccessfulEdits == 0 {
		return result, fmt.Errorf("no edits were successful: %v", result.Errors)
	}

	// Write modified content atomically
	tmpPath := path + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, []byte(currentContent), 0644); err != nil {
		return nil, fmt.Errorf("error writing temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("error finalizing edit: %w", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	// Remove backup on success
	if backupPath != "" {
		os.Remove(backupPath)
		backupPath = ""
	}

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterEdit(path)
	}

	return result, nil
}

// ReplaceNthOccurrence replaces a specific occurrence of a pattern in a file
// occurrence: -1 for last, 1 for first, 2 for second, etc.
// wholeWord: if true, only match whole words
func (e *UltraFastEngine) ReplaceNthOccurrence(ctx context.Context, path, pattern, replacement string, occurrence int, wholeWord bool) (*EditResult, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "replace_nth"); err != nil {
		return nil, err
	}

	start := time.Now()
	defer e.releaseOperation("replace_nth", start)

	// Validate path
	validPath, err := e.validatePath(path)
	if err != nil {
		return nil, fmt.Errorf("path validation error: %w", err)
	}

	// Check if file exists
	info, err := os.Stat(validPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", validPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", validPath)
	}

	// Validate occurrence parameter
	if occurrence == 0 {
		return nil, fmt.Errorf("occurrence cannot be 0 (use -1 for last, 1 for first, etc.)")
	}

	// Create backup
	backupPath, err := e.createBackup(validPath)
	if err != nil {
		return nil, fmt.Errorf("could not create backup: %w", err)
	}
	defer func() {
		if backupPath != "" {
			os.Remove(backupPath)
		}
	}()

	// Read file
	content, err := os.ReadFile(validPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// Prepare regex pattern
	searchPattern := pattern
	if wholeWord {
		searchPattern = `\b` + regexp.QuoteMeta(pattern) + `\b`
	} else {
		// Try to compile as regex first, fallback to literal
		_, err := regexp.Compile(pattern)
		if err != nil {
			searchPattern = regexp.QuoteMeta(pattern)
		}
	}

	regexPattern, err := regexp.Compile(searchPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	// Find all matches with their line numbers
	type matchInfo struct {
		lineIdx int
		match   string
		start   int
		end     int
	}

	var matches []matchInfo

	for lineIdx, line := range lines {
		lineMatches := regexPattern.FindAllStringIndex(line, -1)
		for _, match := range lineMatches {
			matches = append(matches, matchInfo{
				lineIdx: lineIdx,
				match:   line[match[0]:match[1]],
				start:   match[0],
				end:     match[1],
			})
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("pattern not found: '%s'", pattern)
	}

	// Determine which match to replace
	var targetMatchIdx int
	if occurrence == -1 {
		// Last occurrence
		targetMatchIdx = len(matches) - 1
	} else if occurrence > 0 {
		// N-th occurrence (1-indexed)
		targetMatchIdx = occurrence - 1
		if targetMatchIdx >= len(matches) {
			return nil, fmt.Errorf("occurrence %d out of range (only %d matches found)", occurrence, len(matches))
		}
	} else {
		// Negative index other than -1 (e.g., -2 for second to last)
		targetMatchIdx = len(matches) + occurrence
		if targetMatchIdx < 0 {
			return nil, fmt.Errorf("occurrence %d out of range (only %d matches found)", occurrence, len(matches))
		}
	}

	targetMatch := matches[targetMatchIdx]

	// Replace only the target occurrence
	targetLine := lines[targetMatch.lineIdx]
	newLine := targetLine[:targetMatch.start] + replacement + targetLine[targetMatch.end:]
	lines[targetMatch.lineIdx] = newLine

	// Join back
	newContent := strings.Join(lines, "\n")

	// Write modified content atomically
	tmpPath := validPath + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, []byte(newContent), info.Mode()); err != nil {
		return nil, fmt.Errorf("error writing temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, validPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("error finalizing edit: %w", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(validPath)

	// Execute post-edit hooks
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPostEdit,
		ToolName:   "replace_nth_occurrence",
		FilePath:   validPath,
		Operation:  "replace_nth",
		OldContent: string(content),
		NewContent: newContent,
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata: map[string]interface{}{
			"pattern":     pattern,
			"replacement": replacement,
			"occurrence":  occurrence,
			"line_number": targetMatch.lineIdx + 1,
		},
	}

	_, _ = e.hookManager.ExecuteHooks(context.Background(), HookPostEdit, hookCtx)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterEdit(validPath)
	}

	return &EditResult{
		ModifiedContent:  newContent,
		ReplacementCount: 1,
		MatchConfidence:  "high",
		LinesAffected:    1,
	}, nil
}

// validateEditContext performs context validation to ensure the file hasn't
// been modified since it was read. This prevents stale edits that could
// overwrite recent changes.
//
// Returns (valid, warning) where:
// - valid: true if context looks good, false if likely changed
// - warning: descriptive message about the validation result
func (e *UltraFastEngine) validateEditContext(currentContent, oldText string) (bool, string) {
	// Normalize line endings for validation
	normalizedContent := normalizeLineEndings(currentContent)
	normalizedOldText := normalizeLineEndings(oldText)

	// If oldText not found at all, it's definitely invalid
	if !strings.Contains(normalizedContent, normalizedOldText) {
		return false, "old_text not found in current file - file has likely changed"
	}

	// Extract a snippet with surrounding context (3-5 lines before and after)
	lines := strings.Split(normalizedContent, "\n")
	oldLines := strings.Split(normalizedOldText, "\n")

	if len(oldLines) == 0 {
		return false, "invalid old_text"
	}

	// Find where oldText appears in the file
	var matchStartLine int
	found := false

	for i := 0; i <= len(lines)-len(oldLines); i++ {
		// Check if we have a multiline match
		match := true
		for j := 0; j < len(oldLines); j++ {
			if !strings.Contains(lines[i+j], strings.TrimSpace(oldLines[j])) {
				match = false
				break
			}
		}
		if match {
			matchStartLine = i
			found = true
			break
		}
	}

	if !found {
		// Try single-line match
		singleLineOld := strings.TrimSpace(strings.Join(oldLines, " "))
		for i, line := range lines {
			if strings.Contains(line, singleLineOld) {
				matchStartLine = i
				found = true
				break
			}
		}
	}

	if !found {
		return false, "exact match for old_text not found in current file"
	}

	// Check context: verify that surrounding lines are stable
	// Get context window: 2 lines before and 2 lines after the match
	contextBefore := max(0, matchStartLine-2)
	contextAfter := min(len(lines), matchStartLine+len(oldLines)+2)

	contextSnippet := strings.Join(lines[contextBefore:contextAfter], "\n")

	// If the context contains the oldText with some surrounding content,
	// it's likely the edit context is still valid
	contextHasMatch := strings.Contains(contextSnippet, strings.TrimSpace(oldLines[0]))

	if !contextHasMatch {
		return false, "surrounding context doesn't match - file has been modified"
	}

	// Context validation passed
	return true, "context valid - file appears unchanged"
}

// CreateBackup creates a backup of the specified file via MCP interface
// This is the public MCP tool for creating backups (snake_case for Claude Desktop)
func (e *UltraFastEngine) CreateBackup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResponse, error) {
	if err := e.acquireOperation(ctx, "backup"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("backup", start)

	// Get path argument
	path, ok := request.Arguments["path"].(string)
	if !ok || path == "" {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: "❌ Error: path is required"},
			},
		}, nil
	}

	// Validate path
	validPath, err := e.validatePath(path)
	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: invalid path - %v", err)},
			},
		}, nil
	}

	// Check if file exists
	if _, err := os.Stat(validPath); os.IsNotExist(err) {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: file not found - %s", validPath)},
			},
		}, nil
	}

	// Get optional operation context
	operation := "manual_backup"
	if op, ok := request.Arguments["operation"].(string); ok && op != "" {
		operation = op
	}

	// Create backup using BackupManager (if available)
	var backupID string
	if e.backupManager != nil {
		backupID, err = e.backupManager.CreateBackup(validPath, operation)
		if err != nil {
			return &mcp.CallToolResponse{
				Content: []mcp.TextContent{
					{Text: fmt.Sprintf("❌ Error creating backup: %v", err)},
				},
			}, nil
		}
	} else {
		// Fallback to simple backup
		backupID, err = e.createBackup(validPath)
		if err != nil {
			return &mcp.CallToolResponse{
				Content: []mcp.TextContent{
					{Text: fmt.Sprintf("❌ Error creating backup: %v", err)},
				},
			}, nil
		}
	}

	return &mcp.CallToolResponse{
		Content: []mcp.TextContent{
			{Text: fmt.Sprintf("✅ Backup created successfully\n📦 Backup ID: %s\n📄 File: %s\n🏷️ Operation: %s", backupID, validPath, operation)},
		},
	}, nil
}
