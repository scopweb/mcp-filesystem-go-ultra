package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// registerBatchTools registers multi_edit, batch_operations, backup
func registerBatchTools(reg *toolRegistry) {
	engine := reg.engine

	// ============================================================================
	// 12. multi_edit — Multi-edit (unchanged)
	// ============================================================================
	multiEditTool := mcp.NewTool("multi_edit",
		mcp.WithTitleAnnotation("Multi Edit"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("multi_edit — Apply multiple edits to a single file on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use multi_edit for ALL multi-edit operations — never use the runtime's built-in edit tools for host paths. "+
			"Atomically applies all replacements. Auto-backup with undo. "+
			"Related: edit_file (single edit), read_file, search_files, batch_operations."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("edits_json", mcp.Required(), mcp.Description("JSON array of edits: [{\"old_text\": \"...\", \"new_text\": \"...\"}, ...]. Also accepts old_str/new_str as aliases.")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if CRITICAL risk (default: false)")),
		mcp.WithBoolean("tolerant_whitespace", mcp.Description("Apply tolerant_whitespace semantics to all edits in the batch (1 tab = 4 spaces, CRLF = LF). Default: false.")),
	)
	reg.addTool(multiEditTool, auditWrap(engine, "multi_edit", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Accept edits_json as string (normal) or as raw JSON array (Claude Desktop sometimes sends this)
		var edits []core.MultiEditOperation
		args, _ := request.Params.Arguments.(map[string]interface{})

		editsJSON, strErr := request.RequireString("edits_json")
		if strErr == nil {
			// Bug #26: Claude Desktop sends literal newlines inside JSON string values.
			// json.Unmarshal rejects raw \n in strings — fix by escaping them.
			sanitized := editsJSON
			{
				var buf strings.Builder
				inString := false
				escaped := false
				for i := 0; i < len(sanitized); i++ {
					ch := sanitized[i]
					if escaped {
						buf.WriteByte(ch)
						escaped = false
						continue
					}
					if ch == '\\' && inString {
						buf.WriteByte(ch)
						escaped = true
						continue
					}
					if ch == '"' {
						inString = !inString
					}
					if ch == '\n' && inString {
						buf.WriteString("\\n")
						continue
					}
					if ch == '\r' && inString {
						buf.WriteString("\\r")
						continue
					}
					if ch == '\t' && inString {
						buf.WriteString("\\t")
						continue
					}
					buf.WriteByte(ch)
				}
				sanitized = buf.String()
			}
			if err := json.Unmarshal([]byte(sanitized), &edits); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid edits JSON: %v", err)), nil
			}
		} else if args != nil {
			// Defense-in-depth: normalizer should convert raw arrays to JSON string,
			// but keep fallback for edge cases
			if rawEdits, ok := args["edits_json"]; ok {
				rawBytes, err := json.Marshal(rawEdits)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Invalid edits_json: %v", err)), nil
				}
				if err := json.Unmarshal(rawBytes, &edits); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Invalid edits JSON: %v", err)), nil
				}
			} else {
				return mcp.NewToolResultError("edits_json is required"), nil
			}
		} else {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid edits_json: %v", strErr)), nil
		}

		if len(edits) == 0 {
			return mcp.NewToolResultError("edits array cannot be empty"), nil
		}

		// Extract force and dry_run parameters
		force := false
		dryRun := false
		if args != nil {
			if f, ok := args["force"].(bool); ok {
				force = f
			}
			if dr, ok := args["dry_run"].(bool); ok {
				dryRun = dr
			}
		}

		// Execute multi-edit
		result, err := engine.MultiEdit(ctx, path, edits, force, dryRun, false)
		if err != nil {
			// Bug #27: If result is non-nil, this is an atomic rollback — include backup_id and details
			if result != nil && result.BackupID != "" {
				errMsg := fmt.Sprintf("Multi-edit ROLLED BACK (file unchanged): %v\n", err)
				errMsg += fmt.Sprintf("Applied: %d, Failed: %d, Skipped: %d of %d total\n",
					result.SuccessfulEdits, result.FailedEdits, result.SkippedEdits, result.TotalEdits)
				for _, detail := range result.EditDetails {
					switch detail.Status {
					case core.EditStatusApplied:
						errMsg += fmt.Sprintf("  edit %d: would apply (rolled back)\n", detail.Index+1)
					case core.EditStatusFailed:
						errMsg += fmt.Sprintf("  edit %d: FAILED — %s\n", detail.Index+1, detail.Error)
					case core.EditStatusAlreadyPresent:
						errMsg += fmt.Sprintf("  edit %d: already present\n", detail.Index+1)
					}
				}
				errMsg += fmt.Sprintf("Backup: %s (original file is safe)\n", result.BackupID)
				errMsg += "Fix the failing old_text and retry. Use read_file to get exact text."
				return mcp.NewToolResultError(errMsg), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("Multi-edit error: %v", err)), nil
		}

		// Bug #32: dry_run response format for multi_edit
		if dryRun {
			if engine.IsCompactMode() {
				msg := fmt.Sprintf("DRY RUN: %d edits would be applied, %d lines affected",
					result.SuccessfulEdits, result.LinesAffected)
				if result.RiskWarning != "" {
					msg += result.RiskWarning
				}
				msg += "\nNo changes were written to disk"
				return mcp.NewToolResultText(msg), nil
			}
			msg := fmt.Sprintf("DRY RUN — No changes made\nFile: %s\nWould apply: %d edits\nLines affected: %d",
				path, result.SuccessfulEdits, result.LinesAffected)
			if result.RiskWarning != "" {
				msg += result.RiskWarning
			}
			return mcp.NewToolResultText(msg), nil
		}

		// Update backup chain for undo step-through
		if result.BackupID != "" {
			engine.SetCurrentBackupID(path, result.BackupID)
		}

		// Format result (Bug #17: added SkippedEdits and EditDetails)
		if engine.IsCompactMode() {
			msg := ""
			applied := result.SuccessfulEdits
			skipped := result.SkippedEdits
			failed := result.FailedEdits
			total := result.TotalEdits

			if failed > 0 {
				msg = fmt.Sprintf("M %s | %d/%d@+%d-%d | %dL | ERRORS:%d", path, applied+skipped, total, result.LinesAdded, result.LinesRemoved, result.TotalLines, failed)
			} else if skipped > 0 {
				msg = fmt.Sprintf("M %s | %d/%d@+%d-%d | %dL | skip:%d", path, applied, total, result.LinesAdded, result.LinesRemoved, result.TotalLines, skipped)
			} else {
				msg = fmt.Sprintf("M %s | %d@+%d-%d | %dL", path, applied, result.LinesAdded, result.LinesRemoved, result.TotalLines)
			}
			if result.BackupID != "" {
				// Truncate to timestamp only (12 chars) for display
				shortID := result.BackupID
				if len(shortID) > 12 {
					shortID = shortID[:12]
				}
				msg += fmt.Sprintf(" | UNDO:%s", shortID)

				// Show parent chain if exists
				if info, err := engine.GetBackupManager().GetBackupInfo(result.BackupID); err == nil && info.PreviousBackupID != "" {
					parentShort := info.PreviousBackupID
					if len(parentShort) > 12 {
						parentShort = parentShort[:12]
					}
					msg += fmt.Sprintf(" | chain:%s", parentShort)
				}
			}
			if result.RiskWarning != "" {
				msg += " | " + strings.TrimPrefix(result.RiskWarning, "⚠️ ")
			}
			return mcp.NewToolResultText(msg), nil
		}

		// Verbose format: single line summary + optional sections
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("M %s | %d/%d edits | +%d -%d | %dL\n", path, result.SuccessfulEdits, result.TotalEdits, result.LinesAdded, result.LinesRemoved, result.TotalLines))
		if result.BackupID != "" {
			sb.WriteString(fmt.Sprintf("✓ UNDO:%s", result.BackupID))
			// Show parent chain if exists
			if info, err := engine.GetBackupManager().GetBackupInfo(result.BackupID); err == nil && info.PreviousBackupID != "" {
				sb.WriteString(fmt.Sprintf(" ← chain:%s", info.PreviousBackupID))
			}
			sb.WriteString("\n")
		}
		if result.SkippedEdits > 0 {
			sb.WriteString(fmt.Sprintf("  skip: %d already present\n", result.SkippedEdits))
		}
		if result.FailedEdits > 0 {
			sb.WriteString(fmt.Sprintf("  fail: %d\n", result.FailedEdits))
		}

		if len(result.EditDetails) > 0 {
			sb.WriteString("\nEdit details:\n")
			for _, detail := range result.EditDetails {
				switch detail.Status {
				case core.EditStatusApplied:
					sb.WriteString(fmt.Sprintf("  edit %d: applied (confidence: %s)\n", detail.Index+1, detail.MatchConfidence))
				case core.EditStatusAlreadyPresent:
					sb.WriteString(fmt.Sprintf("  edit %d: already present (subsumed by prior edit)\n", detail.Index+1))
				case core.EditStatusAmbiguous:
					sb.WriteString(fmt.Sprintf("  edit %d: AMBIGUOUS — old_text not in original but new_text present\n", detail.Index+1))
				case core.EditStatusFailed:
					sb.WriteString(fmt.Sprintf("  edit %d: FAILED - %s\n", detail.Index+1, detail.Error))
				}
			}
		}

		if len(result.Errors) > 0 {
			sb.WriteString("\nErrors:\n")
			for _, errMsg := range result.Errors {
				sb.WriteString(fmt.Sprintf("  - %s\n", errMsg))
			}
		}

		if result.RiskWarning != "" {
			sb.WriteString(result.RiskWarning)
		}

		// Append file integrity verification result for HIGH/CRITICAL operations
		if result.Integrity != nil {
			inv := result.Integrity
			if inv.Verification == "OK" {
				sb.WriteString(fmt.Sprintf("✓ integrity:%s|%dL|%dB\n", inv.Hash[:8], inv.Lines, inv.SizeBytes))
			} else if inv.Verification == "WARNING" {
				sb.WriteString(fmt.Sprintf("⚠️ integrity:WARNING | %s\n", inv.Warning))
			} else {
				sb.WriteString(fmt.Sprintf("✗ integrity:ERROR | %s\n", inv.Warning))
			}
		}

		// Annotate audit log with backup chain and integrity info
		if result.BackupID != "" {
			prevID := ""
			if info, err := engine.GetBackupManager().GetBackupInfo(result.BackupID); err == nil {
				prevID = info.PreviousBackupID
			}
			core.SetBackupID(ctx, result.BackupID, prevID)
		}
		if result.Integrity != nil {
			core.SetIntegrityStatus(ctx, result.Integrity.Verification, result.Integrity.Warning)
		}

		return mcp.NewToolResultText(sb.String()), nil
	}))

	// ============================================================================
	// 13. batch_operations — Batch operations (enhanced: + pipeline_json + rename_json)
	// ============================================================================
	batchOpsTool := mcp.NewTool("batch_operations",
		mcp.WithTitleAnnotation("Batch Operations"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("batch_operations — Execute atomic file operations (write, edit, copy, move, delete, create_dir) on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use batch_operations for ALL batch/atomic operations on the host disk — never use the runtime's built-in tools for host paths. "+
			"Supports pipelines, rename, dry_run, rollback on error. Params: request_json, pipeline_json, or rename_json. "+
			"Related: edit_file (single edit), multi_edit (multi-edit one file), search_files, backup."),
		mcp.WithString("request_json", mcp.Description("JSON with operations array and options. Fields: operations (array), atomic (bool), create_backup (bool), validate_only (bool)")),
		mcp.WithString("pipeline_json", mcp.Description("JSON-encoded pipeline definition with name, steps, and optional flags (dry_run, force, stop_on_error, create_backup, verbose, parallel)")),
		mcp.WithString("rename_json", mcp.Description("JSON with batch rename parameters. Fields: path, mode, find, replace, prefix, suffix, pattern, extension, start_number, padding, recursive, file_pattern, preview, case_sensitive")),
	)
	reg.addTool(batchOpsTool, auditWrap(engine, "batch_operations", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pipelineJSON := ""
		renameJSON := ""
		requestJSON := ""

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if pj, ok := args["pipeline_json"].(string); ok {
				pipelineJSON = pj
			}
			if rj, ok := args["rename_json"].(string); ok {
				renameJSON = rj
			}
			if rq, ok := args["request_json"].(string); ok {
				requestJSON = rq
			}
		}

		// If pipeline_json is provided, dispatch to pipeline executor
		if pipelineJSON != "" {
			var pipelineReq core.PipelineRequest
			if err := json.Unmarshal([]byte(pipelineJSON), &pipelineReq); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid pipeline JSON: %v", err)), nil
			}

			executor := core.NewPipelineExecutor(engine)
			result, err := executor.Execute(ctx, pipelineReq)
			if err != nil && result == nil {
				return mcp.NewToolResultError(fmt.Sprintf("Pipeline execution failed: %v", err)), nil
			}

			responseText := formatPipelineResult(result, engine.IsCompactMode())

			if !result.Success {
				return mcp.NewToolResultError(responseText), nil
			}

			return mcp.NewToolResultText(responseText), nil
		}

		// If rename_json is provided, dispatch to batch rename
		if renameJSON != "" {
			var batchRenameReq core.BatchRenameRequest
			if err := json.Unmarshal([]byte(renameJSON), &batchRenameReq); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid rename JSON: %v", err)), nil
			}

			result, err := engine.BatchRenameFiles(ctx, batchRenameReq)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Batch rename error: %v", err)), nil
			}

			resultText := core.FormatBatchRenameResult(result, engine.IsCompactMode())
			if !result.Success && !result.Preview {
				return mcp.NewToolResultError(resultText), nil
			}
			return mcp.NewToolResultText(resultText), nil
		}

		// Default: existing batch operations via request_json
		if requestJSON == "" {
			return mcp.NewToolResultError("One of request_json, pipeline_json, or rename_json is required"), nil
		}

		var batchReq core.BatchRequest
		if err := json.Unmarshal([]byte(requestJSON), &batchReq); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid request JSON: %v", err)), nil
		}

		if batchReq.Operations == nil || len(batchReq.Operations) == 0 {
			return mcp.NewToolResultError("operations array is required and cannot be empty"), nil
		}

		batchManager := core.NewBatchOperationManager("", 10)
		batchManager.SetBackupManager(engine.GetBackupManager())
		batchManager.SetEngine(engine)
		result := batchManager.ExecuteBatch(batchReq)

		resultText := formatBatchResult(result)

		if !result.Success {
			return mcp.NewToolResultError(resultText), nil
		}

		return mcp.NewToolResultText(resultText), nil
	}))

	// ============================================================================
	// 14. project_replace — Project-wide find/replace in one call
	// ============================================================================
	projectReplaceTool := mcp.NewTool("project_replace",
		mcp.WithTitleAnnotation("Project Replace"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("project_replace — Find and replace across an entire project tree in a single call. "+
			"Replaces N calls to multi_edit with 1 call. Creates single consolidated backup. "+
			"Related: multi_edit (single file), edit_file (one edit), search_files (discovery)."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Root directory to scan (WSL or Windows format)")),
		mcp.WithString("find", mcp.Required(), mcp.Description("Text or regex pattern to find")),
		mcp.WithString("replace", mcp.Required(), mcp.Description("Replacement text")),
		mcp.WithBoolean("literal", mcp.Description("If true, find is literal text (default); if false, find is regex")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive matching (default: true)")),
		mcp.WithString("file_types", mcp.Description("Comma-separated file extensions to include (e.g. '.php,.html')")),
		mcp.WithString("include_paths", mcp.Description("JSON array of glob patterns to include")),
		mcp.WithString("exclude_paths", mcp.Description("JSON array of glob patterns to exclude (e.g. 'jotajotape/**')")),
		mcp.WithBoolean("preview", mcp.Description("Preview changes without writing (default: false)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create single consolidated backup (default: true)")),
		mcp.WithBoolean("parallel", mcp.Description("Process files in parallel (default: true)")),
		mcp.WithNumber("max_files", mcp.Description("Maximum files to process (safety cap, default: 1000)")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if HIGH/CRITICAL risk (default: false)")),
	)
	reg.addTool(projectReplaceTool, auditWrap(engine, "project_replace", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		find, err := request.RequireString("find")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid find: %v", err)), nil
		}

		replace := ""
		if r, ok := request.GetArguments()["replace"].(string); ok {
			replace = r
		}

		args := request.GetArguments()
		literal := true
		if l, ok := args["literal"].(bool); ok {
			literal = l
		}
		caseSensitive := true
		if cs, ok := args["case_sensitive"].(bool); ok {
			caseSensitive = cs
		}
		fileTypes := ""
		if ft, ok := args["file_types"].(string); ok {
			fileTypes = ft
		}
		preview := false
		if p, ok := args["preview"].(bool); ok {
			preview = p
		}
		createBackup := true
		if cb, ok := args["create_backup"].(bool); ok {
			createBackup = cb
		}
		parallel := true
		if par, ok := args["parallel"].(bool); ok {
			parallel = par
		}
		maxFiles := 1000
		if mf, ok := args["max_files"].(float64); ok {
			maxFiles = int(mf)
		}

		// Parse include/exclude paths
		var includePaths, excludePaths []string
		if inc, ok := args["include_paths"].(string); ok && inc != "" {
			json.Unmarshal([]byte(inc), &includePaths)
		}
		if exc, ok := args["exclude_paths"].(string); ok && exc != "" {
			json.Unmarshal([]byte(exc), &excludePaths)
		}

		result, err := engine.ProjectReplace(ctx, path, find, replace, literal, caseSensitive, fileTypes, includePaths, excludePaths, preview, createBackup, parallel, maxFiles)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("project_replace error: %v", err)), nil
		}

		// Format response
		if engine.IsCompactMode() {
			msg := fmt.Sprintf("PR %s | find:'%s' | %d files | %d replacements", path, find, result.FilesChanged, result.TotalReplaced)
			if result.BackupID != "" {
				shortID := result.BackupID
				if len(shortID) > 12 {
					shortID = shortID[:12]
				}
				msg += fmt.Sprintf(" | UNDO:%s", shortID)
			}
			if result.RiskWarning != "" {
				msg += " | " + strings.TrimPrefix(result.RiskWarning, "⚠️ ")
			}
			if result.DryRun {
				msg += " (preview)"
			}
			return mcp.NewToolResultText(msg), nil
		}

		// Verbose format
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("project_replace: '%s' → '%s'\n", find, replace))
		sb.WriteString(fmt.Sprintf("Path: %s\n", path))
		sb.WriteString(fmt.Sprintf("Files changed: %d\n", result.FilesChanged))
		sb.WriteString(fmt.Sprintf("Total replacements: %d\n", result.TotalReplaced))
		if result.BackupID != "" {
			sb.WriteString(fmt.Sprintf("✓ Backup/UNDO: %s\n", result.BackupID))
		}
		if result.RiskLevel != "" && result.RiskLevel != "LOW" {
			sb.WriteString(fmt.Sprintf("⚠️ Risk: %s\n", result.RiskLevel))
		}
		if result.DryRun {
			sb.WriteString("(preview mode - no changes written)\n")
		}
		if len(result.PerFileResults) > 0 && len(result.PerFileResults) <= 20 {
			sb.WriteString("\nPer-file results:\n")
			for _, fr := range result.PerFileResults {
				sb.WriteString(fmt.Sprintf("  %s: %d replacements\n", fr.Path, fr.Replaced))
			}
		} else if len(result.PerFileResults) > 20 {
			sb.WriteString(fmt.Sprintf("\n(Showing 20 of %d files — use verbose=true for full list)\n", len(result.PerFileResults)))
			for _, fr := range result.PerFileResults[:20] {
				sb.WriteString(fmt.Sprintf("  %s: %d replacements\n", fr.Path, fr.Replaced))
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	}))

	// ============================================================================
	// 15. backup — Backup and recovery (enhanced: + restore action)
	// ============================================================================
	backupTool := mcp.NewTool("backup",
		mcp.WithTitleAnnotation("Backup & Restore"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("backup — Manage backups, restore, and undo. Actions: list, info, compare, cleanup, restore, undo_last, undo_chain, list_trash, restore_trash, purge_trash. "+
			"Auto-created before every edit_file/multi_edit. Soft-deleted files (delete_file) are managed via list_trash/restore_trash/purge_trash when --backup-dir is set. "+
			"Related: edit_file, batch_operations, analyze_operation, delete_file."),
		mcp.WithString("action", mcp.Description("Action: list (default), info, compare, cleanup, restore, undo_last, undo_chain, list_trash, restore_trash, purge_trash")),
		mcp.WithString("backup_id", mcp.Description("Backup ID (required for info, compare, restore)")),
		mcp.WithString("sd_id", mcp.Description("Soft-delete ID (required for restore_trash)")),
		mcp.WithString("file_path", mcp.Description("File path for compare or selective restore")),
		mcp.WithNumber("limit", mcp.Description("Max backups to return for list (default: 20)")),
		mcp.WithString("filter_operation", mcp.Description("Filter by operation: edit, delete, batch, all")),
		mcp.WithString("filter_path", mcp.Description("Filter by file path (substring match)")),
		mcp.WithNumber("newer_than_hours", mcp.Description("Only backups newer than N hours")),
		mcp.WithNumber("older_than_days", mcp.Description("For cleanup/purge_trash: delete entries older than N days (default: 7)")),
		mcp.WithBoolean("dry_run", mcp.Description("For cleanup/restore/purge_trash: preview without executing (default: true for cleanup/purge_trash, false for restore)")),
		mcp.WithBoolean("preview", mcp.Description("For restore: show diff without restoring (default: false)")),
	)
	reg.addTool(backupTool, auditWrap(engine, "backup", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if engine.GetBackupManager() == nil {
			return mcp.NewToolResultError("Backup system not available"), nil
		}

		action := "list"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if a, ok := args["action"].(string); ok && a != "" {
				action = a
			}
		}

		switch action {
		case "list":
			limit := 20
			filterOp := "all"
			filterPath := ""
			newerThan := 0

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if l, ok := args["limit"].(float64); ok {
					limit = int(l)
				}
				if f, ok := args["filter_operation"].(string); ok {
					filterOp = f
				}
				if f, ok := args["filter_path"].(string); ok {
					filterPath = f
				}
				if n, ok := args["newer_than_hours"].(float64); ok {
					newerThan = int(n)
				}
			}

			backups, err := engine.GetBackupManager().ListBackups(limit, filterOp, filterPath, newerThan)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to list backups: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("Available Backups (%d)\n", len(backups)))
			output.WriteString("---\n\n")

			for _, backup := range backups {
				output.WriteString(fmt.Sprintf("# %s\n", backup.BackupID))
				output.WriteString(fmt.Sprintf("   Time: %s (%s)\n", backup.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(backup.Timestamp)))
				output.WriteString(fmt.Sprintf("   Operation: %s\n", backup.Operation))
				output.WriteString(fmt.Sprintf("   Files: %d (%s)\n", len(backup.Files), core.FormatSize(backup.TotalSize)))
				if backup.UserContext != "" {
					output.WriteString(fmt.Sprintf("   Context: %s\n", backup.UserContext))
				}
				output.WriteString("\n")
			}

			if len(backups) == 0 {
				output.WriteString("No backups found matching the criteria.\n")
			} else {
				output.WriteString("Use backup(action:\"restore\", backup_id:\"...\") to restore files\n")
				output.WriteString("Use backup(action:\"info\", backup_id:\"...\") for detailed information\n")
			}

			return mcp.NewToolResultText(output.String()), nil

		case "info":
			backupID, err := request.RequireString("backup_id")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("backup_id is required: %v", err)), nil
			}

			info, err := engine.GetBackupManager().GetBackupInfo(backupID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to get backup info: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("Backup Details: %s\n", info.BackupID))
			output.WriteString("---\n\n")
			output.WriteString(fmt.Sprintf("Timestamp: %s (%s)\n", info.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(info.Timestamp)))
			output.WriteString(fmt.Sprintf("Operation: %s\n", info.Operation))
			if info.UserContext != "" {
				output.WriteString(fmt.Sprintf("Context: %s\n", info.UserContext))
			}
			output.WriteString(fmt.Sprintf("Total Size: %s\n", core.FormatSize(info.TotalSize)))
			output.WriteString(fmt.Sprintf("Files: %d\n\n", len(info.Files)))

			output.WriteString("Files in backup:\n")
			for i, file := range info.Files {
				if i >= 10 {
					output.WriteString(fmt.Sprintf("   ... and %d more files\n", len(info.Files)-10))
					break
				}
				output.WriteString(fmt.Sprintf("   - %s (%s)\n", file.OriginalPath, core.FormatSize(file.Size)))
			}

			output.WriteString(fmt.Sprintf("\nBackup Location: %s\n", engine.GetBackupManager().GetBackupPath(backupID)))

			return mcp.NewToolResultText(output.String()), nil

		case "compare":
			backupID, err := request.RequireString("backup_id")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("backup_id is required: %v", err)), nil
			}

			filePath := ""
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if f, ok := args["file_path"].(string); ok {
					filePath = f
				}
			}
			if filePath == "" {
				return mcp.NewToolResultError("file_path is required for compare action"), nil
			}

			diff, err := engine.GetBackupManager().CompareWithBackup(backupID, filePath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Comparison failed: %v", err)), nil
			}

			return mcp.NewToolResultText(diff), nil

		case "cleanup":
			olderThanDays := 7
			dryRun := true

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if d, ok := args["older_than_days"].(float64); ok {
					olderThanDays = int(d)
				}
				if dr, ok := args["dry_run"].(bool); ok {
					dryRun = dr
				}
			}

			deletedCount, freedSpace, err := engine.GetBackupManager().CleanupOldBackups(olderThanDays, dryRun)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Cleanup failed: %v", err)), nil
			}

			var output strings.Builder
			if dryRun {
				output.WriteString("Dry Run - Preview of cleanup\n\n")
				output.WriteString(fmt.Sprintf("Would delete: %d backup(s)\n", deletedCount))
				output.WriteString(fmt.Sprintf("Would free: %s\n\n", core.FormatSize(freedSpace)))
				output.WriteString("Run with dry_run: false to actually delete backups\n")
			} else {
				output.WriteString("Cleanup completed\n\n")
				output.WriteString(fmt.Sprintf("Deleted: %d backup(s)\n", deletedCount))
				output.WriteString(fmt.Sprintf("Freed: %s\n", core.FormatSize(freedSpace)))
			}

			return mcp.NewToolResultText(output.String()), nil

		case "restore":
			backupID, err := request.RequireString("backup_id")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("backup_id is required: %v", err)), nil
			}

			filePath := ""
			preview := false
			dryRun := false
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if f, ok := args["file_path"].(string); ok {
					filePath = f
				}
				if p, ok := args["preview"].(bool); ok {
					preview = p
				}
				if dr, ok := args["dry_run"].(bool); ok {
					dryRun = dr
				}
			}

			if preview {
				if filePath == "" {
					return mcp.NewToolResultError("preview mode requires file_path parameter"), nil
				}

				diff, err := engine.GetBackupManager().CompareWithBackup(backupID, filePath)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Failed to compare: %v", err)), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Preview - Changes to be restored:\n\n%s", diff)), nil
			}

			// Dry run for full restore preview
			if dryRun && filePath == "" {
				info, err := engine.GetBackupManager().GetBackupInfo(backupID)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Failed to get backup info: %v", err)), nil
				}

				var output strings.Builder
				output.WriteString(fmt.Sprintf("DRY RUN — Restore preview for backup: %s\n", backupID))
				output.WriteString(fmt.Sprintf("Operation: %s\n", info.Operation))
				output.WriteString(fmt.Sprintf("Files to restore: %d\n\n", len(info.Files)))

				for _, file := range info.Files {
					output.WriteString(fmt.Sprintf("   - %s (size: %d bytes, hash: %s)\n",
						file.OriginalPath, file.Size, file.Hash[:8]))
				}
				output.WriteString("\nRun without dry_run:true to execute restore")
				return mcp.NewToolResultText(output.String()), nil
			}

			// Actual restore
			restoredFiles, preRestoreID, err := engine.GetBackupManager().RestoreBackup(backupID, filePath, true)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to restore: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString("Restore completed successfully\n\n")
			output.WriteString(fmt.Sprintf("Restored from backup: %s\n", backupID))
			output.WriteString(fmt.Sprintf("Restored %d file(s):\n", len(restoredFiles)))
			for _, file := range restoredFiles {
				output.WriteString(fmt.Sprintf("   - %s\n", file))
			}
			if preRestoreID != "" {
				output.WriteString(fmt.Sprintf("\nSafety backup (state before restore): %s\n", preRestoreID))
				output.WriteString(fmt.Sprintf("UNDO this restore: backup(action:\"restore\", backup_id:\"%s\")\n", preRestoreID))
			}

			return mcp.NewToolResultText(output.String()), nil

		case "undo_last":
			// Check for dry_run / preview first
			isDryRun := false
			isPreview := false
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if dr, ok := args["dry_run"].(bool); ok && dr {
					isDryRun = true
				}
				if p, ok := args["preview"].(bool); ok && p {
					isPreview = true
				}
			}

			// Get file_path if specified
			var targetFile string
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if fp, ok := args["file_path"].(string); ok && fp != "" {
					targetFile = fp
				}
			}

			// Try step-through undo via backup chain first
			if targetFile != "" {
				currentBackupID := engine.GetCurrentBackupID(targetFile)
				if currentBackupID != "" {
					restoredFiles, prevID, hasMore, err := engine.GetBackupManager().RestorePreviousInChain(currentBackupID, targetFile)
					if err == nil {
						// Preview/dry-run: show what would happen without executing
						if isDryRun || isPreview {
							var output strings.Builder
							output.WriteString(fmt.Sprintf("Preview — Would undo step for: %s\n\n", targetFile))
							output.WriteString(fmt.Sprintf("Backup being reverted: %s\n", currentBackupID))
							output.WriteString(fmt.Sprintf("Files would be restored: %d\n", len(restoredFiles)))
							for _, file := range restoredFiles {
								output.WriteString(fmt.Sprintf("   - %s\n", file))
							}
							if hasMore {
								output.WriteString(fmt.Sprintf("\nPrevious backup in chain: %s\n", prevID))
							} else {
								output.WriteString("\nNo more undo available after this step\n")
							}
							output.WriteString("\nRun without preview/dry_run to execute undo\n")
							return mcp.NewToolResultText(output.String()), nil
						}

						// Update chain
						if prevID != "" {
							engine.SetCurrentBackupID(targetFile, prevID)
						} else {
							engine.ClearBackupID(targetFile)
						}

						var output strings.Builder
						output.WriteString(fmt.Sprintf("UNDO step — restored from backup %s\n\n", currentBackupID))
						output.WriteString(fmt.Sprintf("File: %s\n", targetFile))
						output.WriteString(fmt.Sprintf("Restored %d file(s):\n", len(restoredFiles)))
						for _, file := range restoredFiles {
							output.WriteString(fmt.Sprintf("   - %s\n", file))
						}
						if hasMore {
							output.WriteString(fmt.Sprintf("\nMore undo available. Previous backup: %s\n", prevID))
							output.WriteString("Run backup(action:\"undo_last\", file_path:\"...\", preview:true) to preview next step\n")
						} else {
							output.WriteString("\nNo more undo available — reached earliest backup in chain\n")
						}
						return mcp.NewToolResultText(output.String()), nil
					}
					// Fall through to old behavior if error
				}
			}

			// Fallback: find the most recent backup (old behavior)
			backups, err := engine.GetBackupManager().ListBackups(1, "all", "", 0)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to list backups: %v", err)), nil
			}
			if len(backups) == 0 {
				return mcp.NewToolResultError("No backups found. Nothing to undo."), nil
			}

			lastBackup := backups[0]

			if isDryRun || isPreview {
				var output strings.Builder
				output.WriteString(fmt.Sprintf("Preview — Last backup: %s\n", lastBackup.BackupID))
				output.WriteString(fmt.Sprintf("Time: %s (%s)\n", lastBackup.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(lastBackup.Timestamp)))
				output.WriteString(fmt.Sprintf("Operation: %s\n", lastBackup.Operation))
				output.WriteString(fmt.Sprintf("Files: %d\n\n", len(lastBackup.Files)))
				for _, file := range lastBackup.Files {
					output.WriteString(fmt.Sprintf("   - %s\n", file.OriginalPath))
				}
				output.WriteString("\nRun without preview/dry_run to restore these files\n")
				return mcp.NewToolResultText(output.String()), nil
			}

			// Restore the last backup
			restoredFiles, preRestoreID, err := engine.GetBackupManager().RestoreBackup(lastBackup.BackupID, "", true)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to restore: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("UNDO completed — restored backup %s\n\n", lastBackup.BackupID))
			output.WriteString(fmt.Sprintf("Operation undone: %s\n", lastBackup.Operation))
			output.WriteString(fmt.Sprintf("Time of original edit: %s (%s)\n\n", lastBackup.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(lastBackup.Timestamp)))
			output.WriteString(fmt.Sprintf("Restored %d file(s):\n", len(restoredFiles)))
			for _, file := range restoredFiles {
				output.WriteString(fmt.Sprintf("   - %s\n", file))
			}
			if preRestoreID != "" {
				output.WriteString(fmt.Sprintf("\nSafety backup (state before undo): %s\n", preRestoreID))
				output.WriteString(fmt.Sprintf("REDO (re-apply): backup(action:\"restore\", backup_id:\"%s\")\n", preRestoreID))
			}
			output.WriteString("\nA backup of the current state was created before restoring\n")
			return mcp.NewToolResultText(output.String()), nil

		case "undo_chain":
			// Show the undo chain for a file
			var targetFile string
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if fp, ok := args["file_path"].(string); ok && fp != "" {
					targetFile = fp
				}
			}

			if targetFile == "" {
				return mcp.NewToolResultError("file_path is required for undo_chain action"), nil
			}

			currentBackupID := engine.GetCurrentBackupID(targetFile)
			if currentBackupID == "" {
				return mcp.NewToolResultText(fmt.Sprintf("No undo chain found for %s\nNo edits have been tracked for this file in this session.", targetFile)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("Undo chain for: %s\n\n", targetFile))
			output.WriteString("Backups (newest → oldest):\n")

			visited := make(map[string]bool)
			backupID := currentBackupID
			step := 1
			for backupID != "" && !visited[backupID] {
				visited[backupID] = true
				info, err := engine.GetBackupManager().GetBackupInfo(backupID)
				if err != nil {
					break
				}
				arrow := "→ "
				if step == 1 {
					arrow = "● "
				}
				output.WriteString(fmt.Sprintf("  %s%s | %s | %s\n", arrow, backupID,
					info.Timestamp.Format("15:04:05"), info.Operation))
				backupID = info.PreviousBackupID
				step++
				if step > 100 {
					output.WriteString("  ... (possible cycle detected)\n")
					break
				}
			}

			output.WriteString("\nUse backup(action:\"undo_last\", file_path:\"...\") to step backward\n")
			return mcp.NewToolResultText(output.String()), nil

		case "list_trash":
			// Enumerate soft-deleted files in the trash (only works when
			// --backup-dir is configured; otherwise returns empty).
			limit := 50
			filterPath := ""
			olderThanDays := 0
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if l, ok := args["limit"].(float64); ok {
					limit = int(l)
				}
				if fp, ok := args["filter_path"].(string); ok {
					filterPath = fp
				}
				if od, ok := args["older_than_days"].(float64); ok {
					olderThanDays = int(od)
				}
			}

			entries, err := engine.GetBackupManager().ListTrash(limit, filterPath, olderThanDays)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to list trash: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("Trash entries (%d)\n", len(entries)))
			output.WriteString("---\n\n")
			for _, entry := range entries {
				output.WriteString(fmt.Sprintf("# %s\n", entry.SDID))
				output.WriteString(fmt.Sprintf("   Time: %s (%s)\n", entry.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(entry.Timestamp)))
				output.WriteString(fmt.Sprintf("   Original: %s\n", entry.OriginalPath))
				output.WriteString(fmt.Sprintf("   Size: %s\n", core.FormatSize(entry.Size)))
				if entry.Hash != "" {
					output.WriteString(fmt.Sprintf("   Hash: %s\n", entry.Hash[:12]))
				}
				output.WriteString("\n")
			}
			if len(entries) == 0 {
				output.WriteString("No trash entries found.\n")
				if engine.GetBackupManager().GetBackupDir() == "" {
					output.WriteString("(Trash is only populated when --backup-dir is configured.)\n")
				}
			} else {
				output.WriteString("Use backup(action:\"restore_trash\", sd_id:\"...\") to restore\n")
				output.WriteString("Use backup(action:\"purge_trash\", older_than_days:N) to permanently delete old entries\n")
			}
			return mcp.NewToolResultText(output.String()), nil

		case "restore_trash":
			// Restore a soft-deleted file by its SD-ID. Validates the SD-ID
			// against path-traversal inside the BackupManager.
			sdID, err := request.RequireString("sd_id")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("sd_id is required: %v", err)), nil
			}

			restoredPath, err := engine.GetBackupManager().RestoreTrash(sdID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to restore: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString("Trash restore completed successfully\n\n")
			output.WriteString(fmt.Sprintf("Restored from trash: %s\n", sdID))
			output.WriteString(fmt.Sprintf("File: %s\n", restoredPath))
			return mcp.NewToolResultText(output.String()), nil

		case "purge_trash":
			// Permanently delete trash entries older than olderThanDays.
			olderThanDays := 7
			dryRun := true
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if od, ok := args["older_than_days"].(float64); ok {
					olderThanDays = int(od)
				}
				if dr, ok := args["dry_run"].(bool); ok {
					dryRun = dr
				}
			}

			deletedCount, freedSpace, err := engine.GetBackupManager().PurgeTrash(olderThanDays, dryRun)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Purge trash failed: %v", err)), nil
			}

			var output strings.Builder
			if dryRun {
				output.WriteString("Dry Run - Preview of trash purge\n\n")
				output.WriteString(fmt.Sprintf("Would delete: %d trash entry/entries\n", deletedCount))
				output.WriteString(fmt.Sprintf("Would free: %s\n\n", core.FormatSize(freedSpace)))
				output.WriteString("Run with dry_run: false to actually purge trash\n")
			} else {
				output.WriteString("Trash purge completed\n\n")
				output.WriteString(fmt.Sprintf("Deleted: %d trash entry/entries\n", deletedCount))
				output.WriteString(fmt.Sprintf("Freed: %s\n", core.FormatSize(freedSpace)))
			}
			return mcp.NewToolResultText(output.String()), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown action: %s. Valid: list, info, compare, cleanup, restore, undo_last, undo_chain, list_trash, restore_trash, purge_trash", action)), nil
		}
	}))
}
