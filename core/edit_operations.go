package core

import (
	"bufio"
	"context"
	"encoding/json"
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
	RiskWarning      string // Non-blocking risk warning for MEDIUM/HIGH (empty if LOW/none)
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
func (e *UltraFastEngine) EditFile(ctx context.Context, path, oldText, newText string, force bool) (*EditResult, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore for concurrency control
	if err := e.acquireOperation(ctx, "edit"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("edit", start)

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}

	// Check if path is allowed (access control)
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return nil, &PathError{Op: "edit", Path: path, Err: fmt.Errorf("access denied")}
		}
	}

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
		return nil, fmt.Errorf("context validation failed: %s - file may have been modified. Please re-read the file with search_files + read_file", contextWarning)
	}

	// Calculate change impact for risk assessment
	impact := CalculateChangeImpact(string(content), oldText, newText, e.riskThresholds)

	// Create persistent backup BEFORE blocking decision (Bug #16)
	// Ensures backup exists even for blocked CRITICAL operations
	var backupID string
	if e.backupManager != nil {
		backupID, err = e.backupManager.CreateBackupWithContext(path, "edit_file",
			fmt.Sprintf("Edit: %d occurrences, %.1f%% change, risk=%s",
				impact.Occurrences, impact.ChangePercentage, impact.RiskLevel))
		if err != nil {
			return nil, fmt.Errorf("could not create backup: %w", err)
		}
	}

	// Bug #22: edit_file NEVER blocks — backup is already created, data is safe.
	// Blocking wastes tokens (Claude Desktop must resend the full old_text with force:true).
	// Risk warning is appended to the result instead.

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

	hookResult, err := e.hookManager.ExecuteHooks(ctx, HookPreEdit, hookCtx)
	if err != nil {
		return nil, fmt.Errorf("pre-edit hook denied operation: %w", err)
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

	// Write modified content atomically with secure random temp name
	tmpPath := path + ".tmp." + secureRandomSuffix()

	// Preserve original file permissions
	fileMode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		fileMode = info.Mode()
	}

	if err := os.WriteFile(tmpPath, []byte(finalContent), fileMode); err != nil {
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
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostEdit, hookCtx)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterEdit(path)
	}

	// Store backup ID in result
	result.BackupID = backupID

	// Attach risk warning for any risky operation (Bug #22: always warn, never block)
	if impact.IsRisky {
		result.RiskWarning = impact.FormatRiskNotice(backupID, path)
	}

	return result, nil
}

