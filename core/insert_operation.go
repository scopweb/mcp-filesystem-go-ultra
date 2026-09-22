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
	if anchor == "" {
		return nil, fmt.Errorf("anchor cannot be empty")
	}
	if position != "after" && position != "before" {
		return nil, fmt.Errorf("position must be \"after\" or \"before\"")
	}

	ctx, txn, err := e.BeginFileTxn(ctx, path, false)
	if err != nil {
		return nil, err
	}
	defer txn.Release()
	path = txn.Snapshot().Path
	raw := txn.Snapshot().Bytes
	original := string(raw)
	eol := dominantEOL(original)
	ranges := findEOLTolerantRanges(original, anchor)
	if len(ranges) == 0 {
		return nil, fmt.Errorf("anchor not found: %q", anchor)
	}
	if len(ranges) > 1 {
		return nil, fmt.Errorf("anchor matches %d times (expected 1). Quote more surrounding context to make it unique", len(ranges))
	}
	matchStart, matchEnd := ranges[0][0], ranges[0][1]
	anchorOrig := original[matchStart:matchEnd]
	textN := adaptInsertedEOL(text, eol)

	var combined string
	if position == "before" {
		insert := textN
		if trailingEOL(insert) == "" {
			insert += eol
		}
		if matchStart > 0 && original[matchStart-1] != '\n' {
			insert = eol + insert
		}
		combined = insert + anchorOrig
	} else {
		combined = anchorOrig + textN
		if trailingEOL(anchorOrig) == "" {
			combined = anchorOrig + eol + textN
		}
		if matchEnd < len(original) && trailingEOL(combined) == "" && original[matchEnd] != '\n' && original[matchEnd] != '\r' {
			combined += eol
		}
	}

	newContent := original[:matchStart] + combined + original[matchEnd:]

	result := &EditResult{
		ModifiedContent:  newContent,
		ReplacementCount: 1,
		MatchConfidence:  "high",
		LinesAffected:    strings.Count(textN, "\n") + 1,
		StartLine:        lineNumberAtOrig(original, matchStart),
	}

	if dryRun {
		result.NewHash = contentHashFNV(newContent)
		result.TotalLines = CountLines(newContent)
		return result, nil
	}

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

	backupID, err := txn.Commit(ctx, []byte(finalContent), false, "edit_file",
		fmt.Sprintf("Insert %s anchor: %d lines", position, strings.Count(textN, "\n")+1))
	if err != nil {
		return nil, err
	}
	result.BackupID = backupID

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
