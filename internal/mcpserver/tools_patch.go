package mcpserver

import (
	"context"
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
			return pathErrorResult(errCodeNotAllowed, "access denied", pathA, nil, "call list_allowed_directories"), nil
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
			return pathErrorResult(errCodeNotAllowed, "access denied", pathB, nil, "call list_allowed_directories"), nil
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
		mcp.WithDescription("apply_patch — Apply a unified diff to one file. dry_run previews. expected_hash for OCC. Fail-closed: no fuzzy match, one file per call. Related: diff_files, edit_file, backup."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("path", mcp.Required(), mcp.Description("Destination file")),
		mcp.WithString("patch", mcp.Required(), mcp.Description("Unified diff")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview without writing (default: false)")),
		mcp.WithString("expected_hash", mcp.Description("OCC token from last read_file")),
		mcp.WithBoolean("allow_rewrite", mcp.Description("Bypass accidental-rewrite guard")),
		mcp.WithBoolean("create_backup", mcp.Description("Backup before write (default: true)")),
	)
	reg.addTool(patchTool, auditWrap(engine, "apply_patch", handleApplyPatch(engine)),
		`apply_patch(path:"file.go", patch:"--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n")`,
		`apply_patch(path:"file.go", patch:"...", dry_run:true)`,
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
		createBackup := true
		if v, ok := args["create_backup"].(bool); ok {
			createBackup = v
		}

		path = core.NormalizePath(path)
		if !engine.IsPathAllowed(path) {
			return pathErrorResult(errCodeNotAllowed, "access denied", path, nil, "call list_allowed_directories"), nil
		}
		parsed, err := core.ParseUnifiedDiff(patch)
		if err != nil {
			return pathErrorResult(errCodePatchFailed, err.Error(), path, nil, "pass a unified diff for a single file"), nil
		}
		if !core.PatchHeaderMatches(parsed.NewFile, path) && !core.PatchHeaderMatches(parsed.OldFile, path) {
			return pathErrorResult(errCodePatchFailed, "patch header does not match path", path, map[string]string{
				"+++": parsed.NewFile, "---": parsed.OldFile,
			}, "+++ header must match path (a/ b/ prefixes ok)"), nil
		}

		ctx = core.WithExpectedHash(ctx, expectedHash)
		allowMissing := strings.Contains(core.PatchHeaderPath(parsed.OldFile), "dev/null")
		ctx, txn, txnErr := engine.BeginFileTxn(ctx, path, allowMissing)
		if txnErr != nil {
			if occ, ok := txnErr.(*core.OCCMismatchError); ok {
				return pathErrorResult(errCodeOCCMismatch, "content hash != expected_hash", path, map[string]string{
					"expected_hash": occ.Expected, "actual_hash": occ.Actual,
				}, "call read_file and retry with actual_hash"), nil
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
			return pathErrorResult(errCodeOCCMismatch, "content hash != expected_hash", path, map[string]string{
				"expected_hash": expectedHash, "actual_hash": actualHash,
			}, "call read_file and retry with actual_hash"), nil
		}
		if expectedHash == "" && !isNew {
			if occSignal := core.CheckAutoOCC(path, actualHash); occSignal.Status != core.FeedbackOK {
				core.SetFeedback(ctx, occSignal)
				if occSignal.BlockOp {
					return pathErrorResult(errCodeOCCMismatch, occSignal.Message, path, map[string]string{
						"actual_hash": actualHash,
					}, "re-read the file with read_file, then retry"), nil
				}
			}
		}

		newContent, err := core.ApplyUnifiedPatch(string(oldRaw), patch)
		if err != nil {
			return pathErrorResult(errCodePatchFailed, err.Error(), path, nil, "re-read the file and regenerate the patch"), nil
		}

		if !isNew {
			if sig := core.CheckEditRewrite(string(oldRaw), newContent, int64(len(oldRaw))); sig.BlockOp && !allowRewrite {
				return pathErrorResult(errCodeRewriteBlocked, sig.Message, path, nil, "use write_file or allow_rewrite:true"), nil
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