// SearchAndReplace performs search and replace operations across files
func (e *UltraFastEngine) SearchAndReplace(ctx context.Context, path, pattern, replacement string, caseSensitive bool) (*mcp.CallToolResponse, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore for concurrency control
	if err := e.acquireOperation(ctx, "search_replace"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("search_replace", start)

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}

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
	var info os.FileInfo
	var err error
	// Retry up to 3 times to handle transient Windows file locks
	// (antivirus, indexer, or concurrent operations can briefly block access)
	for attempt := 0; attempt < 3; attempt++ {
		info, err = os.Stat(path)
		if err == nil {
			break
		}
		if attempt < 2 {
			time.Sleep(100 * time.Millisecond)
		}
	}
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

// createBackup creates a backup of a file, preserving original permissions
func (e *UltraFastEngine) createBackup(path string) (string, error) {
	backupPath := path + ".backup"
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// Preserve original file permissions for backup
	fileMode := os.FileMode(0600) // restrictive default for backups
	if info, statErr := os.Stat(path); statErr == nil {
		fileMode = info.Mode()
	}
	err = os.WriteFile(backupPath, content, fileMode)
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

	// OPTIMIZATION 6: Literal escape normalization fallback (Bug #18)
	// LLMs (especially Claude Desktop) sometimes send old_text with literal \n
	// (backslash + n as two chars) instead of real newline characters.
	// Only attempted when: old_text has literal \n but NO real newlines,
	// meaning the LLM likely flattened a multiline string.
	// Safety: content is NOT converted (it already has real newlines),
	// only old_text and new_text are adjusted for matching.
	convertedOld := normalizeLiteralEscapes(oldText)
	if convertedOld != oldText {
		convertedNew := normalizeLiteralEscapes(newText)
		if idx := strings.Index(content, convertedOld); idx >= 0 {
			newContent := strings.ReplaceAll(content, convertedOld, convertedNew)
			replacements := strings.Count(content, convertedOld)
			return &EditResult{
				ModifiedContent:  newContent,
				ReplacementCount: replacements,
				MatchConfidence:  "high",
				LinesAffected:    strings.Count(convertedOld, "\n") + 1,
			}, nil
		}
	}

	// OPTIMIZATION 7: Tab ↔ space normalization fallback
	// LLMs often send spaces when the file uses tabs (or vice versa).
	// Normalize both content and oldText to spaces, find the match position,
	// then replace the original content at that position.
	if strings.ContainsAny(oldText, "\t ") {
		normalizedContent := normalizeIndentation(content)
		normalizedOldText := normalizeIndentation(oldText)
		if idx := strings.Index(normalizedContent, normalizedOldText); idx >= 0 {
			// Map normalized position back to original content.
			// Find the real substring by counting characters up to idx in original.
			origIdx := mapNormalizedIndex(content, normalizedContent, idx)
			origEnd := mapNormalizedIndex(content, normalizedContent, idx+len(normalizedOldText))
			if origIdx >= 0 && origEnd > origIdx && origEnd <= len(content) {
				originalMatch := content[origIdx:origEnd]
				replacements := 1
				// Preserve the indentation style of the file in newText
				fileUseTabs := strings.Count(originalMatch, "\t") > strings.Count(originalMatch, "    ")
				adjustedNew := newText
				if fileUseTabs {
					// File uses tabs — convert leading spaces in newText to tabs
					adjustedNew = convertLeadingSpacesToTabs(newText)
				} else {
					// File uses spaces — convert leading tabs in newText to spaces
					adjustedNew = convertLeadingTabsToSpaces(newText)
				}
				newContent := content[:origIdx] + adjustedNew + content[origEnd:]
				return &EditResult{
					ModifiedContent:  newContent,
					ReplacementCount: replacements,
					MatchConfidence:  "medium",
					LinesAffected:    strings.Count(originalMatch, "\n") + 1,
				}, nil
			}
		}
	}

	// OPTIMIZATION 8: Flexible regex search as last resort (expensive)
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

	// Write back to file atomically with secure random temp name
	tmpPath := filePath + ".tmp." + secureRandomSuffix()
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

// normalizeLiteralEscapes converts literal escape sequences (\\n, \\t) to real ones.
// LLMs sometimes send JSON-escaped newlines as literal two-char sequences in old_text.
// Example: "line1\\nline2" → "line1\nline2"
// Safety: only converts \\n if the text does NOT already contain real newlines,
// to avoid double-conversion when the string is already correct.
func normalizeLiteralEscapes(s string) string {
	if !strings.Contains(s, `\n`) {
		return s // No literal \n found, nothing to do
	}
	// If the string already has real newlines, the literal \n are likely intentional
	// (e.g., code that contains the string literal "\\n")
	if strings.Contains(s, "\n") {
		return s
	}
	// String has literal \n but no real newlines — convert them
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	return s
}

// trimTrailingSpacesPerLine strips trailing spaces and tabs from each line.
// Used as a whitespace-tolerant comparison fallback: editors and LLMs often
// disagree on whether lines should carry trailing whitespace.
func trimTrailingSpacesPerLine(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

func getIndentation(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(trimmed)]
}

// normalizeIndentation converts all leading whitespace to spaces (1 tab = 4 spaces)
// so that tab-vs-space differences don't block matching.
func normalizeIndentation(s string) string {
	lines := strings.Split(s, "\n")
	var sb strings.Builder
	sb.Grow(len(s))
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		indent := getIndentation(line)
		rest := line[len(indent):]
		normalized := strings.ReplaceAll(indent, "\t", "    ")
		sb.WriteString(normalized)
		sb.WriteString(rest)
	}
	return sb.String()
}

// mapNormalizedIndex maps a byte index in normalized text back to the corresponding
// byte index in the original text. Both texts must have the same line structure
// (only leading whitespace differs).
func mapNormalizedIndex(original, normalized string, normIdx int) int {
	if normIdx <= 0 {
		return 0
	}
	if normIdx >= len(normalized) {
		return len(original)
	}
	// Walk both strings in parallel, character by character
	oi := 0
	ni := 0
	for ni < normIdx && oi < len(original) {
		if original[oi] == '\n' && normalized[ni] == '\n' {
			oi++
			ni++
			continue
		}
		// At start of a line, skip whitespace in both versions together
		if ni < len(normalized) && (normalized[ni] == ' ' || normalized[ni] == '\t') &&
			oi < len(original) && (original[oi] == ' ' || original[oi] == '\t') {
			// Consume the entire leading whitespace on this line in both
			lineNi := ni
			lineOi := oi
			for lineNi < len(normalized) && (normalized[lineNi] == ' ' || normalized[lineNi] == '\t') {
				lineNi++
			}
			for lineOi < len(original) && (original[lineOi] == ' ' || original[lineOi] == '\t') {
				lineOi++
			}
			if normIdx <= lineNi {
				// Target is inside this whitespace block — proportional map
				normWS := lineNi - ni
				origWS := lineOi - oi
				frac := float64(normIdx-ni) / float64(normWS)
				return oi + int(frac*float64(origWS))
			}
			ni = lineNi
			oi = lineOi
			continue
		}
		oi++
		ni++
	}
	return oi
}

// convertLeadingSpacesToTabs converts leading spaces to tabs (4 spaces = 1 tab) per line.
func convertLeadingSpacesToTabs(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		indent := getIndentation(line)
		rest := line[len(indent):]
		// Convert groups of 4 spaces to tabs, leave remainder as spaces
		spacesOnly := strings.ReplaceAll(indent, "\t", "    ")
		tabs := strings.Repeat("\t", len(spacesOnly)/4)
		remainder := strings.Repeat(" ", len(spacesOnly)%4)
		lines[i] = tabs + remainder + rest
	}
	return strings.Join(lines, "\n")
}

