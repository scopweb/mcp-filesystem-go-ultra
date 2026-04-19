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
		mcp.WithDescription("create_directory — Create directories (recursive). Related: list_directory, write_file, delete_file, batch_operations."),
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
		mcp.WithDescription("delete_file — Delete files or directories. Default: soft-delete (trash), permanent:true for hard delete. "+
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
					var err error
					if permanent {
						err = engine.DeleteFile(ctx, p)
					} else {
						err = engine.SoftDeleteFile(ctx, p)
					}
					if err != nil {
						results.WriteString(fmt.Sprintf("FAIL: %s — %v\n", p, err))
					} else {
						successCount++
						if permanent {
							results.WriteString(fmt.Sprintf("OK: %s deleted\n", p))
						} else {
							results.WriteString(fmt.Sprintf("OK: %s soft-deleted\n", p))
						}
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
		err = engine.SoftDeleteFile(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s soft-deleted", path)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully moved '%s' to filesdelete folder", path)), nil
	}))

	// ============================================================================
	// 9. move_file — Move/rename file (also replaces rename_file)
	// ============================================================================
	moveFileTool := mcp.NewTool("move_file",
		mcp.WithTitleAnnotation("Move / Rename"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("move_file — Move or rename files and directories. Related: copy_file, delete_file, edit_file, batch_operations."),
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
		mcp.WithDescription("copy_file — Copy files and directories. Related: move_file, delete_file, edit_file, batch_operations, backup."),
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
