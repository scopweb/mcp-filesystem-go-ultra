package core

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Point 4: line-range deletion primitive and atomic block extraction.
//
// edit_file historically only edited by exact text match — there was no way to
// remove "lines X..Y" and no atomic "move this block to another file". The
// latter previously required two manual steps (write the block to a .cs, then
// delete it from the .razor) with no guarantee the bytes written matched the
// bytes deleted. ComputeLineRangeDeletion is the single source of truth that
// closes that gap: the SAME computed slice is what the batch "extract" action
// writes to the destination and removes from the source.

// ComputeLineRangeDeletion splits content into lines (preserving exact bytes,
// including each line's terminator) and returns the text of lines
// [startLine, endLine] (1-based, inclusive) together with the content that
// remains once those lines are removed. Byte-exact: removed and remaining are
// substrings of the original joined back with no transformation.
func ComputeLineRangeDeletion(content string, startLine, endLine int) (removed, remaining string, err error) {
	lines := strings.SplitAfter(content, "\n")
	// SplitAfter on a trailing newline yields a final "" element; drop it so the
	// line numbering matches read_file's 1-based count.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	total := len(lines)
	if total == 0 {
		return "", "", fmt.Errorf("file is empty: nothing to extract")
	}
	if startLine < 1 {
		return "", "", fmt.Errorf("start_line must be >= 1, got %d", startLine)
	}
	if endLine < startLine {
		return "", "", fmt.Errorf("end_line (%d) must be >= start_line (%d)", endLine, startLine)
	}
	if startLine > total {
		return "", "", fmt.Errorf("start_line %d is beyond end of file (%d lines)", startLine, total)
	}
	if endLine > total {
		endLine = total
	}
	removed = strings.Join(lines[startLine-1:endLine], "")
	remaining = strings.Join(lines[:startLine-1], "") + strings.Join(lines[endLine:], "")
	return removed, remaining, nil
}

// ComputeLineRangeReplacement splits content into lines (byte-exact, preserving
// terminators) and replaces lines [startLine, endLine] (1-based, inclusive) with
// newText. Returns the text removed and the resulting content. Newline rules:
//   - If there are lines after the range (suffix non-empty) and newText doesn't
//     end in "\n", one is added so the replacement doesn't glue onto the next
//     line.
//   - If replacing through the last line, a trailing "\n" is added only when the
//     original file ended with one (preserves the file's trailing-newline state).
//   - An empty newText is a pure deletion (prefix+suffix), equivalent to
//     ComputeLineRangeDeletion.
func ComputeLineRangeReplacement(content string, startLine, endLine int, newText string) (removed, result string, err error) {
	lines := strings.SplitAfter(content, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	total := len(lines)
	if total == 0 {
		return "", "", fmt.Errorf("file is empty: nothing to replace")
	}
	if startLine < 1 {
		return "", "", fmt.Errorf("start_line must be >= 1, got %d", startLine)
	}
	if endLine < startLine {
		return "", "", fmt.Errorf("end_line (%d) must be >= start_line (%d)", endLine, startLine)
	}
	if startLine > total {
		return "", "", fmt.Errorf("start_line %d is beyond end of file (%d lines)", startLine, total)
	}
	if endLine > total {
		endLine = total
	}

	prefix := strings.Join(lines[:startLine-1], "")
	removed = strings.Join(lines[startLine-1:endLine], "")
	suffix := strings.Join(lines[endLine:], "")

	block := newText
	if block != "" {
		needNL := suffix != "" || strings.HasSuffix(content, "\n")
		if needNL && !strings.HasSuffix(block, "\n") {
			block += "\n"
		}
	}
	return removed, prefix + block + suffix, nil
}

// countRemovedLines counts the lines represented by a removed slice (handles a
// final line without a trailing newline).
func countRemovedLines(removed string) int {
	if removed == "" {
		return 0
	}
	n := strings.Count(removed, "\n")
	if !strings.HasSuffix(removed, "\n") {
		n++
	}
	return n
}

// DeleteLineRange removes lines [startLine, endLine] (1-based, inclusive) from
// the file at path, writing atomically and creating a backup (mirroring
// EditFile). It returns the exact text removed so callers can reuse it; the
// batch "extract" action relies on this to guarantee written == deleted.
func (e *UltraFastEngine) DeleteLineRange(ctx context.Context, path string, startLine, endLine int) (removed string, result *EditResult, err error) {
	path = NormalizePath(path)

	if err := e.acquireOperation(ctx, "edit"); err != nil {
		return "", nil, err
	}
	start := time.Now()
	defer e.releaseOperation("edit", start)

	if err := ctx.Err(); err != nil {
		return "", nil, fmt.Errorf("operation cancelled: %w", err)
	}
	if !e.IsPathAllowed(path) {
		return "", nil, &PathError{Op: "delete_range", Path: path, Err: fmt.Errorf("access denied")}
	}
	if err := e.validateEditableFile(path); err != nil {
		return "", nil, fmt.Errorf("file validation failed: %w", err)
	}

	contentBytes, rerr := os.ReadFile(path)
	if rerr != nil {
		return "", nil, fmt.Errorf("error reading file: %w", rerr)
	}
	content := string(contentBytes)

	removed, remaining, derr := ComputeLineRangeDeletion(content, startLine, endLine)
	if derr != nil {
		return "", nil, derr
	}

	// Backup before write (parity with EditFile).
	var backupID string
	if e.backupManager != nil {
		e.backupChainMu.RLock()
		previousBackupID := e.backupChain[path]
		e.backupChainMu.RUnlock()
		backupID, err = e.backupManager.CreateBackupWithContextAndParent(path, "delete_range",
			fmt.Sprintf("Delete lines %d-%d", startLine, endLine), previousBackupID)
		if err != nil {
			return "", nil, fmt.Errorf("could not create backup: %w", err)
		}
		e.backupChainMu.Lock()
		e.backupChain[path] = backupID
		e.backupChainMu.Unlock()
	}

	fileMode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		fileMode = info.Mode()
	}
	if werr := atomicWriteFile(path, []byte(remaining), fileMode); werr != nil {
		return "", nil, fmt.Errorf("error writing file: %w", werr)
	}
	e.invalidateFileReadCache(path)

	removedLines := countRemovedLines(removed)
	result = &EditResult{
		ReplacementCount: 1,
		MatchConfidence:  "exact",
		LinesAffected:    removedLines,
		LinesRemoved:     removedLines,
		TotalLines:       strings.Count(remaining, "\n") + 1,
		BackupID:         backupID,
	}
	// Point 2 / AST-Go: structural check on the resulting file.
	if warn := CheckStructureDelta(content, remaining, path); warn != "" {
		result.StructureWarning = warn
		SetIntegrityStatus(ctx, "WARNING", warn)
	}
	// New point 1: post-edit content_hash for re-read-free chaining.
	result.NewHash = contentHashFNV(remaining)
	return removed, result, nil
}