// convertLeadingTabsToSpaces converts leading tabs to spaces (1 tab = 4 spaces) per line.
func convertLeadingTabsToSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		indent := getIndentation(line)
		rest := line[len(indent):]
		lines[i] = strings.ReplaceAll(indent, "\t", "    ") + rest
	}
	return strings.Join(lines, "\n")
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

// MultiEditOperation represents a single edit in a batch.
// Supports both old_text/new_text and old_str/new_str (Claude Desktop alias).
type MultiEditOperation struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// UnmarshalJSON implements custom JSON unmarshaling to accept both
// old_text/new_text and old_str/new_str parameter names.
// Claude Desktop sometimes uses old_str/new_str (from its native str_replace convention).
func (m *MultiEditOperation) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Accept old_text or old_str
	if v, ok := raw["old_text"].(string); ok && v != "" {
		m.OldText = v
	} else if v, ok := raw["old_str"].(string); ok && v != "" {
		m.OldText = v
	}

	// Accept new_text or new_str
	if v, ok := raw["new_text"].(string); ok && v != "" {
		m.NewText = v
	} else if v, ok := raw["new_str"].(string); ok && v != "" {
		m.NewText = v
	}

	return nil
}

// EditDetailStatus represents the outcome of a single edit within MultiEdit
type EditDetailStatus string

const (
	EditStatusApplied        EditDetailStatus = "applied"
	EditStatusAlreadyPresent EditDetailStatus = "already_present"
	EditStatusFailed         EditDetailStatus = "failed"
)

// EditDetail captures the per-edit outcome in a MultiEdit batch
type EditDetail struct {
	Index           int              `json:"index"`
	Status          EditDetailStatus `json:"status"`
	OldTextSnippet  string           `json:"old_text_snippet"`
	NewTextSnippet  string           `json:"new_text_snippet"`
	MatchConfidence string           `json:"match_confidence,omitempty"`
	Error           string           `json:"error,omitempty"`
}

// MultiEditResult represents the result of a multi-edit operation
type MultiEditResult struct {
	TotalEdits      int          `json:"total_edits"`
	SuccessfulEdits int          `json:"successful_edits"`
	FailedEdits     int          `json:"failed_edits"`
	SkippedEdits    int          `json:"skipped_edits"`
	LinesAffected   int          `json:"lines_affected"`
	MatchConfidence string       `json:"match_confidence"`
	Errors          []string     `json:"errors,omitempty"`
	BackupID        string       `json:"backup_id,omitempty"`
	RiskWarning     string       `json:"risk_warning,omitempty"`
	EditDetails     []EditDetail `json:"edit_details,omitempty"`
}

