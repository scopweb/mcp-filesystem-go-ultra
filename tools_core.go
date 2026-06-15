package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

// diskPrefix extracts a short disk/volume tag from an absolute path:
//   - C:\, D:\... → [C], [D]
//   - /mnt/c/... → [WSL]
//
// Used in success responses so the caller immediately sees which volume
// the operation targeted, preventing cross-volume confusion.
func diskPrefix(absPath string) string {
	// Strip leading \ or / so HasPrefix/Contains checks work uniformly
	absPath = strings.TrimPrefix(strings.TrimPrefix(absPath, `\`), `/`)
	if strings.HasPrefix(absPath, `mnt/`) {
		// /mnt/c/Users/... → WSL:C
		parts := strings.SplitN(absPath, "/", 4)
		if len(parts) >= 3 {
			return "[WSL:" + strings.ToUpper(parts[2]) + "]"
		}
		return "[WSL]"
	}
	// C:\Users\... → C
	if len(absPath) >= 2 && absPath[1] == ':' {
		return "[" + strings.ToUpper(absPath[:1]) + "]"
	}
	// Fallback
	if len(absPath) > 0 {
		return "[HOST]"
	}
	return "[?]"
}

// resolveAbsForResponse returns the absolute, normalized path that the engine
// will actually operate on. Used in success responses so the caller can see
// exactly where the file was written, even when the input path was a WSL path,
// relative path, or otherwise transformed by NormalizePath.
//
// Falls back to the input path on any error so we never break the response.
func resolveAbsForResponse(path string) string {
	abs, err := filepath.Abs(core.NormalizePath(path))
	if err != nil || abs == "" {
		return path
	}
	return abs
}

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
	registerGitTools(reg)
	registerMinifyTools(reg)
	// Aliases disabled: duplicates add noise to discovery, hurt token budget.
	// registerAliases(reg)
	// registerClaudeCodeAliases(reg)
	// registerSuperTool(reg)
	registerHelpTool(reg)

	log.Printf("Registered 20 tools (18 core + git + help + minify_js) for v%s — aliases disabled", serverVersion)
	return nil
}

// computeFileOCCHash returns the FNV-1a (8 hex) hash of the full file's raw
// bytes — the same OCC token edit_file / multi_edit validate via expected_hash
// (they hash os.ReadFile(path)). It reads the whole file from disk so that
// PARTIAL reads (range, head/tail, base64) can still surface a valid
// concurrency token without forcing the caller to pull the entire file into
// its context (point 3: content_hash on range reads). The disk read is local
// and bounded; only the partial body is returned to the consumer, so the token
// cost stays small. Returns ("", false) if the file cannot be read.
func computeFileOCCHash(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	h := fnv.New32a()
	h.Write(raw)
	return fmt.Sprintf("%08x", h.Sum32()), true
}

// editStructured builds the structured payload returned alongside the text
// response of an edit op (new point 3). Clients that understand structuredContent
// read counts, backup IDs, warnings and the post-edit content_hash from here
// instead of regex-scraping the text; naive clients still get the text fallback.
func editStructured(path string, r *core.EditResult) map[string]any {
	m := map[string]any{
		"path":          path,
		"replacements":  r.ReplacementCount,
		"lines_added":   r.LinesAdded,
		"lines_removed": r.LinesRemoved,
		"total_lines":   r.TotalLines,
	}
	if r.NewHash != "" {
		m["content_hash"] = r.NewHash
	}
	if r.BackupID != "" {
		m["backup_id"] = r.BackupID
	}
	if r.RiskWarning != "" {
		m["risk_warning"] = r.RiskWarning
	}
	if r.StructureWarning != "" {
		m["structure_warning"] = r.StructureWarning
	}
	if r.Integrity != nil {
		m["integrity"] = r.Integrity.Verification
	}
	return m
}

// diffFormatArg reads the optional diff_format argument (point 1). Empty string
// means "auto" — see core.RenderDiff for the supported values.
func diffFormatArg(args map[string]interface{}) string {
	if args != nil {
		if df, ok := args["diff_format"].(string); ok {
			return df
		}
	}
	return ""
}

// registerCoreTools registers read_file, write_file, edit_file
func registerCoreTools(reg *toolRegistry) {
	engine := reg.engine

	// ============================================================================
	// 1. read_file — Read file (consolidated: mcp_read + read_file + read_file_range + read_base64 + chunked_read + intelligent_read)
	// ============================================================================
	readFileTool := mcp.NewTool("read_file",
		mcp.WithTitleAnnotation("Read File"),
		mcp.WithDescription("read_file — Read file contents from the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use read_file for ALL project files. Never use runtime built-in read tools for files on the host disk. "+
			"Supports line ranges (start_line/end_line), head/tail mode, base64 for binary. "+
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
			var body string
			if engine.IsCompactMode() {
				body = encoded
			} else {
				body = fmt.Sprintf("# File: %s (%d bytes)\n# Base64 encoded:\n%s", path, originalSize, encoded)
			}
			// Point 3: surface the whole-file OCC hash for base64 reads too.
			if contentHash, ok := computeFileOCCHash(core.NormalizePath(path)); ok {
				core.RecordReadHash(core.NormalizePath(path), contentHash) // new point 4
				return mcp.NewToolResultStructured(map[string]any{"content_hash": contentHash}, body), nil
			}
			return mcp.NewToolResultText(body), nil
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
			linesRead := endLine - startLine + 1
			core.SetLinesRead(ctx, linesRead)
			// Approximate total lines from file size (avg 50 chars/line)
			if info, err2 := os.Stat(path); err2 == nil && info.Size() > 0 {
				core.SetFileLinesTotal(ctx, int(info.Size()/50)+1)
			}
			// Point 3: surface the whole-file OCC hash for range reads so the
			// caller can use edit_file/multi_edit expected_hash without first
			// pulling the entire file into context.
			if contentHash, ok := computeFileOCCHash(core.NormalizePath(path)); ok {
				core.RecordReadHash(core.NormalizePath(path), contentHash) // new point 4
				return mcp.NewToolResultStructured(map[string]any{"content_hash": contentHash}, content), nil
			}
			return mcp.NewToolResultText(content), nil
		}

		// Default: read full file
		content, err := engine.ReadFileContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		// Record read for stale-read detection in feedback system
		core.RecordRead(core.NormalizePath(path))

		// Compute FNV-1a content hash (8 hex chars). The hash is the OCC token
		// (Improvement B3) — the model can echo it back via edit_file /
		// multi_edit expected_hash to detect stale reads and prevent lost
		// updates under concurrent writes.
		//
		// Bug B1 fix (#23): the hash is no longer appended as a `# content_hash:`
		// line at the end of the response body. That trailer was visually
		// indistinguishable from legitimate Markdown content (same `# comment`
		// syntax), so consumers — human or AI — copied it as an `old_text`
		// anchor in `edit_file` / `multi_edit`, got `no matches found`, and
		// for `multi_edit` (atomic) the whole batch rolled back. The hash is
		// now returned as a structured response field, so it never appears
		// as content. Clients that understand `structuredContent` read it
		// from there; clients that don't see only the file body.
		//
		// Hash is computed on the ORIGINAL content (before truncation) so
		// it remains a valid OCC token against the file on disk regardless
		// of how much of the body we return to the consumer.
		h := fnv.New32a()
		h.Write([]byte(content))
		contentHash := fmt.Sprintf("%08x", h.Sum32())
		core.RecordReadHash(core.NormalizePath(path), contentHash) // new point 4: track for auto-OCC

		// Apply truncation if explicitly requested
		if maxLines > 0 || mode != "all" {
			content = truncateContent(content, maxLines, mode)
		} else {
			// Auto-truncate large files so the model always knows the real total
			// even when Claude Desktop silently truncates the MCP response.
			content = autoTruncateLargeFile(content, path)
		}

		// Annotate lines read for ROI analysis
		totalLines := strings.Count(content, "\n") + 1
		core.SetFileLinesTotal(ctx, totalLines)
		core.SetLinesRead(ctx, totalLines)

		// Return the body as plain text (NO trailer) and the hash as a
		// structured field. Fallback text for naive clients is the file
		// body — they never see a `# content_hash:` line, so they can't
		// mistake it for content.
		return mcp.NewToolResultStructured(
			map[string]any{"content_hash": contentHash},
			content,
		), nil
	})
	reg.addTool(readFileTool, reg.readFileHandler)

	// ============================================================================
	// 2. write_file — Write file (consolidated: mcp_write + write_file + create_file + write_base64 + streaming_write + intelligent_write)
	// ============================================================================
	writeFileTool := mcp.NewTool("write_file",
		mcp.WithTitleAnnotation("Write File"),
		mcp.WithDescription("write_file — Write/Create files on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use write_file for ALL project files — never use the runtime's built-in write/create tools for host paths. "+
			"Creates or overwrites. For binary use content_base64 with encoding:\"base64\". "+
			"WARNING: To modify/edit existing files use edit_file instead. Related: edit_file, multi_edit, copy_file, batch_operations."),
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

		// Pre-flight path validation (surfaces specific error instead of engine's
		// generic "access denied"). Catches pseudo-Linux paths on Windows, NTFS
		// ADS, dangerous Unicode, reserved device names.
		if validationErr := core.ValidatePathSecurity(path); validationErr != nil {
			return mcp.NewToolResultError(validationErr.Error()), nil
		}

		// Resolve the absolute target path so the response shows where the file
		// actually landed (NormalizePath converts WSL → Windows, filepath.Abs
		// resolves relatives). Falls back to the input path on error.
		absPath := resolveAbsForResponse(path)

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
				return mcp.NewToolResultText(fmt.Sprintf("WRITTEN %s %s | %dB", diskPrefix(absPath), absPath, bytesWritten)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("WRITTEN %s %s | %dB base64", diskPrefix(absPath), absPath, bytesWritten)), nil
		}

		// Normal text write
		if content == "" {
			c, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
			}
			content = c
		}

		// Feedback: check for truncation/inflation/full-rewrite patterns.
		// Normalize path once so os.Stat, CreateBackup, and WriteFileContent
		// all see the same target (consistent on Windows/WSL).
		normPath := core.NormalizePath(path)
		var existingSize int64
		if info, statErr := os.Stat(normPath); statErr == nil {
			existingSize = info.Size()
		}
		signal := core.CheckWriteOp(path, content, existingSize)

		// Adaptive downgrade: if CheckWriteOp wants to block AND a backup
		// manager is configured, create a safety backup and proceed with
		// warn instead. If no backup manager or backup creation fails, keep
		// the original block as a safety net. See core/feedback_adaptive.go.
		var newBackupID string
		if signal.BlockOp && engine.GetBackupManager() != nil {
			prevBackupID := engine.GetCurrentBackupID(normPath)
			createBackup := func(p, op, userCtx string) (string, error) {
				return engine.GetBackupManager().CreateBackupWithContextAndParent(
					p, op, userCtx, prevBackupID,
				)
			}
			signal, newBackupID = core.ApplyAdaptiveWriteBlock(
				signal, true, normPath,
				int64(len(content)), existingSize, createBackup,
			)
			// On successful downgrade, link the new backup into the undo chain
			// so backup(action:"undo_last", file_path:"...") can step back.
			if newBackupID != "" {
				engine.SetCurrentBackupID(normPath, newBackupID)
			}
		}

		if signal.BlockOp {
			core.SetFeedback(ctx, signal)
			return mcp.NewToolResultError(signal.Message + "\n→ " + signal.Suggestion), nil
		} else if signal.Status != core.FeedbackOK {
			// Non-blocking warn — proceed but append feedback to response.
			// Always use verbose format (FormatFeedback, not FormatFeedbackCompact)
			// when the warn came from an adaptive downgrade so the backup ID
			// and restore command remain literal and visible to the AI/operator.
			err = engine.WriteFileContent(ctx, path, content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			core.SetFeedback(ctx, signal)
			if engine.IsCompactMode() && !signal.Downgraded {
				return mcp.NewToolResultText(fmt.Sprintf("WRITTEN %s %s | %dB | %s", diskPrefix(absPath), absPath, len(content), core.FormatFeedbackCompact(signal))), nil
			}
			return mcp.NewToolResultText(core.FormatFeedback(signal, fmt.Sprintf("WRITTEN %s %s | %dB", diskPrefix(absPath), absPath, len(content)))), nil
		}

		err = engine.WriteFileContent(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("WRITTEN %s %s | %dB", diskPrefix(absPath), absPath, len(content))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("WRITTEN %s %s | %dB", diskPrefix(absPath), absPath, len(content))), nil
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
		mcp.WithDescription("edit_file — Edit existing files on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use edit_file for ALL project file modifications — never use the runtime's built-in edit tools for host paths. "+
			"Modes: default (exact match replace), search_replace (regex/literal all occurrences), regex (capture groups). "+
			"Auto-backup on every edit — undo with backup(action:\"undo_last\"). Related: multi_edit, read_file, search_files, batch_operations."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced (default mode)")),
		mcp.WithString("new_text", mcp.Description("New text to replace with (default mode)")),
		mcp.WithString("old_str", mcp.Description("Alias for old_text")),
		mcp.WithString("new_str", mcp.Description("Alias for new_text")),
		mcp.WithBoolean("force", mcp.Description("Force the operation through the risk-threshold check (CRITICAL risk). A safety backup is always created. Note: force does NOT bypass the accidental-rewrite guard — use allow_rewrite for that. Default: false.")),
		mcp.WithBoolean("allow_rewrite", mcp.Description("Bypass ONLY the accidental full-file rewrite guard (small old_text + large new_text with file content remaining). Prefer write_file for a real full-file rewrite; set allow_rewrite:true only when you genuinely want edit semantics on a near-total rewrite. A safety backup is created. Default: false.")),
		mcp.WithString("mode", mcp.Description("Edit mode: \"replace\" (default), \"search_replace\", \"regex\", \"delete_range\" (remove lines start_line..end_line), \"replace_range\" (replace lines start_line..end_line with new_text)")),
		mcp.WithNumber("occurrence", mcp.Description("Which occurrence to replace: 1=first, 2=second, -1=last, -2=second-to-last (default: all)")),
		mcp.WithNumber("start_line", mcp.Description("First line of the range (1-based, inclusive). Used by mode:\"delete_range\" and mode:\"replace_range\".")),
		mcp.WithNumber("end_line", mcp.Description("Last line of the range (1-based, inclusive). Used by mode:\"delete_range\" and mode:\"replace_range\".")),
		// search_replace mode params
		mcp.WithString("pattern", mcp.Description("Regex or literal pattern. In search_replace mode: literal pattern, all occurrences. In regex mode: regex pattern (synthesized into a single-pattern transformation if patterns_json is not provided).")),
		mcp.WithString("replacement", mcp.Description("Replacement text. Used in search_replace mode, and in regex mode when pattern is provided without patterns_json.")),
		// regex mode params
		mcp.WithString("patterns_json", mcp.Description("JSON array of patterns for regex mode: [{\"pattern\": \"regex\", \"replacement\": \"$1...\", \"limit\": -1}]. Optional: if omitted in regex mode, pattern + replacement (or new_text) are used as a single transformation.")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive matching (default: true, for regex mode)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create backup before transformation (default: true, for regex mode)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview changes without writing to disk. Supported in modes: replace (default), search_replace, regex. Default: false.")),
		mcp.WithString("diff_format", mcp.Description("Controls how the diff is rendered (point 1). \"\"/\"auto\" (default): full diff when small, else a summary with anchors + ranges to save tokens; \"full\": always the complete unified diff; \"summary\": per-hunk ranges + first/last anchor lines, eliding large bodies (ideal for big block deletions); \"stat\": just \"+added -removed\"; \"none\": no diff.")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false, for occurrence mode)")),
		// Stale-edit protection: hash returned by the prior read_file call. If the
		// file's actual hash doesn't match, the edit is rejected with a clear error.
		// Improvement B3 (see log analysis: 6 stale-edit cycles in 12 days).
		mcp.WithString("expected_hash", mcp.Description("Optional. The content_hash from the last read_file (full, range, head/tail and base64 reads all return it). If the file's current hash doesn't match, the edit is rejected so the model can re-read first.")),
		mcp.WithBoolean("tolerant_whitespace", mcp.Description("Treat tabs and 4-space runs as equivalent (1 tab = 4 spaces) and CRLF/LF as equivalent when matching old_text. Use when the file has mixed indentation (e.g., tabs in some lines, spaces in others). Original file bytes are preserved — only the matching is tolerant. Default: false.")),
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
		allowRewrite := false
		dryRun := false
		occurrence := 0
		tolerantWhitespace := false

		if args != nil {
			if m, ok := args["mode"].(string); ok {
				mode = m
			}
			if f, ok := args["force"].(bool); ok {
				force = f
			}
			if ar, ok := args["allow_rewrite"].(bool); ok {
				allowRewrite = ar
			}
			if tw, ok := args["tolerant_whitespace"].(bool); ok {
				tolerantWhitespace = tw
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
			singlePattern := ""
			singleReplacement := ""
			if args != nil {
				if pj, ok := args["patterns_json"].(string); ok {
					patternsJSON = pj
				}
				if p, ok := args["pattern"].(string); ok {
					singlePattern = p
				}
				if r, ok := args["replacement"].(string); ok {
					singleReplacement = r
				} else if nt, ok := args["new_text"].(string); ok {
					singleReplacement = nt
				}
			}

			normPath := core.NormalizePath(path)

			var patterns []core.TransformPattern
			if patternsJSON != "" {
				if err := json.Unmarshal([]byte(patternsJSON), &patterns); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Failed to parse patterns JSON: %v", err)), nil
				}
			} else if singlePattern != "" {
				// Synthesize a single-pattern array from pattern + replacement (or new_text)
				patterns = []core.TransformPattern{{
					Pattern:     singlePattern,
					Replacement: singleReplacement,
					Limit:       -1,
				}}
			} else {
				return mcp.NewToolResultError("mode:\"regex\" requires either patterns_json, or pattern (with replacement/new_text)"), nil
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

			// Add diff preview for dry run
			if dryRun && result.TransformedContent != "" {
				newContentStr := result.TransformedContent
				oldContentRaw, _ := os.ReadFile(normPath)
				unifiedDiff := core.RenderDiff(string(oldContentRaw), newContentStr, path, diffFormatArg(args))
				if unifiedDiff != "" {
					output.WriteString("\nDiff (DRY RUN - no changes made):\n")
					output.WriteString(unifiedDiff)
					output.WriteString("\n")
				}
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

			normPath := core.NormalizePath(path)
			oldContentRaw, _ := os.ReadFile(normPath)

			resp, err := engine.SearchAndReplace(ctx, path, pattern, replacement, false, dryRun)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resp.Content) == 0 {
				return mcp.NewToolResultText("No output"), nil
			}
			respText := resp.Content[0].Text

			// Compute unified diff. In dry-run mode the file on disk is unchanged,
			// so we synthesize the would-be content in memory using the same logic
			// as searchAndReplaceInFile (literal pattern, regexp.QuoteMeta).
			var unifiedDiff string
			if dryRun {
				if re, reErr := regexp.Compile(regexp.QuoteMeta(pattern)); reErr == nil {
					// Escape $ in replacement (Go interprets $ as capture group reference)
					safeReplacement := strings.ReplaceAll(replacement, "$", "$$")
					previewContent := re.ReplaceAllString(string(oldContentRaw), safeReplacement)
					unifiedDiff = core.RenderDiff(string(oldContentRaw), previewContent, path, diffFormatArg(args))
				}
			} else {
				newContentRaw, _ := os.ReadFile(normPath)
				unifiedDiff = core.RenderDiff(string(oldContentRaw), string(newContentRaw), path, diffFormatArg(args))
			}

			if engine.IsCompactMode() {
				if strings.Contains(respText, "No matches") {
					return mcp.NewToolResultText("OK: 0 replacements"), nil
				}
				count := parseReplacementCount(respText)
				prefix := "OK"
				if dryRun {
					prefix = "DRY RUN"
				}
				msg := fmt.Sprintf("%s: %d replacements (search_replace)", prefix, count)
				if dryRun {
					msg += " — no changes written to disk"
				}
				if unifiedDiff != "" {
					msg += "\n" + unifiedDiff
				}
				return mcp.NewToolResultText(msg), nil
			}

			if unifiedDiff != "" {
				respText += "\nDiff:\n" + unifiedDiff
			}
			return mcp.NewToolResultText(respText), nil
		}

		// ---- MODE: replace_range ----
		if mode == "replace_range" {
			startLine, endLine := 0, 0
			if args != nil {
				if sl, ok := args["start_line"].(float64); ok {
					startLine = int(sl)
				}
				if el, ok := args["end_line"].(float64); ok {
					endLine = int(el)
				}
			}
			if startLine == 0 || endLine == 0 {
				return mcp.NewToolResultError("mode:\"replace_range\" requires start_line and end_line (1-based, inclusive) and new_text"), nil
			}
			result, rerr := engine.ReplaceLineRange(ctx, path, startLine, endLine, newText)
			if rerr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", rerr)), nil
			}
			if result.BackupID != "" {
				engine.SetCurrentBackupID(path, result.BackupID)
			}
			core.RecordWriteHash(core.NormalizePath(path), result.NewHash) // new point 4
			if engine.IsCompactMode() {
				msg := fmt.Sprintf("R %s | lines %d-%d | +%d-%d | %dL", path, startLine, endLine, result.LinesAdded, result.LinesRemoved, result.TotalLines)
				if result.BackupID != "" {
					short := result.BackupID
					if len(short) > 12 {
						short = short[:12]
					}
					msg += " | UNDO:" + short
				}
				if result.StructureWarning != "" {
					msg += "\n" + result.StructureWarning
				}
				return mcp.NewToolResultStructured(editStructured(path, result), msg), nil
			}
			msg := fmt.Sprintf("Replaced lines %d-%d in %s\nLines: +%d -%d\nTotal lines now: %d",
				startLine, endLine, path, result.LinesAdded, result.LinesRemoved, result.TotalLines)
			if result.BackupID != "" {
				msg += fmt.Sprintf("\n✓ UNDO:%s", result.BackupID)
			}
			if result.StructureWarning != "" {
				msg += "\n" + result.StructureWarning
			}
			return mcp.NewToolResultStructured(editStructured(path, result), msg), nil
		}

		// ---- MODE: delete_range ----
		if mode == "delete_range" {
			startLine, endLine := 0, 0
			if args != nil {
				if sl, ok := args["start_line"].(float64); ok {
					startLine = int(sl)
				}
				if el, ok := args["end_line"].(float64); ok {
					endLine = int(el)
				}
			}
			if startLine == 0 || endLine == 0 {
				return mcp.NewToolResultError("mode:\"delete_range\" requires start_line and end_line (1-based, inclusive)"), nil
			}
			_, result, derr := engine.DeleteLineRange(ctx, path, startLine, endLine)
			if derr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", derr)), nil
			}
			if result.BackupID != "" {
				engine.SetCurrentBackupID(path, result.BackupID)
			}
			core.RecordWriteHash(core.NormalizePath(path), result.NewHash) // new point 4
			if engine.IsCompactMode() {
				msg := fmt.Sprintf("D %s | lines %d-%d (-%d) | %dL", path, startLine, endLine, result.LinesRemoved, result.TotalLines)
				if result.BackupID != "" {
					short := result.BackupID
					if len(short) > 12 {
						short = short[:12]
					}
					msg += " | UNDO:" + short
				}
				if result.StructureWarning != "" {
					msg += "\n" + result.StructureWarning
				}
				return mcp.NewToolResultStructured(editStructured(path, result), msg), nil
			}
			msg := fmt.Sprintf("Deleted lines %d-%d from %s\nLines removed: %d\nTotal lines now: %d",
				startLine, endLine, path, result.LinesRemoved, result.TotalLines)
			if result.BackupID != "" {
				msg += fmt.Sprintf("\n✓ UNDO:%s", result.BackupID)
			}
			if result.StructureWarning != "" {
				msg += "\n" + result.StructureWarning
			}
			return mcp.NewToolResultStructured(editStructured(path, result), msg), nil
		}

		// ---- MODE: replace (default) with optional occurrence ----
		if oldText == "" {
			return mcp.NewToolResultError("old_text (or old_str) is required"), nil
		}

		// Feedback: check for stale-read and new_text size patterns
		normPath := core.NormalizePath(path)
		var fileSize int64
		if info, statErr := os.Stat(normPath); statErr == nil {
			fileSize = info.Size()
		}
		if warnSignal := core.CheckEditOp(path, oldText, fileSize); warnSignal.Status != core.FeedbackOK {
			// Non-blocking — annotate response, don't block
			_ = warnSignal // appended to response below
		}
		if newText != "" {
			_ = core.CheckEditNewText(newText, fileSize) // result appended below
		}

		// Guard against accidental full-file rewrite (bug 2026-06-11):
		// short old_text + large new_text with file content remaining after
		// the match → likely the model intended write_file. BLOCK by default.
		//
		// Point 5: the override is the DEDICATED allow_rewrite flag, NOT force.
		// force is reserved for the risk-threshold bypass; coupling the two meant
		// a legitimately risky edit forced through on risk would also silently
		// disable rewrite protection. Decoupling keeps force from being the
		// catch-all "make it work" flag. The recommended fix for a real
		// full-file rewrite remains write_file — allow_rewrite:true is only for
		// the rare case where edit semantics are genuinely wanted on a near-total
		// rewrite.
		if newText != "" {
			if rewriteSignal := core.CheckEditRewrite(oldText, newText, fileSize); rewriteSignal != nil && rewriteSignal.BlockOp {
				core.SetFeedback(ctx, rewriteSignal)
				if !allowRewrite {
					errMsg := core.FormatFeedback(rewriteSignal,
						"edit_file blocked: looks like an accidental full-file rewrite")
					return mcp.NewToolResultError(errMsg), nil
				}
				// allow_rewrite=true: proceed but the audit will record the pattern
				_ = rewriteSignal // already attached via SetFeedback above
			}
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
		// Read old content before edit to compute diff
		oldContentRaw, _ := os.ReadFile(normPath)
		oldContentStr := string(oldContentRaw)

		// Compute the current on-disk hash once — used by both explicit OCC
		// (expected_hash, B3) and automatic OCC (new point 4).
		hh := fnv.New32a()
		hh.Write(oldContentRaw)
		actualHash := fmt.Sprintf("%08x", hh.Sum32())

		expectedHash := ""
		if args != nil {
			if eh, ok := args["expected_hash"].(string); ok {
				expectedHash = eh
			}
		}

		if expectedHash != "" {
			// Improvement B3: explicit stale-edit protection. Mismatch → hard
			// error so the model re-reads instead of silently overwriting.
			if actualHash != expectedHash {
				core.SetError(ctx, fmt.Sprintf(
					"stale edit: file content changed since read (expected hash: %s, actual: %s). Re-read the file before editing.",
					expectedHash, actualHash))
				return mcp.NewToolResultError(fmt.Sprintf(
					"stale edit: file content changed since read (expected hash: %s, actual: %s). Re-read the file with read_file to get the current content_hash, then retry.",
					expectedHash, actualHash)), nil
			}
		}
		autoOCCWarn := ""
		if expectedHash == "" {
			if occSignal := core.CheckAutoOCC(normPath, actualHash); occSignal.Status != core.FeedbackOK {
				// New point 4: the caller didn't opt into expected_hash, but the
				// file changed on disk since this session last saw it. Warn by
				// default; block only when --auto-occ=block.
				core.SetFeedback(ctx, occSignal)
				if occSignal.BlockOp {
					return mcp.NewToolResultError(core.FormatFeedback(occSignal,
						"edit_file blocked: file changed on disk since this session last read it")), nil
				}
				autoOCCWarn = "⚠ " + occSignal.Message
			}
		}

		result, err := engine.EditFile(ctx, path, oldText, newText, force, dryRun, tolerantWhitespace)
		if err != nil {
			// Record failed old_text for reinforcement detection
			core.RecordFailedOldText(path, oldText)
			editSignal := core.CheckEditOp(path, oldText, fileSize)
			core.SetFeedback(ctx, editSignal)
			errMsg := fmt.Sprintf("Error: %v", err)
			errMsg = core.FormatFeedback(editSignal, errMsg)
			return mcp.NewToolResultError(errMsg), nil
		}

		// Successful edit — reset failure counter and record read
		core.ResetFailedOldText(path, oldText)
		core.RecordRead(normPath)
		// New point 4: track our own write so auto-OCC won't flag it as an
		// external change on the next edit.
		core.RecordWriteHash(normPath, result.NewHash)

		// Update backup chain for undo step-through
		if result.BackupID != "" {
			engine.SetCurrentBackupID(path, result.BackupID)
		}

		// Compute unified diff (honors diff_format — point 1)
		newContentRaw, _ := os.ReadFile(normPath)
		newContentStr := string(newContentRaw)
		unifiedDiff := core.RenderDiff(oldContentStr, newContentStr, path, diffFormatArg(args))

		// Annotate audit with diff line count
		if unifiedDiff != "" {
			core.SetDiffLines(ctx, strings.Count(unifiedDiff, "\n"))
		}

		// Collect non-blocking feedback signals
		editSignal := core.CheckEditOp(path, oldText, fileSize)
		newTextSignal := core.CheckEditNewText(newText, fileSize)
		// Annotate audit with the most severe signal
		if editSignal.Status != core.FeedbackOK {
			core.SetFeedback(ctx, editSignal)
		} else {
			core.SetFeedback(ctx, newTextSignal)
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
			// New terse format: M path/to/file | N@+N-N | NL | UNDO:id | chain:parent
			msg := fmt.Sprintf("M %s | %d@+%d-%d | %dL", path, result.ReplacementCount, result.LinesAdded, result.LinesRemoved, result.TotalLines)
			if result.BackupID != "" {
				// Truncate to timestamp only (12 chars) for display
				shortID := result.BackupID
				if len(shortID) > 12 {
					shortID = shortID[:12]
				}
				msg += fmt.Sprintf(" | UNDO:%s", shortID)

				// Show parent chain if exists (for undo step-through)
				if prevID := result.BackupID; len(prevID) > 12 {
					if info, err := engine.GetBackupManager().GetBackupInfo(result.BackupID); err == nil && info.PreviousBackupID != "" {
						parentShort := info.PreviousBackupID
						if len(parentShort) > 12 {
							parentShort = parentShort[:12]
						}
						msg += fmt.Sprintf(" | chain:%s", parentShort)
					}
				}
			}
			if result.RiskWarning != "" {
				msg += " | " + strings.TrimPrefix(result.RiskWarning, "⚠️ ")
			}
			if result.StructureWarning != "" {
				msg += "\n" + result.StructureWarning
			}
			if autoOCCWarn != "" {
				msg += "\n" + autoOCCWarn
			}
			if unifiedDiff != "" {
				msg += "\n" + unifiedDiff
			}
			sc := editStructured(path, result)
			if autoOCCWarn != "" {
				sc["external_change"] = autoOCCWarn
			}
			return mcp.NewToolResultStructured(sc, msg), nil
		}

		// Verbose format: single line summary + optional sections
		msg := fmt.Sprintf("M %s | %d replacement(s) | +%d -%d | %dL", path, result.ReplacementCount, result.LinesAdded, result.LinesRemoved, result.TotalLines)
		if result.BackupID != "" {
			msg += fmt.Sprintf("\n✓ UNDO:%s", result.BackupID)
			// Show parent chain if exists
			if info, err := engine.GetBackupManager().GetBackupInfo(result.BackupID); err == nil && info.PreviousBackupID != "" {
				msg += fmt.Sprintf(" ← chain:%s", info.PreviousBackupID)
			}
		}
		if result.RiskWarning != "" {
			msg += "\n" + result.RiskWarning
		}
		// Append feedback signals (non-blocking)
		msg = core.FormatFeedback(editSignal, msg)
		msg = core.FormatFeedback(newTextSignal, msg)
		// Append unified diff
		if unifiedDiff != "" {
			msg += "\n\nDiff:\n" + unifiedDiff
		}
		// Append full change analysis for AI visibility
		if result.Analysis != nil {
			a := result.Analysis
			msg += "\n\n---\nChange Analysis\n---\n"
			msg += fmt.Sprintf("File: %s\nOperation: edit\nFile exists: %v\n\n", a.FilePath, a.FileExists)
			msg += fmt.Sprintf("Risk Level: %s\n", strings.ToUpper(a.RiskLevel))
			if len(a.RiskFactors) > 0 {
				msg += "Risk Factors:\n"
				for _, factor := range a.RiskFactors {
					msg += fmt.Sprintf("  - %s\n", factor)
				}
			}
			if a.LinesAdded > 0 || a.LinesRemoved > 0 {
				msg += "Changes:\n"
				if a.LinesAdded > 0 {
					msg += fmt.Sprintf("  + %d lines added\n", a.LinesAdded)
				}
				if a.LinesRemoved > 0 {
					msg += fmt.Sprintf("  - %d lines removed\n", a.LinesRemoved)
				}
				if a.LinesModified > 0 {
					msg += fmt.Sprintf("  ~ %d lines modified\n", a.LinesModified)
				}
			}
			if a.Impact != "" {
				msg += fmt.Sprintf("Impact: %s\n", a.Impact)
			}
			if a.Preview != "" {
				msg += fmt.Sprintf("Preview:\n%s\n", a.Preview)
			}
			if len(a.Suggestions) > 0 {
				msg += "Suggestions:\n"
				for _, s := range a.Suggestions {
					msg += fmt.Sprintf("  - %s\n", s)
				}
			}
			if a.EfficiencyTip != "" {
				msg += a.EfficiencyTip + "\n"
			}
		}
		// Append file integrity verification result for HIGH/CRITICAL operations
		if result.Integrity != nil {
			inv := result.Integrity
			if inv.Verification == "OK" {
				msg += fmt.Sprintf("\n✓ integrity:%s|%dL|%dB", inv.Hash[:8], inv.Lines, inv.SizeBytes)
			} else if inv.Verification == "WARNING" {
				msg += fmt.Sprintf("\n⚠️ integrity:WARNING | %s", inv.Warning)
			} else {
				msg += fmt.Sprintf("\n✗ integrity:ERROR | %s", inv.Warning)
			}
		}

		// Point 2: structural balance warning (delimiter imbalance introduced by this edit)
		if result.StructureWarning != "" {
			msg += "\n" + result.StructureWarning
		}
		// New point 4: auto-OCC external-change warning (warn mode)
		if autoOCCWarn != "" {
			msg += "\n" + autoOCCWarn
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

		sc := editStructured(path, result)
		if autoOCCWarn != "" {
			sc["external_change"] = autoOCCWarn
		}
		return mcp.NewToolResultStructured(sc, msg), nil
	})
	reg.addTool(editFileTool, reg.editFileHandler)
}
