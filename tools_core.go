package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

// toolHandler is a shorthand for MCP tool handler functions
type toolHandler = func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

// toolRegistry holds shared state for tool registration across files
type toolRegistry struct {
	server         *server.MCPServer
	engine         *core.UltraFastEngine
	handlers       map[string]toolHandler // dispatch map for the fs super-tool
	regexTransform *core.RegexTransformer

	// Named handlers needed by alias registration
	readFileHandler    toolHandler
	writeFileHandler   toolHandler
	editFileHandler    toolHandler
	listDirHandler     toolHandler
	searchFilesHandler toolHandler
}

// addTool registers a tool on the server AND adds its handler to the dispatch map
func (r *toolRegistry) addTool(tool mcp.Tool, handler toolHandler) {
	r.server.AddTool(tool, handler)
	r.handlers[tool.Name] = handler
}

// registerTools registers all 16 consolidated filesystem tools + aliases + super-tool + help
func registerTools(s *server.MCPServer, engine *core.UltraFastEngine) error {
	reg := &toolRegistry{
		server:         s,
		engine:         engine,
		handlers:       make(map[string]toolHandler),
		regexTransform: core.NewRegexTransformer(engine),
	}

	registerCoreTools(reg)
	registerSearchTools(reg)
	registerFileTools(reg)
	registerBatchTools(reg)
	registerPlatformTools(reg)
	registerAliases(reg)
	registerSuperTool(reg)
	registerHelpTool(reg)

	log.Printf("Registered 24 tools (16 core + 6 aliases + help + fs super-tool) for v4.2.1")
	return nil
}

