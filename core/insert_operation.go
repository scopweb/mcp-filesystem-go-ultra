package core

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// InsertAtAnchor inserts text before or after the unique occurrence of anchor
// without replacing anything (feedback 2026-08-05, fricción 3: "insertar tras
// anchor" is the dominant real-world edit pattern and must not trip the
// accidental-rewrite guard). The anchor itself is preserved byte-for-byte and
// the inserted text is placed on its own line(s): whatever followed the anchor
// keeps its line structure. Line endings are normalized to LF for matching and
// the file's original EOL style is restored on write (Bug #33 convention).
func (e *UltraFastEngine) InsertAtAnchor(ctx context.Context, path, anchor, text, position string, dryRun bool) (*EditResult, error) {
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
		return nil, e.AccessDeniedError("insert", path)
	}
	if err := e.validateEditableFile(path); err != nil {
		return nil, fmt.Errorf("file validation failed: %w", err)
	}
	if anchor == "" {
		return nil, fmt.Errorf("anchor cannot be empty")
	}
	if position != "after" && position != "before" {
		return nil, fmt.Errorf("position must be \"after\" or \"before\"")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}
	originalEOL := detectEOL(string(raw))
	content := normalizeLineEndings(string(raw))
	anchorN := normalizeLineEndings(anchor)
	textN := normalizeLineEndings(text)

	count := strings.Count(content, anchorN)
	if count == 0 {
		return nil, fmt.Errorf("anchor not found: %q", anchor)
	}
	if count > 1 {
		return nil, fmt.Errorf("anchor matches %d times (expected 1). Quote more surrounding context to make it unique", count)
	}

	matchStart := strings.Index(content, anchorN)
	matchEnd := matchStart + len(anchorN)

	var combined string
	if position == "before" {
		insert := textN
		if !strings.HasSuffix(insert, "\n") {
			insert += "\n"
		}
		// Anchor not at a line start → push it onto its own new line.
		if matchStart > 0 && content[matchStart-1] != '\n' {
			insert = "\n" + insert
		}
		combined = insert + anchorN
	} else {
		combined = anchorN + textN
		if !strings.HasSuffix(anchorN, "\n") {
			combined = anchorN + "\n" + textN
		}
		// Keep whatever follows the anchor on its own line. If the suffix
		// already starts with "\n" (the anchor's own line ending), no extra
		// separator is needed.
		if matchEnd < len(content) && !strings.HasSuffix(combined, "\n") && content[matchEnd] != '\n' {
			combined += "\n"
		}
	}

	newContent := content[:matchStart] + combined + content[matchEnd:]

	result := &EditResult{
		ModifiedContent:  newContent,
		ReplacementCount: 1,
		MatchConfidence:  "high",
		LinesAffected:    strings.Count(textN, "\n") + 1,
		StartLine:        strings.Count(content[:matchStart], "\n") + 1,
	}

	if dryRun {
		return result, nil
	}

	// Persistent backup with chain, same convention as EditFile.
	var backupID string
	if e.backupManager != nil {
		e.backupChainMu.RLock()
		previousBackupID := e.backupChain[path]
		e.backupChainMu.RUnlock()

		backupID, err = e.backupManager.CreateBackupWithContextAndParent(path, "edit_file",
			fmt.Sprintf("Insert %s anchor: %d lines", position, strings.Count(textN, "\n")+1), previousBackupID)
		if err != nil {
			return nil, fmt.Errorf("could not create backup: %w", err)
		}
		e.backupChainMu.Lock()
		e.backupChain[path] = backupID
		e.backupChainMu.Unlock()
	}
	result.BackupID = backupID

	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreEdit,
		ToolName:   "edit_file",
		FilePath:   path,
		Operation:  "insert",
		OldContent: string(raw),
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata: map[string]interface{}{
			"anchor":   anchor,
			"position": position,
		},
	}
	hookResult, err := e.hookManager.ExecuteHooks(ctx, HookPreEdit, hookCtx)
	if err != nil {
		return nil, fmt.Errorf("pre-edit hook denied operation: %w", err)
	}

	finalContent := newContent
	if hookResult != nil && hookResult.ModifiedContent != "" {
		finalContent = hookResult.ModifiedContent
	}
	finalContent = restoreEOL(finalContent, originalEOL)

	tmpPath := path + ".tmp." + secureRandomSuffix()
	fileMode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		fileMode = info.Mode()
	}
	if err := os.WriteFile(tmpPath, []byte(finalContent), fileMode); err != nil {
		return nil, fmt.Errorf("error writing temp file: %w", err)
	}
	if err := renameWithRetry(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("error finalizing insert: %w", err)
	}
	e.invalidateMutatedPath(path)

	hookCtx.Event = HookPostEdit
	hookCtx.NewContent = finalContent
	hookCtx.Metadata["backup_id"] = backupID
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostEdit, hookCtx)

	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterEdit(path)
	}

	result.TotalLines = CountLines(finalContent)
	if added, removed, exact := DiffCounts(string(raw), finalContent); exact {
		result.LinesAdded = added
		result.LinesRemoved = removed
	} else {
		result.LinesAdded = strings.Count(textN, "\n") + 1
		result.LinesRemoved = 0
	}
	result.NewHash = contentHashFNV(finalContent)
	return result, nil
}
