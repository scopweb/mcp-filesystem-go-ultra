package mcpserver

import (
	"context"
	"encoding/base64"
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

	// toolExamples feeds help(tool:"X"). Filled by addTool(..., examples...)
	// or, when omitted, by core.ContractExamples.
	toolExamples map[string][]string

	// Named handlers needed by alias registration
	readFileHandler    toolHandler
	writeFileHandler   toolHandler
	editFileHandler    toolHandler
	listDirHandler     toolHandler
	searchFilesHandler toolHandler
}

// addTool registers a tool on the server AND adds its handler to the dispatch map.
// The trailing examples... is optional and only consumed by help(tool:"<name>").
// All 17 existing call sites pass no examples, so this stays source-compatible.
func (r *toolRegistry) addTool(tool mcp.Tool, handler toolHandler, examples ...string) {
	tool = applyExperimentalPolicy(tool)
	r.server.AddTool(tool, handler)
	r.handlers[tool.Name] = handler
	if len(examples) == 0 {
		examples = core.ContractExamples(tool.Name)
	}
	if len(examples) > 0 {
		if r.toolExamples == nil {
			r.toolExamples = make(map[string][]string)
		}
		r.toolExamples[tool.Name] = examples
	}
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
	registerDiscoveryTools(reg)
	registerPatchTools(reg)
	// Aliases disabled: duplicates add noise to discovery, hurt token budget.
	// registerAliases(reg)
	// registerClaudeCodeAliases(reg)
	// registerSuperTool(reg)
	registerHelpTool(reg)

	log.Printf("Registered 24 tools for v%s — aliases disabled except directory_tree", serverVersion)
	return nil
}

// contentHashBytes returns the FNV-1a (8 hex) OCC token for raw file bytes.
func contentHashBytes(raw []byte) string {
	h := fnv.New32a()
	h.Write(raw)
	return fmt.Sprintf("%08x", h.Sum32())
}

// enforceEditOCC validates explicit expected_hash and session auto-OCC before
// any backup or write. Returns (warning, errorResult). If errorResult != nil
// the caller must return it unchanged.
func enforceEditOCC(ctx context.Context, path, expectedHash string, content []byte) (string, *mcp.CallToolResult) {
	actualHash := contentHashBytes(content)
	if expectedHash != "" && actualHash != expectedHash {
		core.SetError(ctx, fmt.Sprintf(
			"stale edit: file content changed since read (expected hash: %s, actual: %s). Re-read the file before editing.",
			expectedHash, actualHash))
		return "", mcp.NewToolResultError(fmt.Sprintf(
			"stale edit: file content changed since read (expected hash: %s, actual: %s). Re-read the file with read_file to get the current content_hash, then retry.",
			expectedHash, actualHash))
	}
	if expectedHash == "" {
		if occSignal := core.CheckAutoOCC(core.NormalizePath(path), actualHash); occSignal.Status != core.FeedbackOK {
			core.SetFeedback(ctx, occSignal)
			if occSignal.BlockOp {
				return "", mcp.NewToolResultError(core.FormatFeedback(occSignal,
					"edit_file blocked: file changed on disk since this session last read it"))
			}
			return "⚠ " + occSignal.Message, nil
		}
	}
	return "", nil
}