// MultiEdit performs multiple edits on a single file atomically
// This is MUCH faster than calling edit_file multiple times because:
// 1. File is read only once
// 2. All edits are applied in memory
// 3. File is written only once
// 4. Only one backup is created
// ULTRA-FAST: Designed for Claude Desktop batch editing
// Bug #17: Added risk assessment, context validation, hooks, per-edit detail, and "already_present" detection
func (e *UltraFastEngine) MultiEdit(ctx context.Context, path string, edits []MultiEditOperation, force bool) (*MultiEditResult, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Check if path is allowed (access control)
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return nil, &PathError{Op: "multi_edit", Path: path, Err: fmt.Errorf("access denied")}
		}
	}

	// Validate file
	if err := e.validateEditableFile(path); err != nil {
		return nil, fmt.Errorf("file validation failed: %w", err)
	}

	// Read current content once
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}
	originalContent := string(content)

	// Context validation (Bug #17 — parity with EditFile)
	// In multi_edit, individual edits may legitimately fail (partial success).
	// Only hard-block if NO edit passes context validation and none are "already_present".
	validEditCount := 0
	for _, edit := range edits {
		if edit.OldText == "" {
			continue
		}
		contextValid, _ := e.validateEditContext(originalContent, edit.OldText)
		if contextValid {
			validEditCount++
			continue
		}
		// Check "already_present" — newText in file, oldText absent
		normalizedContent := normalizeLineEndings(originalContent)
		normalizedNew := normalizeLineEndings(edit.NewText)
		normalizedOld := normalizeLineEndings(edit.OldText)
		// Also try with literal escapes converted (Bug #22: LLMs send \n as two chars)
		convertedOld := normalizeLiteralEscapes(normalizedOld)
		convertedNew := normalizeLiteralEscapes(normalizedNew)

		oldAbsent := !strings.Contains(normalizedContent, normalizedOld) && !strings.Contains(normalizedContent, convertedOld)
		newPresent := edit.NewText != "" && (strings.Contains(normalizedContent, normalizedNew) || strings.Contains(normalizedContent, convertedNew))

		if newPresent && oldAbsent {
			validEditCount++
		}
		// Otherwise: this edit will fail in the loop — that's OK for partial success
	}
	if validEditCount == 0 && len(edits) > 0 {
		return nil, fmt.Errorf("context validation failed: none of the %d edits match the current file content", len(edits))
	}

	// Risk assessment: simulate all edits to compute aggregate impact (Bug #17)
	simContent := originalContent
	for _, edit := range edits {
		if edit.OldText == "" {
			continue
		}
		simResult, simErr := e.performIntelligentEdit(simContent, edit.OldText, edit.NewText)
		if simErr == nil && simResult.ReplacementCount > 0 {
			simContent = simResult.ModifiedContent
		}
	}
	aggregateImpact := calculateMultiEditImpact(originalContent, simContent, e.riskThresholds)

	// Create persistent backup BEFORE blocking decision (Bug #16)
	var backupID string
	if e.backupManager != nil {
		backupID, err = e.backupManager.CreateBackupWithContext(path, "multi_edit",
			fmt.Sprintf("MultiEdit: %d edits, risk=%s", len(edits), aggregateImpact.RiskLevel))
		if err != nil {
			return nil, fmt.Errorf("could not create backup: %w", err)
		}
	}

	// Bug #22: multi_edit NEVER blocks — backup is already created, data is safe.
	// Blocking wastes tokens (Claude Desktop must resend the full old_text with force:true).
	// Risk warning is appended to the result instead.

	// Execute pre-edit hooks (Bug #17 — parity with EditFile)
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreEdit,
		ToolName:   "multi_edit",
		FilePath:   path,
		Operation:  "multi_edit",
		OldContent: originalContent,
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata: map[string]interface{}{
			"edit_count":     len(edits),
			"risk_level":     aggregateImpact.RiskLevel,
			"change_percent": aggregateImpact.ChangePercentage,
		},
	}
	hookResult, err := e.hookManager.ExecuteHooks(ctx, HookPreEdit, hookCtx)
	if err != nil {
		return nil, fmt.Errorf("pre-edit hook denied operation: %w", err)
	}

	// Apply all edits in order with per-edit detail tracking (Bug #17)
	result := &MultiEditResult{
		TotalEdits:      len(edits),
		MatchConfidence: "high",
		EditDetails:     make([]EditDetail, 0, len(edits)),
	}

	currentContent := originalContent
	totalLinesAffected := 0

	for i, edit := range edits {
		detail := EditDetail{
			Index:          i,
			OldTextSnippet: truncateText(edit.OldText, 60),
			NewTextSnippet: truncateText(edit.NewText, 60),
		}

		if edit.OldText == "" {
			detail.Status = EditStatusFailed
			detail.Error = "old_text cannot be empty"
			result.FailedEdits++
			result.Errors = append(result.Errors, fmt.Sprintf("edit %d: old_text cannot be empty", i+1))
			result.EditDetails = append(result.EditDetails, detail)
			continue
		}

		// Apply this edit
		editResult, editErr := e.performIntelligentEdit(currentContent, edit.OldText, edit.NewText)
		if editErr != nil || editResult.ReplacementCount == 0 {
			// Check "already_present": newText in currentContent AND oldText absent (Bug #17)
			normalizedCurrent := normalizeLineEndings(currentContent)
			normalizedNew := normalizeLineEndings(edit.NewText)
			normalizedOld := normalizeLineEndings(edit.OldText)
			// Also try with literal escapes converted (Bug #22)
			convertedOld := normalizeLiteralEscapes(normalizedOld)
			convertedNew := normalizeLiteralEscapes(normalizedNew)

			oldAbsent := !strings.Contains(normalizedCurrent, normalizedOld) && !strings.Contains(normalizedCurrent, convertedOld)
			newPresent := edit.NewText != "" && (strings.Contains(normalizedCurrent, normalizedNew) || strings.Contains(normalizedCurrent, convertedNew))

			if newPresent && oldAbsent {
				detail.Status = EditStatusAlreadyPresent
				detail.MatchConfidence = "high"
				result.SkippedEdits++
				result.EditDetails = append(result.EditDetails, detail)
				continue
			}

			// Genuine failure
			detail.Status = EditStatusFailed
			if editErr != nil {
				detail.Error = editErr.Error()
				result.Errors = append(result.Errors, fmt.Sprintf("edit %d: %v", i+1, editErr))
			} else {
				detail.Error = "no match found for old_text"
				result.Errors = append(result.Errors, fmt.Sprintf("edit %d: no match found for old_text", i+1))
			}
			result.FailedEdits++
			if result.MatchConfidence == "high" {
				result.MatchConfidence = "medium"
			}
			result.EditDetails = append(result.EditDetails, detail)
			continue
		}

		// Success: edit applied
		detail.Status = EditStatusApplied
		detail.MatchConfidence = editResult.MatchConfidence
		currentContent = editResult.ModifiedContent
		result.SuccessfulEdits++
		totalLinesAffected += editResult.LinesAffected

		if editResult.MatchConfidence == "low" && result.MatchConfidence == "high" {
			result.MatchConfidence = "medium"
		}
		result.EditDetails = append(result.EditDetails, detail)
	}

	result.LinesAffected = totalLinesAffected

	// If no edits succeeded and none were already_present, return error
	if result.SuccessfulEdits == 0 && result.SkippedEdits == 0 {
		return result, fmt.Errorf("no edits were successful: %v", result.Errors)
	}

	// If only "already_present" edits (nothing changed), skip write
	if result.SuccessfulEdits == 0 {
		result.BackupID = backupID
		if aggregateImpact.IsRisky && !aggregateImpact.ShouldBlockOperation(false) {
			result.RiskWarning = aggregateImpact.FormatRiskNotice(backupID, path)
		}
		return result, nil
	}

	// Apply hook modifications if any
	finalContent := currentContent
	if hookResult.ModifiedContent != "" {
		finalContent = hookResult.ModifiedContent
	}

	// Write modified content atomically with secure random temp name
	tmpPath := path + ".tmp." + secureRandomSuffix()

	// Preserve original file permissions
	fileMode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		fileMode = info.Mode()
	}

	if err := os.WriteFile(tmpPath, []byte(finalContent), fileMode); err != nil {
		return nil, fmt.Errorf("error writing temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("error finalizing edit: %w", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	// DO NOT remove backup - keep it persistent for recovery (Bug #16)

	// Execute post-edit hooks (Bug #17 — parity with EditFile)
	hookCtx.Event = HookPostEdit
	hookCtx.NewContent = finalContent
	hookCtx.Metadata["backup_id"] = backupID
	hookCtx.Metadata["successful_edits"] = result.SuccessfulEdits
	hookCtx.Metadata["skipped_edits"] = result.SkippedEdits
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostEdit, hookCtx)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterEdit(path)
	}

	// Store backup ID and attach risk warning (Bug #22: always warn, never block)
	result.BackupID = backupID
	if aggregateImpact.IsRisky {
		result.RiskWarning = aggregateImpact.FormatRiskNotice(backupID, path)
	}

	return result, nil
}

