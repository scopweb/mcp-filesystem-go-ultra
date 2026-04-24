package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerAliases registers the 6 compatibility aliases
func registerAliases(reg *toolRegistry) {
	s := reg.server

	s.AddTool(mcp.NewTool("read_text_file",
		mcp.WithTitleAnnotation("Read File (alias)"),
		mcp.WithDescription("Alias for read_file. To modify/edit files use edit_file tool. Related: edit_file, write_file, search_files, multi_edit."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithNumber("max_lines", mcp.Description("Max lines (optional, 0=all)")),
		mcp.WithString("mode", mcp.Description("Mode: all, head, tail")),
		mcp.WithNumber("start_line", mcp.Description("Starting line number (1-indexed)")),
		mcp.WithNumber("end_line", mcp.Description("Ending line number (inclusive)")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" to read file as base64-encoded binary")),
	), reg.readFileHandler)

	s.AddTool(mcp.NewTool("search",
		mcp.WithTitleAnnotation("Search (alias)"),
		mcp.WithDescription("Alias for search_files. To edit found files use edit_file tool. Related: edit_file, read_file, multi_edit, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Base directory or file (WSL or Windows format)")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
		mcp.WithBoolean("include_content", mcp.Description("Include file content search (default: false)")),
		mcp.WithString("file_types", mcp.Description("Comma-separated file extensions (e.g., '.go,.txt')")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive search (default: false)")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false)")),
		mcp.WithBoolean("include_context", mcp.Description("Include context lines (default: false)")),
		mcp.WithNumber("context_lines", mcp.Description("Number of context lines (default: 3)")),
		mcp.WithBoolean("count_only", mcp.Description("Count pattern occurrences without full search (default: false)")),
		mcp.WithString("return_lines", mcp.Description("Return line numbers of count matches (true/false, for count_only mode)")),
	), reg.searchFilesHandler)

	s.AddTool(mcp.NewTool("edit",
		mcp.WithTitleAnnotation("Edit File (alias)"),
		mcp.WithDescription("Alias for edit_file — prefer using edit_file directly. Edit, modify, replace text in files with auto-backup."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced (default mode)")),
		mcp.WithString("new_text", mcp.Description("New text to replace with (default mode)")),
		mcp.WithString("old_str", mcp.Description("Alias for old_text")),
		mcp.WithString("new_str", mcp.Description("Alias for new_text")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if CRITICAL risk (default: false)")),
		mcp.WithString("mode", mcp.Description("Edit mode: \"replace\" (default), \"search_replace\", \"regex\"")),
		mcp.WithNumber("occurrence", mcp.Description("Which occurrence to replace: 1=first, 2=second, -1=last, -2=second-to-last (default: all)")),
		mcp.WithString("pattern", mcp.Description("Regex or literal pattern (for search_replace and regex modes)")),
		mcp.WithString("replacement", mcp.Description("Replacement text (for search_replace mode)")),
		mcp.WithString("patterns_json", mcp.Description("JSON array of patterns for regex mode: [{\"pattern\": \"regex\", \"replacement\": \"$1...\", \"limit\": -1}]")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive matching (default: true, for regex mode)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create backup before transformation (default: true, for regex mode)")),
		mcp.WithBoolean("dry_run", mcp.Description("Validate without applying changes (default: false, for regex mode)")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false, for occurrence mode)")),
	), reg.editFileHandler)

	s.AddTool(mcp.NewTool("write",
		mcp.WithTitleAnnotation("Write File (alias)"),
		mcp.WithDescription("Alias for write_file — create new files or overwrite. For modifying existing files use edit_file instead."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write")),
		mcp.WithString("content", mcp.Description("Text content to write")),
		mcp.WithString("content_base64", mcp.Description("Base64-encoded binary content")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" for base64 content")),
	), reg.writeFileHandler)

	s.AddTool(mcp.NewTool("create_file",
		mcp.WithTitleAnnotation("Create File (alias)"),
		mcp.WithDescription("Alias for write_file — create new files. For modifying/editing existing files use edit_file instead."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path for the new file")),
		mcp.WithString("content", mcp.Description("Text content to write")),
		mcp.WithString("content_base64", mcp.Description("Base64-encoded binary content")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" for base64 content")),
	), reg.writeFileHandler)

	s.AddTool(mcp.NewTool("directory_tree",
		mcp.WithTitleAnnotation("Directory Tree (alias)"),
		mcp.WithDescription("Alias for list_directory. Related: search_files, read_file, edit_file."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to directory")),
	), reg.listDirHandler)
}

// registerSuperTool registers the fs super-tool that dispatches to all 16 tools
func registerSuperTool(reg *toolRegistry) {
	s := reg.server
	engine := reg.engine
	toolHandlers := reg.handlers

	fsTool := mcp.NewTool("fs",
		mcp.WithTitleAnnotation("Filesystem Super-Tool"),
		mcp.WithDescription("[Super-tool] Single entry point for ALL filesystem operations: "+
			"read, write, edit, multi-edit, search, list directory, move, copy, delete, "+
			"rename files, create directory, batch operations, backup/undo/restore, "+
			"analyze, WSL sync, server info. Use when individual tools are not available. "+
			"Set action param to select operation: read_file, write_file, edit_file, "+
			"multi_edit, list_directory, search_files, analyze_operation, create_directory, "+
			"delete_file, move_file, copy_file, get_file_info, batch_operations, backup, "+
			"wsl, server_info. Pass all operation params alongside action. "+
			"Example: fs(action:\"edit_file\", path:\"f.go\", old_text:\"x\", new_text:\"y\")"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		// Routing
		mcp.WithString("action", mcp.Required(), mcp.Description(
			"Operation to perform: read_file, write_file, edit_file, multi_edit, "+
				"list_directory, search_files, analyze_operation, create_directory, "+
				"delete_file, move_file, copy_file, get_file_info, batch_operations, "+
				"backup, wsl, server_info")),
		// Universal
		mcp.WithString("path", mcp.Description("File or directory path")),
		// read_file
		mcp.WithNumber("start_line", mcp.Description("Start line (1-indexed)")),
		mcp.WithNumber("end_line", mcp.Description("End line (inclusive)")),
		mcp.WithNumber("max_lines", mcp.Description("Max lines (0=all)")),
		mcp.WithString("mode", mcp.Description("Read: all/head/tail — Edit: replace/search_replace/regex")),
		mcp.WithString("encoding", mcp.Description("\"base64\" for binary read/write")),
		// write_file
		mcp.WithString("content", mcp.Description("Text content")),
		mcp.WithString("content_base64", mcp.Description("Base64 binary content")),
		// edit_file
		mcp.WithString("old_text", mcp.Description("Text to replace")),
		mcp.WithString("new_text", mcp.Description("Replacement text")),
		mcp.WithString("old_str", mcp.Description("Alias for old_text")),
		mcp.WithString("new_str", mcp.Description("Alias for new_text")),
		mcp.WithNumber("occurrence", mcp.Description("Occurrence: 1=first, -1=last")),
		mcp.WithBoolean("force", mcp.Description("Force past CRITICAL risk")),
		mcp.WithBoolean("whole_word", mcp.Description("Whole word matching")),
		// search_files
		mcp.WithString("pattern", mcp.Description("Search/regex pattern")),
		mcp.WithString("replacement", mcp.Description("Replacement (search_replace)")),
		mcp.WithString("patterns_json", mcp.Description("Regex patterns JSON array")),
		mcp.WithBoolean("include_content", mcp.Description("Search file contents")),
		mcp.WithString("file_types", mcp.Description("Extension filter (.go,.txt)")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive")),
		mcp.WithBoolean("include_context", mcp.Description("Include context lines")),
		mcp.WithNumber("context_lines", mcp.Description("Context line count")),
		mcp.WithBoolean("count_only", mcp.Description("Count matches only")),
		mcp.WithString("return_lines", mcp.Description("Return line numbers")),
		mcp.WithBoolean("create_backup", mcp.Description("Create backup")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview without applying")),
		// multi_edit
		mcp.WithString("edits_json", mcp.Description("Edits array JSON")),
		// move/copy
		mcp.WithString("source_path", mcp.Description("Source for move/copy")),
		mcp.WithString("dest_path", mcp.Description("Destination for move/copy")),
		// delete
		mcp.WithBoolean("permanent", mcp.Description("Hard delete (default: soft)")),
		// analyze
		mcp.WithString("operation", mcp.Description("Analyze op: file/optimize/write/edit/delete")),
		// batch
		mcp.WithString("request_json", mcp.Description("Batch operations JSON")),
		mcp.WithString("pipeline_json", mcp.Description("Pipeline JSON")),
		mcp.WithString("rename_json", mcp.Description("Batch rename JSON")),
		// backup — uses "action" field internally, so we remap from backup_action
		mcp.WithString("backup_action", mcp.Description("Backup: list/info/compare/cleanup/restore/undo_last")),
		mcp.WithString("backup_id", mcp.Description("Backup ID")),
		mcp.WithString("file_path", mcp.Description("File path for backup compare/restore")),
		mcp.WithNumber("limit", mcp.Description("Max backup results")),
		mcp.WithString("filter_operation", mcp.Description("Filter by op type")),
		mcp.WithString("filter_path", mcp.Description("Filter by path")),
		mcp.WithNumber("newer_than_hours", mcp.Description("Backups newer than N hours")),
		mcp.WithNumber("older_than_days", mcp.Description("Cleanup: older than N days")),
		mcp.WithBoolean("preview", mcp.Description("Preview restore")),
		// wsl — uses "action" field internally, remap from wsl_action
		mcp.WithString("wsl_action", mcp.Description("WSL: sync/status/autosync_config/autosync_status")),
		mcp.WithString("wsl_path", mcp.Description("WSL path")),
		mcp.WithString("windows_path", mcp.Description("Windows path")),
		mcp.WithString("direction", mcp.Description("Sync direction")),
		mcp.WithBoolean("create_dirs", mcp.Description("Create dirs on sync")),
		mcp.WithString("filter_pattern", mcp.Description("WSL sync filter")),
		mcp.WithBoolean("enabled", mcp.Description("Enable auto-sync")),
		mcp.WithBoolean("sync_on_write", mcp.Description("Auto-sync on write")),
		mcp.WithBoolean("sync_on_edit", mcp.Description("Auto-sync on edit")),
		mcp.WithBoolean("silent", mcp.Description("Silent auto-sync")),
		// server_info — uses "action" field internally, remap from server_action
		mcp.WithString("server_action", mcp.Description("Info: help/stats/artifact")),
		mcp.WithString("topic", mcp.Description("Help topic")),
		mcp.WithString("sub_action", mcp.Description("Artifact: capture/write/info")),
	)

	s.AddTool(fsTool, auditWrap(engine, "fs", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		actionStr, err := request.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError(
				"action is required. Valid: read_file, write_file, edit_file, multi_edit, " +
					"list_directory, search_files, analyze_operation, create_directory, " +
					"delete_file, move_file, copy_file, get_file_info, batch_operations, " +
					"backup, wsl, server_info"), nil
		}

		// For backup, wsl, server_info: remap sub-action params to "action" key
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			switch actionStr {
			case "backup":
				if ba, ok := args["backup_action"].(string); ok && ba != "" {
					args["action"] = ba
					request.Params.Arguments = args
				}
			case "wsl":
				if wa, ok := args["wsl_action"].(string); ok && wa != "" {
					args["action"] = wa
					request.Params.Arguments = args
				}
			case "server_info":
				if sa, ok := args["server_action"].(string); ok && sa != "" {
					args["action"] = sa
					request.Params.Arguments = args
				}
			}
		}

		handler, ok := toolHandlers[actionStr]
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf(
				"Unknown action: %s. Valid: read_file, write_file, edit_file, multi_edit, "+
					"list_directory, search_files, analyze_operation, create_directory, "+
					"delete_file, move_file, copy_file, get_file_info, batch_operations, "+
					"backup, wsl, server_info", actionStr)), nil
		}

		// Remove "action" from args before dispatching — the target handler's
		// param validator doesn't know about "action" and will reject it.
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if _, hasAction := args["action"]; hasAction {
				// Only strip "action" for tools that don't use it natively.
				// backup/wsl/server_info re-inject it above; strip the fs-level one.
				switch actionStr {
				case "backup", "wsl", "server_info":
					// keep "action" — already remapped from backup_action/wsl_action/server_action
				default:
					cleaned := make(map[string]interface{}, len(args))
					for k, v := range args {
						if k != "action" {
							cleaned[k] = v
						}
					}
					request.Params.Arguments = cleaned
				}
			}
		}

		return handler(ctx, request)
	}))
}

// registerHelpTool registers the standalone help discovery tool
func registerHelpTool(reg *toolRegistry) {
	helpTool := mcp.NewTool("help",
		mcp.WithTitleAnnotation("Server Help"),
		mcp.WithDescription("Returns tool catalog and usage information."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
	reg.server.AddTool(helpTool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(serverInstructions), nil
	})
}