func formatEditDryRun(path, oldContent, predicted string, replacements int, extra string, args map[string]interface{}) (string, map[string]any) {
	currentHash := contentHashBytes([]byte(oldContent))
	predictedHash := contentHashBytes([]byte(predicted))
	diff := core.RenderDiff(oldContent, predicted, path, diffFormatArg(args))
	sc := editStructuredFromContents(path, oldContent, oldContent, replacements, 0, 0, "")
	var b strings.Builder
	b.WriteString("DRY RUN — No changes made\n")
	b.WriteString(fmt.Sprintf("File: %s\n", path))
	if extra != "" {
		b.WriteString(extra)
		if !strings.HasSuffix(extra, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString(fmt.Sprintf("current_hash: %s\npredicted_hash: %s\n", currentHash, predictedHash))
	if diff != "" {
		b.WriteString(diff)
		if !strings.HasSuffix(diff, "\n") {
			b.WriteString("\n")
		}
	}
	msg := b.String()
	return msg, attachMessage(sc, msg)
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
	return contentHashBytes(raw), true
}

// verifyOnDiskWrite independently reopens the final host file after the engine
// reports success. Size and hash are derived from the same bytes so the
// structured response cannot mix requested input size with hook/EOL-transformed
// content. The returned path is the canonical authorized target when available.
func verifyOnDiskWrite(engine *core.UltraFastEngine, path string) (verifiedPath string, bytesWritten int, contentHash string, verified bool) {
	verifiedPath = resolveAbsForResponse(path)
	normPath := core.NormalizePath(path)
	if engine != nil {
		if resolved, err := engine.ResolveAndAuthorize("verify_write", normPath); err == nil {
			normPath = resolved
			verifiedPath = resolveAbsForResponse(resolved)
		}
	}

	raw, err := os.ReadFile(normPath)
	if err != nil {
		return verifiedPath, 0, "", false
	}
	return verifiedPath, len(raw), contentHashBytes(raw), true
}

const unverifiedWriteWarning = "POST-WRITE VERIFICATION FAILED: the atomic write returned success, but the host file could not be reopened. Confirm with get_file_info/read_file before continuing; do not repeat the write blindly."

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
	if r.MatchMethod != "" {
		m["match_method"] = r.MatchMethod
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

// attachMessage injects the human-readable text response into the structured
// payload under "message". MCP clients that surface structuredContent ignore the
// text fallback of NewToolResultStructured; without this, such clients lose the
// diff / summary (and, for read_file, the file body — see the read handlers).
// Returns the same map for call-site chaining.
func attachMessage(m map[string]any, msg string) map[string]any {
	if m == nil {
		m = map[string]any{}
	}
	m["message"] = msg
	return m
}

// writeStructured builds the structured payload for write_file responses.
// bytesWritten and content_hash are measured by reopening the host file after
// the write. verified is always present so clients never have to infer whether
// the evidence came from an actual read-back.
func writeStructured(absPath string, bytesWritten int, contentHash string, verified bool) map[string]any {
	m := map[string]any{
		"path":          absPath,
		"bytes_written": bytesWritten,
		"verified":      verified,
	}
	if contentHash != "" {
		m["content_hash"] = contentHash
	}
	return m
}

// attachParentBackup adds parent_backup_id to a structured payload when the
// backup chain has a previous entry, mirroring the "chain:" segment of the
// compact text response. Defensive: no-ops when backupID is empty, the engine
// is nil, or the engine has no backup manager wired up. Helper-only — does
// not change the existing signatures of editStructured / multiEditStructured.
func attachParentBackup(m map[string]any, engine *core.UltraFastEngine, backupID string) map[string]any {
	if backupID == "" || engine == nil || engine.GetBackupManager() == nil {
		return m
	}
	if info, err := engine.GetBackupManager().GetBackupInfo(backupID); err == nil && info.PreviousBackupID != "" {
		m["parent_backup_id"] = info.PreviousBackupID
	}
	return m
}

// editStructuredFromContents builds a schema-valid edit_file structured payload
// for code paths that do not return a fully-populated *core.EditResult
// (search_replace, occurrence and regex modes). Line stats are the real
// Myers-diff line counts (identical context lines are NOT counted) with a
// legacy span-based fallback when the DP matrix guard says the file is too
// large for an exact diff.
func editStructuredFromContents(path, oldContent, newContent string, replacements, oldSpanLines, newSpanLines int, backupID string) map[string]any {
	totalLines := core.CountLines(newContent)
	linesAdded, linesRemoved, exact := core.DiffCounts(oldContent, newContent)
	if !exact {
		// Legacy span estimate (DP matrix guard for pathological files).
		originalLines := core.CountLines(oldContent)
		linesRemoved = oldSpanLines * replacements
		linesAdded = newSpanLines * replacements
		if net := linesAdded - linesRemoved; net != totalLines-originalLines {
			if totalLines >= originalLines {
				linesAdded = totalLines - originalLines + linesRemoved
			} else {
				linesRemoved = originalLines - totalLines + linesAdded
			}
		}
	}
	m := map[string]any{
		"path":          path,
		"replacements":  replacements,
		"lines_added":   linesAdded,
		"lines_removed": linesRemoved,
		"total_lines":   totalLines,
		"content_hash":  contentHashBytes([]byte(newContent)),
	}
	if backupID != "" {
		m["backup_id"] = backupID
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
		mcp.WithRawOutputSchema(readFileOutputSchema),
		mcp.WithDescription("read_file — Read file contents from the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Replaces bash cat/head/tail/cut/sed -n — NEVER use the shell. "+
			"Logs: mode=\"tail\" max_lines=40 (each line auto-cut to 300 chars; max_line_length:0 disables). "+
			"Range: start_line/end_line (absolute, inclusive) OR start_line/max_lines (count). Binary: encoding=\"base64\". Batch: paths JSON array. "+
			"To MODIFY files use edit_file."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Description("Path to file (WSL or Windows format). Required unless paths is provided.")),
		mcp.WithArray("paths", mcp.WithStringItems(), mcp.Description("Native array of paths, or a JSON array string (legacy adapter). e.g. [\"file1.txt\",\"file2.txt\"]")),
		mcp.WithNumber("max_lines", mcp.Description("Max lines (optional, 0=all). With mode=tail/head this is tail -N / head -N. With start_line set and end_line omitted, this is a LINE COUNT from start_line (offset+count range read, e.g. start_line:100, max_lines:50 reads lines 100-149) — use this instead of guessing end_line when you only know how many lines you want.")),
		mcp.WithNumber("max_line_length", mcp.Description("Max characters per line (cut -c). head/tail default 300. 0=no cut. Use on logs; do not use when you need exact text for edit_file.")),
		mcp.WithString("mode", mcp.Description("all (default) | head | tail. Logs: mode=tail max_lines=40. Replaces bash tail/head."), mcp.Enum("all", "head", "tail")),
		mcp.WithNumber("start_line", mcp.Description("Starting line number (1-indexed) for range read. Pair with end_line (absolute line number) OR max_lines (line count) — not both.")),
		mcp.WithNumber("end_line", mcp.Description("Ending line number for range read — an ABSOLUTE line number, not a count of lines. If you know how many lines you want instead of where they end, use max_lines with start_line and omit end_line.")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" to read file as base64-encoded binary")),
	)
	reg.readFileHandler = auditWrap(engine, "read_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Batch mode: read multiple files in one call
		// Note: If BOTH path AND paths are provided, AND range params are set,
		// we prioritize path+range over paths (batch) to avoid confusion.
		var paths []string
		var usePathRange bool

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if v, ok := args["paths"]; ok && v != nil {
				decoded, err := core.DecodePaths(v)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				paths = decoded
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
			// FASE1: structuredContent conformance. No content_hash here —
			// per-file hashes will come in a later phase (the readFileOutputSchema
			// declares content_hash as optional precisely for this branch).
			combined := results.String()
			return mcp.NewToolResultStructured(map[string]any{"content": combined}, combined), nil
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
		maxLineLength := 0
		maxLineLengthSet := false

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
			if mll, ok := args["max_line_length"].(float64); ok {
				maxLineLength = int(mll)
				maxLineLengthSet = true
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
				return mcp.NewToolResultStructured(map[string]any{"content": body, "content_hash": contentHash}, body), nil
			}
			// FASE1: structuredContent conformance fallback (file read OK but
			// computeFileOCCHash failed — usually transient lock). No
			// content_hash; the schema declares it optional.
			return mcp.NewToolResultStructured(map[string]any{"content": body}, body), nil
		}

		// Range read mode: read specific line range
		if startLine > 0 && endLine == 0 {
			if maxLines > 0 {
				// offset+count convention (start_line + max_lines, no end_line) —
				// mirrors Claude Code's own Read tool (offset/limit). Added
				// because agents that treat "end_line" as a count instead of an
				// absolute line number were hitting an inverted-range error
				// (end_line < start_line) instead of getting what they meant.
				endLine = startLine + maxLines - 1
			} else {
				endLine = 999999
			}
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
			if maxLineLengthSet && maxLineLength > 0 {
				content = truncateLineWidths(content, maxLineLength)
			}
			if contentHash, ok := computeFileOCCHash(core.NormalizePath(path)); ok {
				core.RecordReadHash(core.NormalizePath(path), contentHash) // new point 4
				return mcp.NewToolResultStructured(map[string]any{"content": content, "content_hash": contentHash}, content), nil
			}
			// FASE1: structuredContent conformance fallback (range read OK but
			// computeFileOCCHash failed). No content_hash; schema declares it optional.
			return mcp.NewToolResultStructured(map[string]any{"content": content}, content), nil
		}

		// Default: read full file
		content, err := engine.ReadFileContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(formatToolError(err)), nil
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

		lineLimit := maxLineLength
		if !maxLineLengthSet && (mode == "head" || mode == "tail") {
			lineLimit = defaultHeadTailLineLength
		}
		if lineLimit > 0 {
			content = truncateLineWidths(content, lineLimit)
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
			map[string]any{"content": content, "content_hash": contentHash},
			content,
		), nil
	})
	reg.addTool(readFileTool, reg.readFileHandler,
		`read_file(path:"file.go")`,
		`read_file(path:"logs/app.txt", mode:"tail", max_lines:40)`,
		`read_file(path:"file.go", start_line:10, end_line:40)`,
		`read_file(path:"file.go", start_line:100, max_lines:50)`, // lines 100-149 — count instead of absolute end_line
		`read_file(path:"file.bin", encoding:"base64")`,
	)

	// ============================================================================
	// 2. write_file — Write file (consolidated: mcp_write + write_file + create_file + write_base64 + streaming_write + intelligent_write)
	// ============================================================================
	writeFileTool := mcp.NewTool("write_file",
		mcp.WithTitleAnnotation("Write File"),
		mcp.WithRawOutputSchema(writeFileOutputSchema),
		mcp.WithDescription("write_file — Write/Create files on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use write_file for ALL project files — never use the runtime's built-in write/create tools for host paths; those may target a different sandbox. "+
			"Returns verified post-write host evidence, but still confirm each mutation independently with get_file_info/list_directory and read_file when content matters. "+
			"Creates or overwrites. For binary use content_base64 with encoding:\"base64\". "+
			"WARNING: To modify/edit existing files use edit_file instead. Related: edit_file, multi_edit, copy_file, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write (WSL or Windows format)")),
		mcp.WithString("content", mcp.Description("Text content to write to the file")),
		mcp.WithString("content_base64", mcp.Description("Base64-encoded binary content to write")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" when content is base64-encoded")),
		mcp.WithString("mode", mcp.Description("overwrite (default) or append. append does not trigger rewrite-guard."), mcp.Enum("overwrite", "append")),
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

		// The final response path is resolved after the write by
		// verifyOnDiskWrite, which can report the canonical authorized target.

		// Check for base64 content
		contentBase64 := ""
		encoding := ""
		content := ""
		writeMode := "overwrite"

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
			if m, ok := args["mode"].(string); ok && m != "" {
				writeMode = strings.ToLower(m)
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

			_, err := engine.WriteBase64(ctx, path, b64Content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			// Reopen the final host file so the payload carries actual disk
			// evidence, not merely the decoded input length.
			verifiedPath, bytesWritten, b64ContentHash, verified := verifyOnDiskWrite(engine, path)
			if verified {
				core.RecordWriteHash(core.NormalizePath(path), b64ContentHash)
			}
			msg := fmt.Sprintf("WRITTEN %s %s | %dB", diskPrefix(verifiedPath), verifiedPath, bytesWritten)
			if !engine.IsCompactMode() {
				msg += " base64"
			}
			if !verified {
				msg += "\n⚠ " + unverifiedWriteWarning
			}
			sc := writeStructured(verifiedPath, bytesWritten, b64ContentHash, verified)
			if !verified {
				sc["feedback"] = unverifiedWriteWarning
			}
			return mcp.NewToolResultStructured(attachMessage(sc, msg), msg), nil
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
		if writeMode == "append" {
			if existing, err := os.ReadFile(normPath); err == nil {
				content = string(existing) + content
			}
			err = engine.WriteFileContent(ctx, path, content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			verifiedPath, bytesWritten, writeContentHash, verified := verifyOnDiskWrite(engine, path)
			if verified {
				core.RecordWriteHash(normPath, writeContentHash)
			}
			msg := fmt.Sprintf("APPENDED %s %s | %dB", diskPrefix(verifiedPath), verifiedPath, bytesWritten)
			sc := writeStructured(verifiedPath, bytesWritten, writeContentHash, verified)
			return mcp.NewToolResultStructured(attachMessage(sc, msg), msg), nil
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
			// Reopen once after the write so hooks/EOL transformations are reflected
			// in both the byte count and OCC token.
			verifiedPath, bytesWritten, writeContentHash, verified := verifyOnDiskWrite(engine, path)
			if verified {
				core.RecordWriteHash(normPath, writeContentHash)
			}
			core.SetFeedback(ctx, signal)
			if engine.IsCompactMode() && !signal.Downgraded {
				msg := fmt.Sprintf("WRITTEN %s %s | %dB | %s", diskPrefix(verifiedPath), verifiedPath, bytesWritten, core.FormatFeedbackCompact(signal))
				if !verified {
					msg += " | " + unverifiedWriteWarning
				}
				sc := writeStructured(verifiedPath, bytesWritten, writeContentHash, verified)
				if newBackupID != "" {
					sc["backup_id"] = newBackupID
				}
				sc["feedback"] = core.FormatFeedbackCompact(signal)
				if !verified {
					sc["feedback"] = sc["feedback"].(string) + " | " + unverifiedWriteWarning
				}
				return mcp.NewToolResultStructured(attachMessage(sc, msg), msg), nil
			}
			msg := core.FormatFeedback(signal, fmt.Sprintf("WRITTEN %s %s | %dB", diskPrefix(verifiedPath), verifiedPath, bytesWritten))
			if !verified {
				msg += "\n⚠ " + unverifiedWriteWarning
			}
			sc := writeStructured(verifiedPath, bytesWritten, writeContentHash, verified)
			if newBackupID != "" {
				sc["backup_id"] = newBackupID
			}
			// Use the already-formatted feedback string (preserves the exact
			// newline/arrow formatting FormatFeedback produces) as the value
			// of the structured feedback field. No re-formatting — easier to
			// keep the two surfaces in sync.
			sc["feedback"] = core.FormatFeedback(signal, "")
			if !verified {
				sc["feedback"] = sc["feedback"].(string) + "\n" + unverifiedWriteWarning
			}
			return mcp.NewToolResultStructured(attachMessage(sc, msg), msg), nil
		}

		err = engine.WriteFileContent(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		verifiedPath, bytesWritten, writeContentHash, verified := verifyOnDiskWrite(engine, path)
		if verified {
			core.RecordWriteHash(normPath, writeContentHash)
		}
		msg := fmt.Sprintf("WRITTEN %s %s | %dB", diskPrefix(verifiedPath), verifiedPath, bytesWritten)
		if !verified {
			msg += "\n⚠ " + unverifiedWriteWarning
		}
		sc := writeStructured(verifiedPath, bytesWritten, writeContentHash, verified)
		if !verified {
			sc["feedback"] = unverifiedWriteWarning
		}
		return mcp.NewToolResultStructured(attachMessage(sc, msg), msg), nil
	})
	reg.addTool(writeFileTool, reg.writeFileHandler)

	// ============================================================================
	// 3. edit_file — Edit file (consolidated: mcp_edit + edit_file + search_and_replace + replace_nth_occurrence + regex_transform_file + smart_edit + intelligent_edit + recovery_edit)
	// ============================================================================
	editFileTool := mcp.NewTool("edit_file",
		mcp.WithTitleAnnotation("Edit File"),
		mcp.WithRawOutputSchema(editFileOutputSchema),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("edit_file — Edit existing files on the real host filesystem (the user's actual disk, e.g. C:\\, D:\\, /mnt/...). "+
			"Use edit_file for ALL project file modifications — never use the runtime's built-in edit tools for host paths; those may target a different sandbox. "+
			"After success, verify independently with get_file_info/list_directory and read_file when content matters. "+
			"Modes: default (exact match replace), search_replace (regex/literal all occurrences), regex (capture groups). "+
			"Auto-backup on every edit — undo with backup(action:\"undo_last\"). Related: multi_edit, read_file, search_files, batch_operations."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced (default mode)")),
		mcp.WithString("new_text", mcp.Description("New text to replace with (default mode)")),
		mcp.WithString("old_str", mcp.Description("Alias for old_text")),
		mcp.WithString("new_str", mcp.Description("Alias for new_text")),
		mcp.WithBoolean("force", mcp.Description("Force the operation through the risk-threshold check (CRITICAL risk). A safety backup is always created. Note: force does NOT bypass the accidental-rewrite guard — use allow_rewrite for that. Default: false.")),
		mcp.WithBoolean("allow_rewrite", mcp.Description("Bypass ONLY the accidental full-file rewrite guard (small old_text + large new_text with file content remaining). Prefer write_file for a real full-file rewrite; set allow_rewrite:true only when you genuinely want edit semantics on a near-total rewrite. A safety backup is created. Default: false.")),
		mcp.WithString("mode", mcp.Description("Edit mode: \"replace\" (default), \"search_replace\", \"regex\", \"delete_range\" (remove lines start_line..end_line, or start_line+line_count), \"replace_range\" (replace lines start_line..end_line, or start_line+line_count, with new_text), \"insert\" (insert new_text before/after anchor without replacing anything)"), mcp.Enum("replace", "search_replace", "regex", "delete_range", "replace_range", "insert")),
		mcp.WithString("anchor", mcp.Description("Anchor text for mode:\"insert\". Must match exactly once in the file. The anchor is preserved; new_text is inserted on its own line(s).")),
		mcp.WithString("position", mcp.Description("Where to insert relative to the anchor in mode:\"insert\": \"after\" (default) or \"before\"."), mcp.Enum("after", "before")),
		mcp.WithNumber("occurrence", mcp.Description("Which occurrence to replace: 1=first, 2=second, -1=last, -2=second-to-last (default: all)")),
		mcp.WithNumber("start_line", mcp.Description("First line of the range (1-based, inclusive). Used by mode:\"delete_range\" and mode:\"replace_range\". Pair with end_line (absolute) OR line_count (count) — not both.")),
		mcp.WithNumber("end_line", mcp.Description("Last line of the range — an ABSOLUTE line number, not a count of lines. Used by mode:\"delete_range\" and mode:\"replace_range\". If you know how many lines instead of where they end, use line_count with start_line and omit end_line.")),
		mcp.WithNumber("line_count", mcp.Description("Number of lines from start_line, as an alternative to end_line. Used by mode:\"delete_range\" and mode:\"replace_range\" — e.g. start_line:100, line_count:50 targets lines 100-149.")),
		// search_replace mode params
		mcp.WithString("pattern", mcp.Description("Regex or literal pattern. In search_replace mode: literal pattern, all occurrences. In regex mode: regex pattern (synthesized into a single-pattern transformation if patterns_json is not provided).")),
		mcp.WithString("replacement", mcp.Description("Replacement text. Used in search_replace mode, and in regex mode when pattern is provided without patterns_json.")),
		// regex mode params
		mcp.WithArray("patterns", mcp.Description("Native array of regex patterns: [{\"pattern\":\"regex\",\"replacement\":\"$1\",\"limit\":-1}]. Legacy adapter: patterns_json.")),
		mcp.WithString("patterns_json", mcp.Description("JSON array of patterns for regex mode: [{\"pattern\": \"regex\", \"replacement\": \"$1...\", \"limit\": -1}]. Optional: if omitted in regex mode, pattern (with replacement/new_text) or native patterns is used.")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive matching (default: true, for regex mode)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create backup before transformation (default: true, for regex mode)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview changes without writing to disk. Supported in all modes (replace, search_replace, regex, insert, replace_range, delete_range, occurrence). Does not create backups or update the undo chain. Default: false.")),
		mcp.WithString("diff_format", mcp.Description("Controls how the diff is rendered (point 1). \"\"/\"auto\" (default): full diff when small, else a summary with anchors + ranges to save tokens; \"full\": always the complete unified diff; \"summary\": per-hunk ranges + first/last anchor lines, eliding large bodies (ideal for big block deletions); \"stat\": just \"+added -removed\"; \"none\": no diff.")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false, for occurrence mode)")),
		// Stale-edit protection: hash returned by the prior read_file call. If the
		// file's actual hash doesn't match, the edit is rejected with a clear error.
		// Improvement B3 (see log analysis: 6 stale-edit cycles in 12 days).
		mcp.WithString("expected_hash", mcp.Description("Optional. The content_hash from the last read_file (full, range, head/tail and base64 reads all return it). If the file's current hash doesn't match, the edit is rejected so the model can re-read first.")),
		mcp.WithBoolean("tolerant_whitespace", mcp.Description("Treat tabs and 4-space runs as equivalent (1 tab = 4 spaces) and CRLF/LF as equivalent when matching old_text. Use when the file has mixed indentation (e.g., tabs in some lines, spaces in others). Original file bytes are preserved — only the matching is tolerant. Default: false.")),
		mcp.WithBoolean("strict", mcp.Description("Opt-in strict matching: no implicit fallbacks (trim/regex/escape). Tolerant match only if tolerant_whitespace:true. Default: false.")),
		mcp.WithNumber("expected_matches", mcp.Description("If set, old_text must match exactly this many times or the edit is rejected with candidate line numbers.")),
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
		expectedHash := ""

		if args != nil {
			if m, ok := args["mode"].(string); ok {
				mode = m
			}
			if eh, ok := args["expected_hash"].(string); ok {
				expectedHash = eh
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
		if mode != "" {
			switch mode {
			case "replace", "search_replace", "regex", "delete_range", "replace_range", "insert":
			default:
				return mcp.NewToolResultError(fmt.Sprintf(`parameter "mode": invalid value %q (valid: replace, search_replace, regex, delete_range, replace_range, insert)`, mode)), nil
			}
		}
		strict := false
		var expectedMatches *int
		if args != nil {
			if s, ok := args["strict"].(bool); ok {
				strict = s
			}
			if em, ok := args["expected_matches"].(float64); ok {
				n := int(em)
				expectedMatches = &n
			}
		}
		ctx = core.WithExpectedHash(ctx, expectedHash)
		ctx = core.WithEditPolicy(ctx, core.EditPolicy{Strict: strict, ExpectedMatches: expectedMatches})

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
			var nativePatterns any
			if args != nil {
				nativePatterns = args["patterns"]
			}
			if nativePatterns != nil || patternsJSON != "" {
				decoded, err := core.DecodeDual[[]core.TransformPattern](nativePatterns, patternsJSON, "patterns", "patterns_json")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				patterns = decoded
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

			oldContentRaw, _ := os.ReadFile(normPath)
			autoOCCWarn := ""
			if warn, blocked := enforceEditOCC(ctx, path, expectedHash, oldContentRaw); blocked != nil {
				return blocked, nil
			} else {
				autoOCCWarn = warn
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
			if autoOCCWarn != "" {
				output.WriteString(autoOCCWarn + "\n")
			}

			if dryRun {
				predicted := result.TransformedContent
				output.WriteString(fmt.Sprintf("current_hash: %s\npredicted_hash: %s\n",
					contentHashBytes(oldContentRaw), contentHashBytes([]byte(predicted))))
				if predicted != "" {
					unifiedDiff := core.RenderDiff(string(oldContentRaw), predicted, path, diffFormatArg(args))
					if unifiedDiff != "" {
						output.WriteString("\nDiff (DRY RUN - no changes made):\n")
						output.WriteString(unifiedDiff)
						output.WriteString("\n")
					}
				}
			}

			if len(result.Errors) > 0 {
				output.WriteString("\nErrors:\n")
				for _, err := range result.Errors {
					output.WriteString(fmt.Sprintf("  - %s\n", err))
				}
			}

			newContentStr := result.TransformedContent
			if !dryRun {
				if newRaw, readErr := os.ReadFile(normPath); readErr == nil {
					newContentStr = string(newRaw)
				}
				core.RefreshKnownHashes([]string{normPath})
			}
			msg := output.String()
			return mcp.NewToolResultStructured(attachMessage(
				editStructuredFromContents(path, string(oldContentRaw), newContentStr, result.TotalReplacements, 0, 0, result.BackupID), msg), msg), nil
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
			autoOCCWarn := ""
			if warn, blocked := enforceEditOCC(ctx, path, expectedHash, oldContentRaw); blocked != nil {
				return blocked, nil
			} else {
				autoOCCWarn = warn
			}

			resp, err := engine.SearchAndReplace(ctx, path, pattern, replacement, false, dryRun)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resp.Content) == 0 {
				oldStr := string(oldContentRaw)
				msg := "No output"
				return mcp.NewToolResultStructured(attachMessage(editStructuredFromContents(path, oldStr, oldStr, 0, 0, 0, ""), msg), msg), nil
			}
			respText := resp.Content[0].Text

			// Compute unified diff. In dry-run mode the file on disk is unchanged,
			// so we synthesize the would-be content in memory using the same logic
			// as searchAndReplaceInFile (literal pattern, regexp.QuoteMeta).
			var unifiedDiff string
			var newContentStr string
			if dryRun {
				newContentStr = string(oldContentRaw)
				count := 0
				if re, reErr := regexp.Compile(regexp.QuoteMeta(pattern)); reErr == nil {
					count = len(re.FindAllString(string(oldContentRaw), -1))
					safeReplacement := strings.ReplaceAll(replacement, "$", "$$")
					newContentStr = re.ReplaceAllString(string(oldContentRaw), safeReplacement)
				}
				extra := fmt.Sprintf("Would change: %d replacement(s)\n", count)
				if autoOCCWarn != "" {
					extra += autoOCCWarn + "\n"
				}
				msg, sc := formatEditDryRun(path, string(oldContentRaw), newContentStr, count, extra, args)
				return mcp.NewToolResultStructured(sc, msg), nil
			} else {
				newContentRaw, _ := os.ReadFile(normPath)
				newContentStr = string(newContentRaw)
				unifiedDiff = core.RenderDiff(string(oldContentRaw), newContentStr, path, diffFormatArg(args))
				core.RefreshKnownHashes([]string{normPath})
			}

			count := 0
			if !strings.Contains(respText, "No matches") {
				count = parseReplacementCount(respText)
			}
			structured := func(msg string) map[string]any {
				return attachMessage(editStructuredFromContents(path, string(oldContentRaw), newContentStr, count,
					strings.Count(pattern, "\n")+1, strings.Count(replacement, "\n")+1, ""), msg)
			}

			if engine.IsCompactMode() {
				if strings.Contains(respText, "No matches") {
					msg := "OK: 0 replacements"
					return mcp.NewToolResultStructured(structured(msg), msg), nil
				}
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
				if autoOCCWarn != "" {
					msg += "\n" + autoOCCWarn
				}
				return mcp.NewToolResultStructured(structured(msg), msg), nil
			}

			if unifiedDiff != "" {
				respText += "\nDiff:\n" + unifiedDiff
			}
			if autoOCCWarn != "" {
				respText += "\n" + autoOCCWarn
			}
			return mcp.NewToolResultStructured(structured(respText), respText), nil
		}

		// ---- MODE: replace_range ----
		if mode == "replace_range" {
			startLine, endLine, lineCount := 0, 0, 0
			if args != nil {
				if sl, ok := args["start_line"].(float64); ok {
					startLine = int(sl)
				}
				if el, ok := args["end_line"].(float64); ok {
					endLine = int(el)
				}
				if lc, ok := args["line_count"].(float64); ok {
					lineCount = int(lc)
				}
			}
			if startLine > 0 && endLine == 0 && lineCount > 0 {
				// offset+count convention (start_line + line_count, no end_line) —
				// see read_file's start_line+max_lines for the same idea. Agents
				// that treat end_line as a count get a correct path here instead
				// of an inverted range.
				endLine = startLine + lineCount - 1
			}
			if startLine == 0 || endLine == 0 {
				return mcp.NewToolResultError("mode:\"replace_range\" requires start_line and (end_line or line_count) and new_text"), nil
			}
			normPath := core.NormalizePath(path)
			oldContentRaw, _ := os.ReadFile(normPath)
			autoOCCWarn := ""
			if warn, blocked := enforceEditOCC(ctx, path, expectedHash, oldContentRaw); blocked != nil {
				return blocked, nil
			} else {
				autoOCCWarn = warn
			}
			result, rerr := engine.ReplaceLineRange(ctx, path, startLine, endLine, newText, dryRun)
			if rerr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", rerr)), nil
			}
			if dryRun {
				extra := fmt.Sprintf("Would replace lines %d-%d\n", startLine, endLine)
				if autoOCCWarn != "" {
					extra += autoOCCWarn + "\n"
				}
				msg, sc := formatEditDryRun(path, string(oldContentRaw), result.ModifiedContent, result.ReplacementCount, extra, args)
				return mcp.NewToolResultStructured(sc, msg), nil
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
				return mcp.NewToolResultStructured(attachMessage(attachParentBackup(editStructured(path, result), engine, result.BackupID), msg), msg), nil
			}
			msg := fmt.Sprintf("Replaced lines %d-%d in %s\nLines: +%d -%d\nTotal lines now: %d",
				startLine, endLine, path, result.LinesAdded, result.LinesRemoved, result.TotalLines)
			if result.BackupID != "" {
				msg += fmt.Sprintf("\n✓ UNDO:%s", result.BackupID)
			}
			if result.StructureWarning != "" {
				msg += "\n" + result.StructureWarning
			}
			return mcp.NewToolResultStructured(attachMessage(attachParentBackup(editStructured(path, result), engine, result.BackupID), msg), msg), nil
		}

		// ---- MODE: delete_range ----
		if mode == "delete_range" {
			startLine, endLine, lineCount := 0, 0, 0
			if args != nil {
				if sl, ok := args["start_line"].(float64); ok {
					startLine = int(sl)
				}
				if el, ok := args["end_line"].(float64); ok {
					endLine = int(el)
				}
				if lc, ok := args["line_count"].(float64); ok {
					lineCount = int(lc)
				}
			}
			if startLine > 0 && endLine == 0 && lineCount > 0 {
				endLine = startLine + lineCount - 1
			}
			if startLine == 0 || endLine == 0 {
				return mcp.NewToolResultError("mode:\"delete_range\" requires start_line and (end_line or line_count)"), nil
			}
			normPath := core.NormalizePath(path)
			oldContentRaw, _ := os.ReadFile(normPath)
			autoOCCWarn := ""
			if warn, blocked := enforceEditOCC(ctx, path, expectedHash, oldContentRaw); blocked != nil {
				return blocked, nil
			} else {
				autoOCCWarn = warn
			}
			_, result, derr := engine.DeleteLineRange(ctx, path, startLine, endLine, dryRun)
			if derr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", derr)), nil
			}
			if dryRun {
				extra := fmt.Sprintf("Would delete lines %d-%d\n", startLine, endLine)
				if autoOCCWarn != "" {
					extra += autoOCCWarn + "\n"
				}
				msg, sc := formatEditDryRun(path, string(oldContentRaw), result.ModifiedContent, result.ReplacementCount, extra, args)
				return mcp.NewToolResultStructured(sc, msg), nil
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
				return mcp.NewToolResultStructured(attachMessage(attachParentBackup(editStructured(path, result), engine, result.BackupID), msg), msg), nil
			}
			msg := fmt.Sprintf("Deleted lines %d-%d from %s\nLines removed: %d\nTotal lines now: %d",
				startLine, endLine, path, result.LinesRemoved, result.TotalLines)
			if result.BackupID != "" {
				msg += fmt.Sprintf("\n✓ UNDO:%s", result.BackupID)
			}
			if result.StructureWarning != "" {
				msg += "\n" + result.StructureWarning
			}
			return mcp.NewToolResultStructured(attachMessage(attachParentBackup(editStructured(path, result), engine, result.BackupID), msg), msg), nil
		}

		// ---- MODE: insert ----
		if mode == "insert" {
			anchor := ""
			position := "after"
			if args != nil {
				if a, ok := args["anchor"].(string); ok {
					anchor = a
				}
				if p, ok := args["position"].(string); ok && p != "" {
					position = p
				}
			}
			if anchor == "" {
				return mcp.NewToolResultError("anchor is required for mode:\"insert\""), nil
			}
			if newText == "" {
				return mcp.NewToolResultError("new_text (the text to insert) is required for mode:\"insert\""), nil
			}

			normPath := core.NormalizePath(path)
			oldContentRaw, _ := os.ReadFile(normPath)
			oldContentStr := string(oldContentRaw)
			autoOCCWarn := ""
			if warn, blocked := enforceEditOCC(ctx, path, expectedHash, oldContentRaw); blocked != nil {
				return blocked, nil
			} else {
				autoOCCWarn = warn
			}

			result, err := engine.InsertAtAnchor(ctx, path, anchor, newText, position, dryRun)
			if err != nil {
				return mcp.NewToolResultError(formatToolError(err)), nil
			}
			if dryRun {
				extra := fmt.Sprintf("Would insert %s anchor (line %d)\n", position, result.StartLine)
				if autoOCCWarn != "" {
					extra += autoOCCWarn + "\n"
				}
				msg, sc := formatEditDryRun(path, oldContentStr, result.ModifiedContent, result.ReplacementCount, extra, args)
				return mcp.NewToolResultStructured(sc, msg), nil
			}
			core.RefreshKnownHashes([]string{normPath})
			if result.BackupID != "" {
				engine.SetCurrentBackupID(path, result.BackupID)
			}

			newContentStr := result.ModifiedContent
			if newRaw, readErr := os.ReadFile(normPath); readErr == nil {
				newContentStr = string(newRaw)
			}
			unifiedDiff := core.RenderDiff(oldContentStr, newContentStr, path, diffFormatArg(args))
			if unifiedDiff != "" {
				core.SetDiffLines(ctx, strings.Count(unifiedDiff, "\n"))
			}

			msg := fmt.Sprintf("OK: inserted %s anchor (line %d)", position, result.StartLine)
			if result.BackupID != "" && !engine.IsCompactMode() {
				msg += fmt.Sprintf("\nBackup ID: %s", result.BackupID)
			}
			if autoOCCWarn != "" {
				msg += "\n" + autoOCCWarn
			}
			if unifiedDiff != "" {
				msg += "\n" + unifiedDiff
			}
			return mcp.NewToolResultStructured(attachMessage(editStructured(path, result), msg), msg), nil
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

			oldContentRaw, _ := os.ReadFile(normPath)
			autoOCCWarn := ""
			if warn, blocked := enforceEditOCC(ctx, path, expectedHash, oldContentRaw); blocked != nil {
				return blocked, nil
			} else {
				autoOCCWarn = warn
			}

			result, err := engine.ReplaceNthOccurrence(ctx, path, oldText, newText, occurrence, wholeWord, dryRun)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			if dryRun {
				extra := fmt.Sprintf("Would replace occurrence #%d\n", occurrence)
				if autoOCCWarn != "" {
					extra += autoOCCWarn + "\n"
				}
				msg, sc := formatEditDryRun(path, string(oldContentRaw), result.ModifiedContent, result.ReplacementCount, extra, args)
				return mcp.NewToolResultStructured(sc, msg), nil
			}
			core.RecordWriteHash(core.NormalizePath(path), result.NewHash)

			if engine.IsCompactMode() {
				msg := fmt.Sprintf("OK: replaced occurrence #%d", occurrence)
				return mcp.NewToolResultStructured(attachMessage(editStructured(path, result), msg), msg), nil
			}
			msg := fmt.Sprintf("Successfully replaced occurrence #%d\nLine affected: %d\nConfidence: %s",
				occurrence, result.LinesAffected, result.MatchConfidence)
			if autoOCCWarn != "" {
				msg += "\n" + autoOCCWarn
			}
			return mcp.NewToolResultStructured(attachMessage(editStructured(path, result), msg), msg), nil
		}

		// Default: standard EditFile
		oldContentRaw, _ := os.ReadFile(normPath)
		oldContentStr := string(oldContentRaw)
		autoOCCWarn := ""
		if warn, blocked := enforceEditOCC(ctx, path, expectedHash, oldContentRaw); blocked != nil {
			return blocked, nil
		} else {
			autoOCCWarn = warn
		}

		result, err := engine.EditFile(ctx, path, oldText, newText, force, dryRun, tolerantWhitespace)
		if err != nil {
			// Record failed old_text for reinforcement detection
			core.RecordFailedOldText(path, oldText)
			editSignal := core.CheckEditOp(path, oldText, fileSize, expectedHash != "")
			core.SetFeedback(ctx, editSignal)
			errMsg := formatToolError(err)
			errMsg = core.FormatFeedback(editSignal, errMsg)
			return mcp.NewToolResultError(errMsg), nil
		}

		if dryRun {
			extra := fmt.Sprintf("Would change: %d replacement(s)\n", result.ReplacementCount)
			if result.RiskWarning != "" {
				extra += result.RiskWarning + "\n"
			}
			if autoOCCWarn != "" {
				extra += autoOCCWarn + "\n"
			}
			msg, sc := formatEditDryRun(path, oldContentStr, result.ModifiedContent, result.ReplacementCount, extra, args)
			return mcp.NewToolResultStructured(sc, msg), nil
		}

		core.ResetFailedOldText(path, oldText)
		core.RecordRead(normPath)
		core.RefreshKnownHashes([]string{normPath})
		if result.BackupID != "" {
			engine.SetCurrentBackupID(path, result.BackupID)
		}

		newContentRaw, _ := os.ReadFile(normPath)
		newContentStr := string(newContentRaw)
		unifiedDiff := core.RenderDiff(oldContentStr, newContentStr, path, diffFormatArg(args))
		if unifiedDiff != "" {
			core.SetDiffLines(ctx, strings.Count(unifiedDiff, "\n"))
		}

		editSignal := core.CheckEditOp(path, oldText, fileSize, expectedHash != "")
		newTextSignal := core.CheckEditNewText(newText, fileSize)
		if editSignal.Status != core.FeedbackOK {
			core.SetFeedback(ctx, editSignal)
		} else {
			core.SetFeedback(ctx, newTextSignal)
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
			attachParentBackup(sc, engine, result.BackupID)
			return mcp.NewToolResultStructured(attachMessage(sc, msg), msg), nil
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
		return mcp.NewToolResultStructured(attachMessage(sc, msg), msg), nil
	})
	reg.addTool(editFileTool, reg.editFileHandler)
}
