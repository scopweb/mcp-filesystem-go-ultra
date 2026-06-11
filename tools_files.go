package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// registerFileTools registers create_directory, delete_file, move_file, copy_file, get_file_info
func registerFileTools(reg *toolRegistry) {
	engine := reg.engine

	// ============================================================================
	// 7. create_directory — Create directory
	// ============================================================================
	createDirTool := mcp.NewTool("create_directory",
		mcp.WithTitleAnnotation("Create Directory"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("create_directory — Create directories on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use create_directory for ALL project directory creation — never use the runtime's built-in mkdir tools for host paths. "+
			"Recursive creation supported. Related: list_directory, write_file, delete_file, batch_operations."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the directory to create")),
	)
	reg.addTool(createDirTool, auditWrap(engine, "create_directory", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		err = engine.CreateDirectory(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s created", path)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully created directory: %s", path)), nil
	}))

	// ============================================================================
	// 8. delete_file — Delete file (soft-delete by default, permanent with permanent:true)
	// ============================================================================
	deleteFileTool := mcp.NewTool("delete_file",
		mcp.WithTitleAnnotation("Delete File"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("delete_file — Delete files from the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use delete_file for ALL project file deletions — never use the runtime's built-in delete tools for host paths. "+
			"Default: soft-delete (to trash folder), permanent:true for hard delete. "+
			"Batch: pass paths (JSON array) to delete multiple files in one call. Related: copy_file, move_file, edit_file, backup."),
		mcp.WithString("path", mcp.Description("Path to the file or directory to delete. Required unless paths is provided.")),
		mcp.WithString("paths", mcp.Description("JSON array of paths to delete multiple files in one call, e.g. '[\"a.txt\",\"b.txt\"]'")),
		mcp.WithBoolean("permanent", mcp.Description("Permanently delete instead of soft-delete (default: false)")),
	)
	reg.addTool(deleteFileTool, auditWrap(engine, "delete_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		permanent := false
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if p, ok := args["permanent"].(bool); ok {
				permanent = p
			}
		}

		// Batch mode: delete multiple files in one call
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if pathsJSON, ok := args["paths"].(string); ok && pathsJSON != "" {
				var paths []string
				if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Invalid paths JSON: %v", err)), nil
				}
				if len(paths) == 0 {
					return mcp.NewToolResultError("paths array is empty"), nil
				}
				var results strings.Builder
				successCount := 0
				for _, p := range paths {
					p = core.NormalizePath(p)
					if permanent {
						err := engine.DeleteFile(ctx, p)
						if err != nil {
							results.WriteString(fmt.Sprintf("FAIL: %s — %v\n", p, err))
							continue
						}
						successCount++
						results.WriteString(fmt.Sprintf("OK: %s deleted\n", p))
					} else {
						info, err := engine.SoftDeleteFile(ctx, p)
						if err != nil {
							results.WriteString(fmt.Sprintf("FAIL: %s — %v\n", p, err))
							continue
						}
						successCount++
						// Annotate audit with the SD-ID (last one wins for batch — the
						// tool-level audit entry will only carry the final SD-ID, but
						// the per-line output preserves all of them for the user).
						core.SetSoftDeleteID(ctx, info.SDID)
						results.WriteString(formatSoftDeleteLine(p, info, engine.IsCompactMode()))
						results.WriteString("\n")
					}
				}
				results.WriteString(fmt.Sprintf("\n%d/%d succeeded", successCount, len(paths)))
				return mcp.NewToolResultText(results.String()), nil
			}
		}

		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		if permanent {
			err = engine.DeleteFile(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText(fmt.Sprintf("OK: %s deleted", path)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Successfully deleted: %s", path)), nil
		}

		// Default: soft delete
		info, err := engine.SoftDeleteFile(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		core.SetSoftDeleteID(ctx, info.SDID)

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(formatSoftDeleteCompact(path, info)), nil
		}
		return mcp.NewToolResultText(formatSoftDeleteVerbose(path, info)), nil
	}))

	// ============================================================================
	// 9. move_file — Move/rename file (also replaces rename_file)
	// ============================================================================
	moveFileTool := mcp.NewTool("move_file",
		mcp.WithTitleAnnotation("Move / Rename"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("move_file — Move or rename files on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use move_file for ALL project file moves — never use the runtime's built-in move/rename tools for host paths. "+
			"Related: copy_file, delete_file, edit_file, batch_operations."),
		mcp.WithString("source_path", mcp.Required(), mcp.Description("Current path of the file/directory")),
		mcp.WithString("dest_path", mcp.Required(), mcp.Description("New path for the file/directory")),
	)
	reg.addTool(moveFileTool, auditWrap(engine, "move_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourcePath, err := request.RequireString("source_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid source_path: %v", err)), nil
		}

		destPath, err := request.RequireString("dest_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid dest_path: %v", err)), nil
		}

		err = engine.MoveFile(ctx, sourcePath, destPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: moved to %s", destPath)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully moved '%s' to '%s'", sourcePath, destPath)), nil
	}))

	// ============================================================================
	// 10. copy_file — Copy file
	// ============================================================================
	copyFileTool := mcp.NewTool("copy_file",
		mcp.WithTitleAnnotation("Copy File"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("copy_file — Copy files on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use copy_file for ALL project file copies — never use the runtime's built-in copy tools for host paths. "+
			"Also copies directories recursively. Related: move_file, delete_file, edit_file, batch_operations, backup."),
		mcp.WithString("source_path", mcp.Required(), mcp.Description("Path of the file/directory to copy")),
		mcp.WithString("dest_path", mcp.Required(), mcp.Description("Destination path for the copy")),
	)
	reg.addTool(copyFileTool, auditWrap(engine, "copy_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourcePath, err := request.RequireString("source_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid source_path: %v", err)), nil
		}

		destPath, err := request.RequireString("dest_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid dest_path: %v", err)), nil
		}

		err = engine.CopyFile(ctx, sourcePath, destPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: copied to %s", destPath)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully copied '%s' to '%s'", sourcePath, destPath)), nil
	}))

	// ============================================================================
	// 11. get_file_info — Get file information
	// ============================================================================
	fileInfoTool := mcp.NewTool("get_file_info",
		mcp.WithTitleAnnotation("File Info"),
		mcp.WithDescription("get_file_info — File/directory metadata (size, permissions, dates). "+
			"Batch: pass paths (JSON array) to get info for multiple files in one call. Related: read_file, list_directory, search_files."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Description("Path to the file or directory. Required unless paths is provided.")),
		mcp.WithString("paths", mcp.Description("JSON array of paths for batch file info, e.g. '[\"file1.txt\",\"dir/\"]'")),
	)
	reg.addTool(fileInfoTool, auditWrap(engine, "get_file_info", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Batch mode: get info for multiple files in one call
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if pathsJSON, ok := args["paths"].(string); ok && pathsJSON != "" {
				var paths []string
				if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Invalid paths JSON: %v", err)), nil
				}
				if len(paths) == 0 {
					return mcp.NewToolResultError("paths array is empty"), nil
				}
				var results strings.Builder
				for i, p := range paths {
					p = core.NormalizePath(p)
					if i > 0 {
						results.WriteString("\n")
					}
					info, err := engine.GetFileInfo(ctx, p)
					if err != nil {
						results.WriteString(fmt.Sprintf("=== %s ===\nERROR: %v\n", p, err))
					} else {
						results.WriteString(info)
						if !strings.HasSuffix(info, "\n") {
							results.WriteString("\n")
						}
					}
				}
				return mcp.NewToolResultText(results.String()), nil
			}
		}

		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		info, err := engine.GetFileInfo(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(info), nil
	}))
}