// calculateMultiEditImpact computes aggregate risk by comparing original to final content.
// This avoids double-counting that would occur with per-edit CalculateChangeImpact on overlapping edits.
func calculateMultiEditImpact(originalContent, finalContent string, thresholds RiskThresholds) *ChangeImpact {
	impact := &ChangeImpact{
		TotalLines:  len(strings.Split(originalContent, "\n")),
		RiskFactors: []string{},
	}

	if originalContent == finalContent {
		impact.RiskLevel = "low"
		return impact
	}

	// Character-level diff approximation
	oldLen := len(originalContent)
	newLen := len(finalContent)

	// Length difference
	var lenDiff int
	if oldLen > newLen {
		lenDiff = oldLen - newLen
	} else {
		lenDiff = newLen - oldLen
	}

	// Count differing characters in the overlapping portion
	minLen := oldLen
	if newLen < minLen {
		minLen = newLen
	}
	diffChars := 0
	for i := 0; i < minLen; i++ {
		if originalContent[i] != finalContent[i] {
			diffChars++
		}
	}

	impact.CharactersChanged = int64(lenDiff + diffChars)

	if oldLen > 0 {
		impact.ChangePercentage = (float64(impact.CharactersChanged) / float64(oldLen)) * 100.0
	}

	// Apply same risk thresholds as CalculateChangeImpact
	impact.RiskLevel = "low"
	impact.IsRisky = false

	if impact.ChangePercentage >= 90.0 {
		impact.RiskLevel = "critical"
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("Almost complete file rewrite (%.1f%%)", impact.ChangePercentage))
	} else if impact.ChangePercentage >= thresholds.HighPercentage {
		impact.RiskLevel = "high"
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("Large portion of file affected (%.1f%%)", impact.ChangePercentage))
	} else if impact.ChangePercentage >= thresholds.MediumPercentage {
		impact.RiskLevel = "medium"
		impact.IsRisky = true
		impact.RiskFactors = append(impact.RiskFactors,
			fmt.Sprintf("Significant changes (%.1f%% of file)", impact.ChangePercentage))
	}

	return impact
}

