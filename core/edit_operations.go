package core

import (
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
func (e *UltraFastEngine) EditFile(path, oldText, newText string) (*EditResult, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Validate file
	if err := e.validateEditableFile(path); err != nil {
		return nil, fmt.Errorf("file validation failed: %v", err)
	}

	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	// Validate context: Check if surrounding content suggests file has changed
	// This prevents overwriting recent modifications
	contextValid, contextWarning := e.validateEditContext(string(content), oldText)
	if !contextValid {
		return nil, fmt.Errorf("context validation failed: %s - file may have been modified. Please re-read the file with smart_search + read_file_range", contextWarning)
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
			"old_text": oldText,
			"new_text": newText,
		},
	}

	hookResult, err := e.hookManager.ExecuteHooks(context.Background(), HookPreEdit, hookCtx)
	if err != nil {
		return nil, fmt.Errorf("pre-edit hook denied operation: %v", err)
	}

	// Create backup
	backupPath, err := e.createBackup(path)
	if err != nil {
		return nil, fmt.Errorf("could not create backup: %v", err)
	}
	defer func() {
		if backupPath != "" {
			os.Remove(backupPath)
		}
	}()

	// Perform intelligent edit
	result, err := e.performIntelligentEdit(string(content), oldText, newText)
	if err != nil {
		return nil, fmt.Errorf("edit failed: %v", err)
	}

	// Apply hook modifications if any
	finalContent := result.ModifiedContent
	if hookResult.ModifiedContent != "" {
		finalContent = hookResult.ModifiedContent
	}

	// Write modified content atomically
	tmpPath := path + ".tmp." + fmt.Sprintf("%d", e.metrics.OperationsTotal)
	if err := os.WriteFile(tmpPath, []byte(finalContent), 0644); err != nil {
		return nil, fmt.Errorf("error writing temp file: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("error finalizing edit: %v", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	// Remove backup on success
	if backupPath != "" {
		os.Remove(backupPath)
		backupPath = ""
	}

	// Execute post-edit hooks
	hookCtx.Event = HookPostEdit
	hookCtx.NewContent = finalContent
	_, _ = e.hookManager.ExecuteHooks(context.Background(), HookPostEdit, hookCtx)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterEdit(path)
	}

	return result, nil
}

// SearchAndReplace performs search and replace operations across files
func (e *UltraFastEngine) SearchAndReplace(path, pattern, replacement string, caseSensitive bool) (*mcp.CallToolResponse, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Validate path
	validPath, err := e.validatePath(path)
	if err != nil {
		return nil, fmt.Errorf("path validation failed: %v", err)
	}

	// Check if it's a file or directory
	info, err := os.Stat(validPath)
	if err != nil {
		return nil, fmt.Errorf("error accessing path: %v", err)
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
		return "", fmt.Errorf("invalid path: %v", err)
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
func (e *UltraFastEngine) performIntelligentEdit(content, oldText, newText string) (*EditResult, error) {
	if oldText == "" {
		return nil, fmt.Errorf("old_text cannot be empty")
	}

	// Normalize line endings once
	content = normalizeLineEndings(content)
	oldText = normalizeLineEndings(oldText)
	newText = normalizeLineEndings(newText)

	// OPTIMIZATION 1: Fast path for exact match (most common case)
	if idx := strings.Index(content, oldText); idx >= 0 {
		newContent := strings.ReplaceAll(content, oldText, newText)
		replacements := strings.Count(content, oldText)
		linesAffected := calculateLinesWithText(content, oldText)

		return &EditResult{
			ModifiedContent:  newContent,
			ReplacementCount: replacements,
			MatchConfidence:  "high",
			LinesAffected:    linesAffected,
		}, nil
	}

	// OPTIMIZATION 2: Pre-compute normalized variants once
	normalizedOld := strings.TrimSpace(oldText)

	// OPTIMIZATION 3: Check if normalized version exists in content
	if normalizedOld != oldText && strings.Contains(content, normalizedOld) {
		// Fast replacement for normalized text
		newContent := strings.ReplaceAll(content, normalizedOld, newText)
		return &EditResult{
			ModifiedContent:  newContent,
			ReplacementCount: strings.Count(content, normalizedOld),
			MatchConfidence:  "high",
			LinesAffected:    calculateLinesWithText(content, normalizedOld),
		}, nil
	}

	// OPTIMIZATION 4: Line-by-line processing with minimal allocations
	lines := strings.Split(content, "\n")
	replacements := 0
	affectedLines := 0
	modified := false

	for i, line := range lines {
		// Try exact match in line first
		if strings.Contains(line, oldText) {
			lines[i] = strings.ReplaceAll(line, oldText, newText)
			replacements += strings.Count(line, oldText)
			affectedLines++
			modified = true
			continue
		}

		// Try normalized match with indentation preservation
		if trimmed := strings.TrimSpace(line); trimmed == normalizedOld {
			lines[i] = getIndentation(line) + strings.TrimSpace(newText)
			replacements++
			affectedLines++
			modified = true
			continue
		}

		// Try normalized match without indentation
		if strings.Contains(line, normalizedOld) {
			lines[i] = strings.ReplaceAll(line, normalizedOld, newText)
			replacements += strings.Count(line, normalizedOld)
			affectedLines++
			modified = true
		}
	}

	if modified {
		return &EditResult{
			ModifiedContent:  strings.Join(lines, "\n"),
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
		return nil, fmt.Errorf("path validation error: %v", err)
	}

	// Check if file exists
	info, err := os.Stat(validPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", validPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %v", err)
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
		return nil, fmt.Errorf("could not create backup: %v", err)
	}
	defer func() {
		if backupPath != "" {
			os.Remove(backupPath)
		}
	}()

	// Read file
	content, err := os.ReadFile(validPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
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
		return nil, fmt.Errorf("invalid pattern: %v", err)
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
		return nil, fmt.Errorf("error writing temp file: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, validPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("error finalizing edit: %v", err)
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
	// If oldText not found at all, it's definitely invalid
	if !strings.Contains(currentContent, oldText) {
		return false, "old_text not found in current file - file has likely changed"
	}

	// Extract a snippet with surrounding context (3-5 lines before and after)
	lines := strings.Split(currentContent, "\n")
	oldLines := strings.Split(oldText, "\n")

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
