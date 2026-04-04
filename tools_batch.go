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
		mcp.WithDescription("multi_edit — Apply multiple edits to one file atomically. Faster than calling edit_file multiple times. "+
			"Auto-backup with undo. Related: edit_file (single edit), read_file, search_files, batch_operations."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("edits_json", mcp.Required(), mcp.Description("JSON array of edits: [{\"old_text\": \"...\", \"new_text\": \"...\"}, ...]. Also accepts old_str/new_str as aliases.")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if CRITICAL risk (default: false)")),
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
		result, err := engine.MultiEdit(ctx, path, edits, force, dryRun)
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

		// Format result (Bug #17: added SkippedEdits and EditDetails)
		if engine.IsCompactMode() {
			msg := ""
			applied := result.SuccessfulEdits
			skipped := result.SkippedEdits
			failed := result.FailedEdits
			total := result.TotalEdits

			if failed > 0 {
				msg = fmt.Sprintf("OK: %d/%d edits, %d lines", applied+skipped, total, result.LinesAffected)
			} else if skipped > 0 {
				msg = fmt.Sprintf("OK: %d edits (%d applied, %d already present), %d lines",
					total, applied, skipped, result.LinesAffected)
			} else {
				msg = fmt.Sprintf("OK: %d edits, %d lines", applied, result.LinesAffected)
			}
			if result.BackupID != "" {
				msg += fmt.Sprintf(" [backup:%s | UNDO: backup(action:\"restore\", backup_id:\"%s\")]", result.BackupID, result.BackupID)
			}
			if result.RiskWarning != "" {
				msg += result.RiskWarning
			}
			return mcp.NewToolResultText(msg), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Multi-edit completed on %s\n", path))
		sb.WriteString(fmt.Sprintf("Total edits: %d\n", result.TotalEdits))
		sb.WriteString(fmt.Sprintf("Applied: %d\n", result.SuccessfulEdits))
		if result.SkippedEdits > 0 {
			sb.WriteString(fmt.Sprintf("Already present: %d\n", result.SkippedEdits))
		}
		if result.FailedEdits > 0 {
			sb.WriteString(fmt.Sprintf("Failed: %d\n", result.FailedEdits))
		}
		sb.WriteString(fmt.Sprintf("Lines affected: %d\n", result.LinesAffected))
		sb.WriteString(fmt.Sprintf("Confidence: %s\n", result.MatchConfidence))
		if result.BackupID != "" {
			sb.WriteString(fmt.Sprintf("Backup ID: %s\nUNDO: backup(action:\"restore\", backup_id:\"%s\")\n", result.BackupID, result.BackupID))
		}

		if len(result.EditDetails) > 0 {
			sb.WriteString("\nEdit details:\n")
			for _, detail := range result.EditDetails {
				switch detail.Status {
				case core.EditStatusApplied:
					sb.WriteString(fmt.Sprintf("  edit %d: applied (confidence: %s)\n", detail.Index+1, detail.MatchConfidence))
				case core.EditStatusAlreadyPresent:
					sb.WriteString(fmt.Sprintf("  edit %d: already present (subsumed by prior edit)\n", detail.Index+1))
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
		mcp.WithDescription("batch_operations — Bulk file operations, pipelines, and batch rename in one call. Params: request_json, pipeline_json, or rename_json. "+
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
		batchManager.SetEngine(engine)
		result := batchManager.ExecuteBatch(batchReq)

		resultText := formatBatchResult(result)

		if !result.Success {
			return mcp.NewToolResultError(resultText), nil
		}

		return mcp.NewToolResultText(resultText), nil
	}))

	// ============================================================================
	// 14. backup — Backup and recovery (enhanced: + restore action)
	// ============================================================================
	backupTool := mcp.NewTool("backup",
		mcp.WithTitleAnnotation("Backup & Restore"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("backup — Manage backups, restore, and undo. Actions: list, info, compare, cleanup, restore, undo_last. "+
			"Auto-created before every edit_file/multi_edit. Related: edit_file, batch_operations, analyze_operation."),
		mcp.WithString("action", mcp.Description("Action: list (default), info, compare, cleanup, restore, undo_last")),
		mcp.WithString("backup_id", mcp.Description("Backup ID (required for info, compare, restore)")),
		mcp.WithString("file_path", mcp.Description("File path for compare or selective restore")),
		mcp.WithNumber("limit", mcp.Description("Max backups to return for list (default: 20)")),
		mcp.WithString("filter_operation", mcp.Description("Filter by operation: edit, delete, batch, all")),
		mcp.WithString("filter_path", mcp.Description("Filter by file path (substring match)")),
		mcp.WithNumber("newer_than_hours", mcp.Description("Only backups newer than N hours")),
		mcp.WithNumber("older_than_days", mcp.Description("For cleanup: delete backups older than N days (default: 7)")),
		mcp.WithBoolean("dry_run", mcp.Description("For cleanup/restore: preview without executing (default: true for cleanup, false for restore)")),
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
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if f, ok := args["file_path"].(string); ok {
					filePath = f
				}
				if p, ok := args["preview"].(bool); ok {
					preview = p
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

			// Actual restore
			restoredFiles, err := engine.GetBackupManager().RestoreBackup(backupID, filePath, true)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to restore: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString("Restore completed successfully\n\n")
			output.WriteString(fmt.Sprintf("Restored %d file(s):\n", len(restoredFiles)))
			for _, file := range restoredFiles {
				output.WriteString(fmt.Sprintf("   - %s\n", file))
			}
			output.WriteString("\nA backup of the current state was created before restoring\n")

			return mcp.NewToolResultText(output.String()), nil

		case "undo_last":
			// Find the most recent backup
			backups, err := engine.GetBackupManager().ListBackups(1, "all", "", 0)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to list backups: %v", err)), nil
			}
			if len(backups) == 0 {
				return mcp.NewToolResultError("No backups found. Nothing to undo."), nil
			}

			lastBackup := backups[0]

			// Check for preview mode
			preview := false
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if p, ok := args["preview"].(bool); ok {
					preview = p
				}
			}

			if preview {
				var output strings.Builder
				output.WriteString(fmt.Sprintf("Preview — Last backup: %s\n", lastBackup.BackupID))
				output.WriteString(fmt.Sprintf("Time: %s (%s)\n", lastBackup.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(lastBackup.Timestamp)))
				output.WriteString(fmt.Sprintf("Operation: %s\n", lastBackup.Operation))
				output.WriteString(fmt.Sprintf("Files: %d\n\n", len(lastBackup.Files)))
				for _, file := range lastBackup.Files {
					output.WriteString(fmt.Sprintf("   - %s\n", file.OriginalPath))
				}
				output.WriteString("\nRun backup(action:\"undo_last\") without preview to restore these files\n")
				return mcp.NewToolResultText(output.String()), nil
			}

			// Restore the last backup
			restoredFiles, err := engine.GetBackupManager().RestoreBackup(lastBackup.BackupID, "", true)
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
			output.WriteString("\nA backup of the current state was created before restoring\n")
			return mcp.NewToolResultText(output.String()), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown action: %s. Valid: list, info, compare, cleanup, restore, undo_last", action)), nil
		}
	}))
}