// formatSoftDeleteLine formats a single line of a batch soft-delete result.
// Used inside the `paths` JSON batch handler (one line per file).
func formatSoftDeleteLine(path string, info *core.SoftDeleteInfo, compact bool) string {
	if info.SDID != "" {
		return fmt.Sprintf("OK: %s soft-deleted (SD:%s)", path, info.SDID)
	}
	return fmt.Sprintf("OK: %s soft-deleted (legacy)", path)
}

// formatSoftDeleteCompact returns a single-line response for the single-path
// delete_file tool in compact mode. Includes the SD-ID (or "legacy" marker)
// and a one-line restore command.
func formatSoftDeleteCompact(path string, info *core.SoftDeleteInfo) string {
	if info.SDID != "" {
		return fmt.Sprintf("D %s | SD:%s | restore: backup(action:\"restore_trash\", sd_id:\"%s\")",
			path, info.SDID, info.SDID)
	}
	return fmt.Sprintf("D %s | legacy | manual: move_file(%s → %s) | hint: set --backup-dir",
		path, info.DestPath, path)
}

// formatSoftDeleteVerbose returns a multi-line response for the single-path
// delete_file tool in verbose mode. Includes the SD-ID (or "legacy" marker),
// the dest path, and a restore command. When the entry is recoverable via the
// backup tool, the restore command uses backup(action:"restore_trash"). When
// it's a legacy entry, the response points to a manual move_file.
func formatSoftDeleteVerbose(path string, info *core.SoftDeleteInfo) string {
	if info.SDID != "" {
		return fmt.Sprintf(
			"OK: %s soft-deleted\n"+
				"   SD-ID: %s\n"+
				"   Trash: %s\n"+
				"   Restore: backup(action:\"restore_trash\", sd_id:\"%s\")\n"+
				"   Or manually: move_file(%s → %s)\n",
			path, info.SDID, info.DestPath, info.SDID, info.DestPath, path)
	}
	return fmt.Sprintf(
		"OK: %s soft-deleted (legacy mode — no discoverable trash)\n"+
			"   Trash: %s\n"+
			"   Manual restore: move_file(%s → %s)\n"+
			"   Hint: set --backup-dir to get a discoverable trash + restore_trash action\n",
		path, info.DestPath, info.DestPath, path)
}
