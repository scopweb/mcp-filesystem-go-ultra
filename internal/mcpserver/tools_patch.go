package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func registerPatchTools(reg *toolRegistry) {
	engine := reg.engine

	diffTool := mcp.NewTool("diff_files",
		mcp.WithTitleAnnotation("Diff Files"),
		mcp.WithRawOutputSchema(diffFilesOutputSchema),
		mcp.WithDescription("diff_files — Unified diff between two paths, or a file vs its last backup (against:\"backup\"). Related: apply_patch, backup, edit_file."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path_a", mcp.Description("Left/old path")),
		mcp.WithString("path_b", mcp.Description("Right/new path (required unless against=backup)")),
		mcp.WithString("path", mcp.Description("File to compare when against=backup")),
		mcp.WithString("against", mcp.Description("backup: compare path (or path_a) to last backup")),
	)
	reg.addTool(diffTool, auditWrap(engine, "diff_files", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := request.Params.Arguments.(map[string]interface{})
		against, _ := args["against"].(string)
		pathA, _ := args["path_a"].(string)
		pathB, _ := args["path_b"].(string)
		path, _ := args["path"].(string)
		if pathA == "" {
			pathA = path
		}
		if pathA == "" {
			return mcp.NewToolResultError("path_a or path is required"), nil
		}
		pathA = core.NormalizePath(pathA)
		if !engine.IsPathAllowed(pathA) {
			return notAllowedResult(engine, pathA), nil
		}

		var contentA, contentB []byte
		var err error
		nameB := pathB
		if strings.EqualFold(against, "backup") {
			id := engine.GetCurrentBackupID(pathA)
			if id == "" || engine.GetBackupManager() == nil {
				return mcp.NewToolResultError("no backup in this session for " + pathA), nil
			}
			cmp, err := engine.GetBackupManager().CompareWithBackup(id, pathA)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultStructured(map[string]any{
				"status": statusOK, "path_a": pathA, "against": "backup", "message": cmp,
			}, cmp), nil
		}
		if pathB == "" {
			return mcp.NewToolResultError("path_b is required unless against=backup"), nil
		}
		pathB = core.NormalizePath(pathB)
		if !engine.IsPathAllowed(pathB) {
			return notAllowedResult(engine, pathB), nil
		}
		contentA, err = os.ReadFile(pathA)
		if err != nil {
			return mcp.NewToolResultError(formatToolError(err)), nil
		}
		contentB, err = os.ReadFile(pathB)
		if err != nil {
			return mcp.NewToolResultError(formatToolError(err)), nil
		}
		a, b := string(contentA), string(contentB)
		if a == b {
			msg := fmt.Sprintf("identical: %s %s", pathA, pathB)
			return mcp.NewToolResultStructured(map[string]any{
				"status": "identical", "path_a": pathA, "path_b": pathB, "identical": true,
				"added": 0, "removed": 0, "message": msg,
			}, msg), nil
		}
		added, removed, _ := core.DiffCounts(a, b)
		diff := core.UnifiedDiff(a, b, filepath.Base(nameB))
		msg := fmt.Sprintf("%s\n+%d -%d", strings.TrimSpace(diff), added, removed)
		return mcp.NewToolResultStructured(map[string]any{
			"status": statusOK, "path_a": pathA, "path_b": pathB, "identical": false,
			"added": added, "removed": removed, "message": msg,
		}, msg), nil
	}))

	patchTool := mcp.NewTool("apply_patch",
		mcp.WithTitleAnnotation("Apply Patch"),
		mcp.WithRawOutputSchema(applyPatchOutputSchema),
		mcp.WithDescription("apply_patch — Apply a unified diff. One file: path is the destination. Several files: path is the directory root and the patch is an in-process transaction (all or nothing; not crash-durable). "+
			"dry_run previews. expected_hash for OCC on a single file. Fail-closed: no fuzzy match. Destination EOL wins (CRLF file + LF patch keeps CRLF). "+
			"If PATCH_FAILED: read_file and regenerate the hunk; do not retry the same patch. Related: diff_files, edit_file, backup."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("path", mcp.Required(), mcp.Description("Destination file (single-file patch) or directory root (multi-file patch)")),
		mcp.WithString("patch", mcp.Required(), mcp.Description("Unified diff (one or more files)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview without writing (default: false)")),
		mcp.WithString("expected_hash", mcp.Description("OCC token from last read_file")),
		mcp.WithString("detail", mcp.Description("Set to full to include a bounded diff in an OCC conflict."), mcp.Enum("full")),
		mcp.WithBoolean("include_diff", mcp.Description("Include a bounded diff in an OCC conflict.")),
		mcp.WithBoolean("allow_rewrite", mcp.Description("Bypass accidental-rewrite guard")),
		mcp.WithBoolean("create_backup", mcp.Description("Backup before write (default: true)")),
	)
	reg.addTool(patchTool, auditWrap(engine, "apply_patch", handleApplyPatch(engine)),
		`apply_patch(path:"file.go", patch:"--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n")`,
		`apply_patch(path:"file.go", patch:"...", dry_run:true)`,
		`apply_patch(path:"proj/", patch:"--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-a\n+A\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-b\n+B\n")`,
	)
}

func handleApplyPatch(engine *core.UltraFastEngine) toolHandler {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		patch, err := request.RequireString("patch")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		args, _ := request.Params.Arguments.(map[string]interface{})
		dryRun, _ := args["dry_run"].(bool)
		allowRewrite, _ := args["allow_rewrite"].(bool)
		expectedHash, _ := args["expected_hash"].(string)
		includeOCCDiff, _ := args["include_diff"].(bool)
		if detail, _ := args["detail"].(string); detail == "full" {
			includeOCCDiff = true
		}
		createBackup := true
		if v, ok := args["create_backup"].(bool); ok {
			createBackup = v
		}

		path = core.NormalizePath(path)
		if !engine.IsPathAllowed(path) {
			return notAllowedResult(engine, path), nil
		}
		files, err := core.ParseUnifiedDiffs(patch)
		if err != nil {
			return patchFailedResult(path, err, nil), nil
		}
		if len(files) > 1 {
			return handleMultiFilePatch(ctx, engine, path, files, dryRun, allowRewrite, createBackup, expectedHash)
		}
		parsed := &files[0]
		if !core.PatchHeaderMatches(parsed.NewFile, path) && !core.PatchHeaderMatches(parsed.OldFile, path) {
			return patchFailedResult(path, &core.PatchError{
				Reason: core.PatchReasonMalformed,
				Msg:    "patch header does not match path",
			}, map[string]string{"+++": parsed.NewFile, "---": parsed.OldFile}), nil
		}

		ctx = core.WithExpectedHash(ctx, expectedHash)
		ctx = core.WithOCCConflictDiff(ctx, includeOCCDiff)
		allowMissing := strings.Contains(core.PatchHeaderPath(parsed.OldFile), "dev/null")
		ctx, txn, txnErr := engine.BeginFileTxn(ctx, path, allowMissing)
		if txnErr != nil {
			if occ, ok := txnErr.(*core.OCCMismatchError); ok {
				return occMismatchResult("content hash != expected_hash", path, occ.Expected, occ.Actual, occ.Conflict), nil
			}
			return mcp.NewToolResultError(formatToolError(txnErr)), nil
		}
		defer txn.Release()
		snap := txn.Snapshot()
		path = snap.Path
		oldRaw := snap.Bytes
		isNew := !snap.Exists
		if allowMissing {
			isNew = true
			if !snap.Exists {
				oldRaw = nil
			}
		}

		actualHash := contentHashBytes(oldRaw)
		if expectedHash != "" && actualHash != expectedHash {
			baseline := engine.FindOCCBaseline(path, expectedHash)
			conflict := core.BuildOCCConflict(expectedHash, oldRaw, baseline)
			if includeOCCDiff {
				core.IncludeOCCConflictDiff(&conflict, oldRaw, baseline, filepath.Base(path))
			}
			return occMismatchResult("content hash != expected_hash", path, expectedHash, actualHash, conflict), nil
		}
		if expectedHash == "" && !isNew {
			if occSignal := core.CheckAutoOCC(path, actualHash); occSignal.Status != core.FeedbackOK {
				core.SetFeedback(ctx, occSignal)
				if occSignal.BlockOp {
					return hashRequiredResult(path, actualHash), nil
				}
			}
		}

		newContent, err := core.ApplyUnifiedPatch(string(oldRaw), patch)
		if err != nil {
			return patchFailedResult(path, err, nil), nil
		}

		if !isNew {
			if sig := core.CheckEditRewrite(string(oldRaw), newContent, int64(len(oldRaw))); sig.BlockOp && !allowRewrite {
				return rewriteBlockedResult(path, sig.Message, len(oldRaw), len(newContent)), nil
			}
		}

		added, removed, _ := core.DiffCounts(string(oldRaw), newContent)
		preview := core.RenderDiff(string(oldRaw), newContent, filepath.Base(path), "stat")
		if dryRun {
			msg := fmt.Sprintf("DRY_RUN %s | %s | +%d -%d", path, preview, added, removed)
			sc := markSimulated(map[string]any{
				"path": path, "lines_added": added, "lines_removed": removed, "message": msg,
			}, actualHash, contentHashBytes([]byte(newContent)))
			return mcp.NewToolResultStructured(sc, msg), nil
		}

		var backupID string
		if !createBackup {
			txn.SkipBackup()
		}
		backupID, err = txn.Commit(ctx, []byte(newContent), false, "apply_patch", "")
		if err != nil {
			return mcp.NewToolResultError(formatToolError(err)), nil
		}
		_, _, hash, verified := verifyOnDiskWrite(engine, path)
		if verified {
			core.RecordWriteHash(path, hash)
		}
		msg := fmt.Sprintf("PATCHED %s | +%d -%d", path, added, removed)
		if backupID != "" {
			msg += " | UNDO:" + backupID
		}
		sc := markApplied(map[string]any{
			"path": path, "lines_added": added, "lines_removed": removed, "message": msg,
		})
		if hash != "" {
			sc["content_hash"] = hash
		}
		if backupID != "" {
			sc["backup_id"] = backupID
		}
		return mcp.NewToolResultStructured(sc, msg), nil
	}
}