// truncateText returns the first n characters of s, appending "..." if truncated
func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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

	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostEdit, hookCtx)

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
	// effectiveOldText is used for context checking — updated if literal escape conversion applies
	effectiveOldText := normalizedOldText

	// Level 1: exact normalized match (fastest, most common case)
	exactMatch := strings.Contains(normalizedContent, normalizedOldText)

	if !exactMatch {
		// Level 1.5: Literal escape normalization (Bug #18)
		// LLMs may send literal \n instead of real newlines
		convertedOld := normalizeLiteralEscapes(normalizedOldText)
		if convertedOld != normalizedOldText && strings.Contains(normalizedContent, convertedOld) {
			exactMatch = true
			effectiveOldText = convertedOld
		}
	}

	if !exactMatch {
		// Level 2: trailing-whitespace-tolerant match.
		// Editors and LLMs often disagree on trailing spaces/tabs per line.
		// If the text matches when trailing whitespace is stripped, the file
		// has NOT changed — only the whitespace representation differs.
		// performIntelligentEdit handles the actual replacement via OPTIMIZATION 6.
		trimmedContent := trimTrailingSpacesPerLine(normalizedContent)
		trimmedOld := trimTrailingSpacesPerLine(normalizedOldText)
		if !strings.Contains(trimmedContent, trimmedOld) {
			// Also try with literal escapes converted + trimmed (Bug #18)
			convertedOld := normalizeLiteralEscapes(normalizedOldText)
			trimmedConverted := trimTrailingSpacesPerLine(convertedOld)
			if convertedOld != normalizedOldText && strings.Contains(trimmedContent, trimmedConverted) {
				// Match found with literal escape conversion — let performIntelligentEdit handle it
			} else {
				lineCount := strings.Count(normalizedOldText, "\n") + 1
				return false, fmt.Sprintf(
					"old_text not found in file (%d line(s)). "+
						"ALWAYS read the file with read_file BEFORE editing. "+
						"Copy the exact text from the read result as old_text",
					lineCount,
				)
			}
		}
		// Whitespace-tolerant match found — proceed to context check.
		// performIntelligentEdit will resolve the actual replacement.
	}

	// Extract a snippet with surrounding context (3-5 lines before and after)
	// Use effectiveOldText which may have literal escapes converted (Bug #18)
	lines := strings.Split(normalizedContent, "\n")
	oldLines := strings.Split(effectiveOldText, "\n")

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
		return false, "old_text not found. ALWAYS read the file with read_file BEFORE editing. Copy the exact text from the read result as old_text"
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