// registerCoreTools registers read_file, write_file, edit_file
func registerCoreTools(reg *toolRegistry) {
	engine := reg.engine

	// ============================================================================
	// 1. read_file — Read file (consolidated: mcp_read + read_file + read_file_range + read_base64 + chunked_read + intelligent_read)
	// ============================================================================
	readFileTool := mcp.NewTool("read_file",
		mcp.WithTitleAnnotation("Read File"),
		mcp.WithDescription("read_file — Read and view file contents. Supports line ranges (start_line/end_line), head/tail mode, base64 for binary. "+
			"Batch: pass paths (JSON array) to read multiple files in one call. "+
			"To MODIFY files use edit_file. Related: edit_file, write_file, search_files, multi_edit, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Description("Path to file (WSL or Windows format). Required unless paths is provided.")),
		mcp.WithString("paths", mcp.Description("JSON array of paths to read multiple files in one call, e.g. '[\"file1.txt\",\"file2.txt\"]'")),
		mcp.WithNumber("max_lines", mcp.Description("Max lines (optional, 0=all)")),
		mcp.WithString("mode", mcp.Description("Mode: all, head, tail")),
		mcp.WithNumber("start_line", mcp.Description("Starting line number (1-indexed) for range read")),
		mcp.WithNumber("end_line", mcp.Description("Ending line number (inclusive) for range read")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" to read file as base64-encoded binary")),
	)
	reg.readFileHandler = auditWrap(engine, "read_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Batch mode: read multiple files in one call
		// Note: If BOTH path AND paths are provided, AND range params are set,
		// we prioritize path+range over paths (batch) to avoid confusion.
		var paths []string
		var usePathRange bool

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if pathsJSON, ok := args["paths"].(string); ok && pathsJSON != "" {
				if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Invalid paths JSON: %v", err)), nil
				}
			}

			// Check if we should use path+range instead of paths (batch)
			// If both path AND paths are provided, AND start_line/end_line are set,
			// use path with range to avoid ambiguous behavior
			if pathStr, ok := args["path"].(string); ok && pathStr != "" {
				if sl, ok := args["start_line"].(float64); ok && sl > 0 {
					if el, ok := args["end_line"].(float64); ok && el > 0 {
						// Both path and paths provided with range — use path with range
						usePathRange = true
					}
				}
			}
		}

		// If paths is set and we should NOT use path+range, process batch
		if len(paths) > 0 && !usePathRange {
			if len(paths) == 0 {
				return mcp.NewToolResultError("paths array is empty"), nil
			}
			var results strings.Builder
			for i, p := range paths {
				p = core.NormalizePath(p)
				content, err := engine.ReadFileContent(ctx, p)
				if i > 0 {
					results.WriteString("\n")
				}
				results.WriteString(fmt.Sprintf("=== %s ===\n", p))
				if err != nil {
					results.WriteString(fmt.Sprintf("ERROR: %v\n", err))
				} else {
					results.WriteString(content)
					if !strings.HasSuffix(content, "\n") {
						results.WriteString("\n")
					}
				}
			}
			return mcp.NewToolResultText(results.String()), nil
		}

		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Get optional parameters
		maxLines := 0
		mode := "all"
		startLine := 0
		endLine := 0
		encoding := ""

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if ml, ok := args["max_lines"].(float64); ok {
				maxLines = int(ml)
			}
			if m, ok := args["mode"].(string); ok && m != "" {
				mode = m
			}
			if sl, ok := args["start_line"].(float64); ok {
				startLine = int(sl)
			}
			if el, ok := args["end_line"].(float64); ok {
				endLine = int(el)
			}
			if enc, ok := args["encoding"].(string); ok {
				encoding = enc
			}
		}

		// Base64 mode: read binary file as base64
		if encoding == "base64" {
			encoded, originalSize, err := engine.ReadBase64(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText(encoded), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("# File: %s (%d bytes)\n# Base64 encoded:\n%s", path, originalSize, encoded)), nil
		}

		// Range read mode: read specific line range
		if startLine > 0 && endLine == 0 {
			endLine = 999999
		}
		if endLine > 0 && startLine == 0 {
			startLine = 1
		}
		if startLine > 0 && endLine > 0 {
			content, err := engine.ReadFileRange(ctx, path, startLine, endLine)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(content), nil
		}

		// Default: read full file
		content, err := engine.ReadFileContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		// Apply truncation if requested
		if maxLines > 0 || mode != "all" {
			content = truncateContent(content, maxLines, mode)
		}

		return mcp.NewToolResultText(content), nil
	})
	reg.addTool(readFileTool, reg.readFileHandler)

	// ============================================================================
	// 2. write_file — Write file (consolidated: mcp_write + write_file + create_file + write_base64 + streaming_write + intelligent_write)
	// ============================================================================
	writeFileTool := mcp.NewTool("write_file",
		mcp.WithTitleAnnotation("Write File"),
		mcp.WithDescription("write_file — Create NEW files or full overwrite. WARNING: To modify/edit/change existing files use edit_file instead (not this tool). "+
			"Supports base64 binary (encoding:\"base64\"). Related: edit_file, multi_edit, copy_file, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write (WSL or Windows format)")),
		mcp.WithString("content", mcp.Description("Text content to write to the file")),
		mcp.WithString("content_base64", mcp.Description("Base64-encoded binary content to write")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" when content is base64-encoded")),
	)
	reg.writeFileHandler = auditWrap(engine, "write_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Check for base64 content
		contentBase64 := ""
		encoding := ""
		content := ""

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if cb, ok := args["content_base64"].(string); ok {
				contentBase64 = cb
			}
			if enc, ok := args["encoding"].(string); ok {
				encoding = enc
			}
			if c, ok := args["content"].(string); ok {
				content = c
			}
		}

		// Base64 write mode
		if contentBase64 != "" || encoding == "base64" {
			b64Content := contentBase64
			if b64Content == "" {
				b64Content = content
			}
			if b64Content == "" {
				return mcp.NewToolResultError("content_base64 or content with encoding:\"base64\" is required"), nil
			}

			// Validate base64 before passing to engine (fast fail)
			if _, decodeErr := base64.StdEncoding.DecodeString(b64Content); decodeErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid base64: %v", decodeErr)), nil
			}

			bytesWritten, err := engine.WriteBase64(ctx, path, b64Content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText(fmt.Sprintf("OK: %s written", formatSize(int64(bytesWritten)))), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes (from base64) to %s", bytesWritten, path)), nil
		}

		// Normal text write
		if content == "" {
			c, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
			}
			content = c
		}

		err = engine.WriteFileContent(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s written", formatSize(int64(len(content))))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)), nil
	})
	reg.addTool(writeFileTool, reg.writeFileHandler)

	// ============================================================================
	// 3. edit_file — Edit file (consolidated: mcp_edit + edit_file + search_and_replace + replace_nth_occurrence + regex_transform_file + smart_edit + intelligent_edit + recovery_edit)
	// ============================================================================
	editFileTool := mcp.NewTool("edit_file",
		mcp.WithTitleAnnotation("Edit File"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("edit_file — Edit, modify, change, replace, or update text in existing files. "+
			"Use edit_file (NOT write_file) whenever you need to change file contents. "+
			"Modes: default (exact match replace), search_replace (regex/literal all occurrences), regex (capture groups). "+
			"Auto-backup on every edit — undo with backup(action:\"undo_last\"). "+
			"Related: multi_edit (multiple edits atomically), read_file, search_files, batch_operations."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced (default mode)")),
		mcp.WithString("new_text", mcp.Description("New text to replace with (default mode)")),
		mcp.WithString("old_str", mcp.Description("Alias for old_text")),
		mcp.WithString("new_str", mcp.Description("Alias for new_text")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if CRITICAL risk (default: false)")),
		mcp.WithString("mode", mcp.Description("Edit mode: \"replace\" (default), \"search_replace\", \"regex\"")),
		mcp.WithNumber("occurrence", mcp.Description("Which occurrence to replace: 1=first, 2=second, -1=last, -2=second-to-last (default: all)")),
		// search_replace mode params
		mcp.WithString("pattern", mcp.Description("Regex or literal pattern (for search_replace and regex modes)")),
		mcp.WithString("replacement", mcp.Description("Replacement text (for search_replace mode)")),
		// regex mode params
		mcp.WithString("patterns_json", mcp.Description("JSON array of patterns for regex mode: [{\"pattern\": \"regex\", \"replacement\": \"$1...\", \"limit\": -1}]")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive matching (default: true, for regex mode)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create backup before transformation (default: true, for regex mode)")),
		mcp.WithBoolean("dry_run", mcp.Description("Validate without applying changes (default: false, for regex mode)")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false, for occurrence mode)")),
	)
	regexTransform := reg.regexTransform
	reg.editFileHandler = auditWrap(engine, "edit_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Extract all optional parameters
		args := request.GetArguments()
		mode := ""
		oldText := ""
		newText := ""
		force := false
		dryRun := false
		occurrence := 0

		if args != nil {
			if m, ok := args["mode"].(string); ok {
				mode = m
			}
			if f, ok := args["force"].(bool); ok {
				force = f
			}
			if dr, ok := args["dry_run"].(bool); ok {
				dryRun = dr
			}
			if occ, ok := args["occurrence"].(float64); ok {
				occurrence = int(occ)
			}
			if ot, ok := args["old_text"].(string); ok {
				oldText = ot
			}
			if nt, ok := args["new_text"].(string); ok {
				newText = nt
			}
		}

		// ---- MODE: regex ----
		if mode == "regex" {
			patternsJSON := ""
			if args != nil {
				if pj, ok := args["patterns_json"].(string); ok {
					patternsJSON = pj
				}
			}
			if patternsJSON == "" {
				return mcp.NewToolResultError("patterns_json is required for mode:\"regex\""), nil
			}

			var patterns []core.TransformPattern
			if err := json.Unmarshal([]byte(patternsJSON), &patterns); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to parse patterns JSON: %v", err)), nil
			}

			caseSensitive := true
			createBackup := true
			dryRun := false

			if args != nil {
				if cs, ok := args["case_sensitive"].(bool); ok {
					caseSensitive = cs
				}
				if cb, ok := args["create_backup"].(bool); ok {
					createBackup = cb
				}
				if dr, ok := args["dry_run"].(bool); ok {
					dryRun = dr
				}
			}

			result, err := regexTransform.Transform(ctx, core.RegexTransformConfig{
				FilePath:      path,
				Patterns:      patterns,
				Mode:          core.ModeSequential,
				CaseSensitive: caseSensitive,
				CreateBackup:  createBackup,
				DryRun:        dryRun,
			})

			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Transformation failed: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString("Regex Transformation Complete\n")
			output.WriteString("---\n")
			output.WriteString(fmt.Sprintf("File: %s\n", result.FilePath))
			output.WriteString(fmt.Sprintf("Patterns Applied: %d/%d\n", result.PatternsApplied, len(patterns)))
			output.WriteString(fmt.Sprintf("Total Replacements: %d\n", result.TotalReplacements))
			output.WriteString(fmt.Sprintf("Lines Affected: %d\n", result.LinesAffected))
			output.WriteString(fmt.Sprintf("Duration: %v\n", result.Duration))

			if result.BackupID != "" {
				output.WriteString(fmt.Sprintf("Backup ID: %s\n", result.BackupID))
			}

			if len(result.Errors) > 0 {
				output.WriteString("\nErrors:\n")
				for _, err := range result.Errors {
					output.WriteString(fmt.Sprintf("  - %s\n", err))
				}
			}

			return mcp.NewToolResultText(output.String()), nil
		}

		// ---- MODE: search_replace ----
		if mode == "search_replace" {
			pattern := ""
			replacement := ""
			if args != nil {
				if p, ok := args["pattern"].(string); ok {
					pattern = p
				}
				if r, ok := args["replacement"].(string); ok {
					replacement = r
				}
			}
			if pattern == "" {
				return mcp.NewToolResultError("pattern is required for mode:\"search_replace\""), nil
			}
			if replacement == "" && args != nil {
				if nt, ok := args["new_text"].(string); ok {
					replacement = nt
				}
			}

			resp, err := engine.SearchAndReplace(ctx, path, pattern, replacement, false)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resp.Content) > 0 {
				return mcp.NewToolResultText(resp.Content[0].Text), nil
			}
			return mcp.NewToolResultText("No output"), nil
		}

		// ---- MODE: replace (default) with optional occurrence ----
		if oldText == "" {
			return mcp.NewToolResultError("old_text (or old_str) is required"), nil
		}

		// If occurrence is specified, use ReplaceNthOccurrence
		if occurrence != 0 {
			wholeWord := false
			if args != nil {
				if ww, ok := args["whole_word"].(string); ok {
					wholeWord = (ww == "true" || ww == "True" || ww == "TRUE")
				} else if ww, ok := args["whole_word"].(bool); ok {
					wholeWord = ww
				}
			}

			result, err := engine.ReplaceNthOccurrence(ctx, path, oldText, newText, occurrence, wholeWord)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			if engine.IsCompactMode() {
				return mcp.NewToolResultText(fmt.Sprintf("OK: replaced occurrence #%d", occurrence)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Successfully replaced occurrence #%d\nLine affected: %d\nConfidence: %s",
				occurrence, result.LinesAffected, result.MatchConfidence)), nil
		}

		// Default: standard EditFile
		result, err := engine.EditFile(ctx, path, oldText, newText, force, dryRun)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		// Bug #32: dry_run response format
		if dryRun {
			if engine.IsCompactMode() {
				msg := fmt.Sprintf("DRY RUN: %d changes would be made", result.ReplacementCount)
				if result.RiskWarning != "" {
					msg += result.RiskWarning
				}
				msg += "\nNo changes were written to disk"
				return mcp.NewToolResultText(msg), nil
			}
			msg := fmt.Sprintf("DRY RUN — No changes made\nFile: %s\nWould change: %d replacement(s)\nMatch confidence: %s\nLines affected: %d",
				path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)
			if result.RiskWarning != "" {
				msg += result.RiskWarning
			}
			return mcp.NewToolResultText(msg), nil
		}

		if engine.IsCompactMode() {
			msg := fmt.Sprintf("OK: %d changes (+%d -%d) | %d lines", result.ReplacementCount, result.LinesAdded, result.LinesRemoved, result.TotalLines)
			if result.StartLine > 0 {
				msg += fmt.Sprintf(" | %s#L%d-%d", path, result.StartLine, result.EndLine)
			}
			if result.BackupID != "" {
				msg += fmt.Sprintf(" [with backup | UNDO: backup(action:\"restore\", backup_id:\"%s\")]", result.BackupID)
			}
			if result.RiskWarning != "" {
				msg += result.RiskWarning
			}
			if result.EfficiencyHint != "" {
				msg += "\n" + result.EfficiencyHint
			}
			return mcp.NewToolResultText(msg), nil
		}
		msg := fmt.Sprintf("Successfully edited %s\nChanges: %d replacement(s) (+%d -%d)\nMatch confidence: %s\nFile total lines: %d",
			path, result.ReplacementCount, result.LinesAdded, result.LinesRemoved, result.MatchConfidence, result.TotalLines)
		if result.BackupID != "" {
			msg += fmt.Sprintf("\nBackup ID: %s\nUNDO: backup(action:\"restore\", backup_id:\"%s\")", result.BackupID, result.BackupID)
		}
		if result.RiskWarning != "" {
			msg += result.RiskWarning
		}
		if result.EfficiencyHint != "" {
			msg += "\n" + result.EfficiencyHint
		} else if result.LinesAffected > 10 {
			msg += "\nTIP: Use read_file to verify large edits, or analyze_operation for dry-run preview"
		}
		return mcp.NewToolResultText(msg), nil
	})
	reg.addTool(editFileTool, reg.editFileHandler)
}