func handleMultiFilePatch(ctx context.Context, engine *core.UltraFastEngine, base string, files []core.ParsedPatch, dryRun, allowRewrite, createBackup bool, expectedHash string) (*mcp.CallToolResult, error) {
	if expectedHash != "" {
		return validationResult("expected_hash is only valid for a single-file patch", "expected_hash", "omitted for multi-file"), nil
	}
	res, err := engine.ApplyMultiFilePatch(ctx, files, core.MultiFilePatchOpts{
		BaseDir:      base,
		DryRun:       dryRun,
		AllowRewrite: allowRewrite,
		CreateBackup: createBackup,
	})
	if err != nil {
		var occ *core.OCCMismatchError
		if errors.As(err, &occ) {
			return occMismatchResult("content hash != expected_hash", base, occ.Expected, occ.Actual, occ.Conflict), nil
		}
		var blocked *core.RewriteBlockedError
		if errors.As(err, &blocked) {
			return rewriteBlockedResult(blocked.Path, blocked.Message, blocked.OldLen, blocked.NewLen), nil
		}
		var pe *core.PathError
		if errors.As(err, &pe) && pe != nil && strings.Contains(strings.ToLower(pe.Error()), "access denied") {
			return notAllowedResult(engine, pe.Path), nil
		}
		var patchErr *core.PatchError
		if errors.As(err, &patchErr) {
			return patchFailedResult(base, err, nil), nil
		}
		return mcp.NewToolResultError(formatToolError(err)), nil
	}

	filesOut := make([]map[string]any, 0, len(res.Files))
	added, removed := 0, 0
	var b strings.Builder
	if dryRun {
		b.WriteString("DRY_RUN")
	} else {
		b.WriteString("PATCHED")
	}
	fmt.Fprintf(&b, " %d files", len(res.Files))
	for _, f := range res.Files {
		added += f.Added
		removed += f.Removed
		entry := map[string]any{
			"path": f.Path, "lines_added": f.Added, "lines_removed": f.Removed,
		}
		if f.NewHash != "" {
			entry["content_hash"] = f.NewHash
		}
		if f.BackupID != "" {
			entry["backup_id"] = f.BackupID
		}
		if dryRun {
			entry["current_hash"] = f.OldHash
			entry["predicted_hash"] = f.NewHash
		} else if f.NewHash != "" {
			core.RecordWriteHash(f.Path, f.NewHash)
		}
		filesOut = append(filesOut, entry)
		fmt.Fprintf(&b, " | %s +%d -%d", filepath.Base(f.Path), f.Added, f.Removed)
	}
	msg := b.String()
	payload := map[string]any{
		"path": base, "files": filesOut, "lines_added": added, "lines_removed": removed, "message": msg,
	}
	if dryRun {
		return mcp.NewToolResultStructured(markSimulated(payload, "", ""), msg), nil
	}
	return mcp.NewToolResultStructured(markApplied(payload), msg), nil
}