// ReplaceLineRange replaces lines [startLine, endLine] (1-based, inclusive) with
// newText, writing atomically and creating a backup (mirroring DeleteLineRange).
// It's the line-numbered counterpart to text-match editing — the natural partner
// to read_file's range output: read lines X..Y, replace exactly those (new point 2).
func (e *UltraFastEngine) ReplaceLineRange(ctx context.Context, path string, startLine, endLine int, newText string) (result *EditResult, err error) {
	path = NormalizePath(path)

	if err := e.acquireOperation(ctx, "edit"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("edit", start)

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}
	if !e.IsPathAllowed(path) {
		return nil, &PathError{Op: "replace_range", Path: path, Err: fmt.Errorf("access denied")}
	}
	if err := e.validateEditableFile(path); err != nil {
		return nil, fmt.Errorf("file validation failed: %w", err)
	}

	contentBytes, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, fmt.Errorf("error reading file: %w", rerr)
	}
	content := string(contentBytes)

	removed, remaining, derr := ComputeLineRangeReplacement(content, startLine, endLine, newText)
	if derr != nil {
		return nil, derr
	}

	// Backup before write (parity with EditFile/DeleteLineRange).
	var backupID string
	if e.backupManager != nil {
		e.backupChainMu.RLock()
		previousBackupID := e.backupChain[path]
		e.backupChainMu.RUnlock()
		backupID, err = e.backupManager.CreateBackupWithContextAndParent(path, "replace_range",
			fmt.Sprintf("Replace lines %d-%d", startLine, endLine), previousBackupID)
		if err != nil {
			return nil, fmt.Errorf("could not create backup: %w", err)
		}
		e.backupChainMu.Lock()
		e.backupChain[path] = backupID
		e.backupChainMu.Unlock()
	}

	fileMode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		fileMode = info.Mode()
	}
	if werr := atomicWriteFile(path, []byte(remaining), fileMode); werr != nil {
		return nil, fmt.Errorf("error writing file: %w", werr)
	}
	e.invalidateFileReadCache(path)

	removedLines := countRemovedLines(removed)
	addedLines := countRemovedLines(newText)
	result = &EditResult{
		ReplacementCount: 1,
		MatchConfidence:  "exact",
		LinesAffected:    removedLines,
		LinesRemoved:     removedLines,
		LinesAdded:       addedLines,
		TotalLines:       strings.Count(remaining, "\n") + 1,
		BackupID:         backupID,
	}
	// Point 2 / AST-Go: structural check on the resulting file.
	if warn := CheckStructureDelta(content, remaining, path); warn != "" {
		result.StructureWarning = warn
		SetIntegrityStatus(ctx, "WARNING", warn)
	}
	// New point 1: post-edit content_hash for re-read-free chaining.
	result.NewHash = contentHashFNV(remaining)
	return result, nil
}
