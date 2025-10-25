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
func (e *UltraFastEngine) EditFile(path, oldText, newText string) (*EditResult, error) {
	// Validate file
	if err := e.validateEditableFile(path); err != nil {
		return nil, fmt.Errorf("file validation failed: %v", err)
	}

	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

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

	return result, nil
}

// SearchAndReplace performs search and replace operations across files
func (e *UltraFastEngine) SearchAndReplace(path, pattern, replacement string, caseSensitive bool) (*mcp.CallToolResponse, error) {
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

// performIntelligentEdit performs intelligent text replacement
func (e *UltraFastEngine) performIntelligentEdit(content, oldText, newText string) (*EditResult, error) {
	if oldText == "" {
		return nil, fmt.Errorf("old_text cannot be empty")
	}

	// Normalize line endings
	content = normalizeLineEndings(content)
	oldText = normalizeLineEndings(oldText)
	newText = normalizeLineEndings(newText)

	// Fast path: Check exact match first (most common)
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

	// Flexible search if no exact match
	lines := strings.Split(content, "\n")
	newLines := make([]string, len(lines))
	replacements := 0
	affectedLines := 0

	normalizedOld := strings.TrimSpace(oldText)

	// Try line by line replacement
	for i, line := range lines {
		newLine := line

		if strings.Contains(line, oldText) {
			newLine = strings.ReplaceAll(line, oldText, newText)
			replacements += strings.Count(line, oldText)
			affectedLines++
		} else if trimmed := strings.TrimSpace(line); trimmed == normalizedOld {
			newLine = getIndentation(line) + strings.TrimSpace(newText)
			replacements++
			affectedLines++
		} else if strings.Contains(line, normalizedOld) {
			newLine = strings.ReplaceAll(line, normalizedOld, newText)
			replacements += strings.Count(line, normalizedOld)
			affectedLines++
		}

		newLines[i] = newLine
	}

	// If no replacements found, try multiline search
	if replacements == 0 {
		if strings.Contains(content, oldText) {
			newContent := strings.ReplaceAll(content, oldText, newText)
			return &EditResult{
				ModifiedContent:  newContent,
				ReplacementCount: 1,
				MatchConfidence:  "medium",
				LinesAffected:    strings.Count(oldText, "\n") + 1,
			}, nil
		}

		// Last resort: flexible regex search
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

	if replacements > 0 {
		return &EditResult{
			ModifiedContent:  strings.Join(newLines, "\n"),
			ReplacementCount: replacements,
			MatchConfidence:  "medium",
			LinesAffected:    affectedLines,
		}, nil
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

	return &EditResult{
		ModifiedContent:  newContent,
		ReplacementCount: 1,
		MatchConfidence:  "high",
		LinesAffected:    1,
	}, nil
}
