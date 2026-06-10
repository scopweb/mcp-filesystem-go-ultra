package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// registerMinifyTools registers the JS minification tool.
//
// The minifier is a pure-stdlib best-effort JS minifier (see
// core.MinifyJS). It strips comments, collapses whitespace, and (by
// default) emits single-line output. The original file is overwritten
// in place — a backup is auto-created before the write so the change
// is always recoverable via backup(action:"undo_last").
func registerMinifyTools(reg *toolRegistry) {
	engine := reg.engine

	minifyTool := mcp.NewTool("minify_js",
		mcp.WithTitleAnnotation("Minify JavaScript"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("minify_js — Minify a JavaScript file in place using a pure-stdlib state-machine "+
			"minifier (no external deps, no Node.js needed). Auto-backup before write — undo with "+
			"backup(action:\"undo_last\"). Use when working with JS that needs to be smaller for "+
			"distribution (e.g. pre-deploy, browser-loaded scripts) but the user does NOT want to "+
			"rebuild via their toolchain. The minifier never modifies string/regex/template contents "+
			"— only comments and whitespace. Best-effort: edge cases (regexes with `/` in char classes, "+
			"tagged templates) are handled with conservative heuristics."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the .js file to minify (overwritten in place)")),
		mcp.WithString("output_path", mcp.Description("Optional: write minified output to this path instead of overwriting path")),
		mcp.WithBoolean("remove_comments", mcp.Description("Strip // and /* */ comments (default: true)")),
		mcp.WithBoolean("collapse_whitespace", mcp.Description("Collapse runs of spaces/tabs to single space where needed (default: true)")),
		mcp.WithBoolean("single_line", mcp.Description("Emit all output on a single line (default: true)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview the minified output without writing (default: false)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create a backup before overwriting (default: true)")),
	)
	reg.addTool(minifyTool, auditWrap(engine, "minify_js", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		normPath := core.NormalizePath(path)

		// Access control
		if !engine.IsPathAllowed(normPath) {
			return mcp.NewToolResultError(fmt.Sprintf("Error: access denied: %s", normPath)), nil
		}

		// TOCTOU defense: re-resolve symlinks and re-authorize the canonical
		// target before reading/writing, so the operation can't be redirected
		// outside the sandbox by a symlink swapped in after the check above.
		if resolved, rErr := engine.ResolveAndAuthorize("minify_js", normPath); rErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", rErr)), nil
		} else {
			normPath = resolved
		}

		// Validate file exists and is a file
		info, err := os.Stat(normPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: cannot stat %s: %v", normPath, err)), nil
		}
		if info.IsDir() {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %s is a directory, not a file", normPath)), nil
		}

		// Parse options with defaults applied
		opts := core.MinifyOptions{
			RemoveComments:     true,
			CollapseWhitespace: true,
			SingleLine:         true,
		}
		outputPath := ""
		dryRun := false
		createBackup := true
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if op, ok := args["output_path"].(string); ok && op != "" {
				outputPath = core.NormalizePath(op)
			}
			if dr, ok := args["dry_run"].(bool); ok {
				dryRun = dr
			}
			if cb, ok := args["create_backup"].(bool); ok {
				createBackup = cb
			}
			if rc, ok := args["remove_comments"].(bool); ok {
				opts.RemoveComments = rc
			}
			if cw, ok := args["collapse_whitespace"].(bool); ok {
				opts.CollapseWhitespace = cw
			}
			if sl, ok := args["single_line"].(bool); ok {
				opts.SingleLine = sl
			}
		}

		// If the user explicitly set ALL three to false, that means
		// "do nothing useful" — warn instead of returning identical output.
		if !opts.RemoveComments && !opts.CollapseWhitespace && !opts.SingleLine {
			return mcp.NewToolResultError("Error: all of remove_comments, collapse_whitespace, and single_line are false — nothing to do. Set at least one to true."), nil
		}

		// Read original
		originalBytes, err := os.ReadFile(normPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error reading %s: %v", normPath, err)), nil
		}
		original := string(originalBytes)

		// Minify
		minified, stats := core.MinifyJS(original, opts)

		// Determine write target
		target := normPath
		if outputPath != "" {
			target = outputPath
			// Validate the output path is also allowed
			if !engine.IsPathAllowed(target) {
				return mcp.NewToolResultError(fmt.Sprintf("Error: output_path %s is not in allowed paths", target)), nil
			}
			// TOCTOU defense: re-resolve + re-authorize the canonical output target
			if resolved, rErr := engine.ResolveAndAuthorize("minify_js", target); rErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", rErr)), nil
			} else {
				target = resolved
			}
		}

		// Dry-run: return the preview without writing
		if dryRun {
			return formatMinifyResult(normPath, target, stats, minified /*wroteFile=*/, false /*backupID=*/, "", engine.IsCompactMode()), nil
		}

		// Create backup (unless explicitly disabled) — only when overwriting
		// the original. Output to a different path doesn't need a backup of
		// the source because the source is untouched.
		var backupID string
		if createBackup && target == normPath && engine.GetBackupManager() != nil {
			prevBackupID := engine.GetCurrentBackupID(normPath)
			backupID, err = engine.GetBackupManager().CreateBackupWithContextAndParent(
				normPath, "minify_js",
				fmt.Sprintf("Minify JS: %d → %d bytes (%.1f%% reduction)",
					stats.InputBytes, stats.OutputBytes, stats.ReductionPercent),
				prevBackupID,
			)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error creating backup: %v", err)), nil
			}
		}

		// Ensure target directory exists (for output_path case)
		if dir := filepath.Dir(target); dir != "" {
			if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error creating directory for %s: %v", target, mkdirErr)), nil
			}
		}

		// Atomic write
		tmpPath := target + ".tmp." + core.SecureRandomSuffix()
		// Preserve original file mode if overwriting, else use 0644
		fileMode := os.FileMode(0644)
		if target == normPath {
			fileMode = info.Mode()
		}
		if err := os.WriteFile(tmpPath, []byte(minified), fileMode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error writing temp file: %v", err)), nil
		}
		if err := os.Rename(tmpPath, target); err != nil {
			os.Remove(tmpPath)
			return mcp.NewToolResultError(fmt.Sprintf("Error finalizing write: %v", err)), nil
		}

		// Invalidate cache if the engine has one
		engine.InvalidateCache(normPath)
		if target != normPath {
			engine.InvalidateCache(target)
		}

		// Update backup chain if we wrote in place
		if target == normPath && backupID != "" {
			engine.SetCurrentBackupID(normPath, backupID)
		}

		return formatMinifyResult(normPath, target, stats, minified /*wroteFile=*/, true, backupID, engine.IsCompactMode()), nil
	}))
}

// formatMinifyResult renders the minify tool response in compact or
// verbose form. Centralized so the dry-run and live paths share the
// same output format.
func formatMinifyResult(sourcePath, target string, stats core.MinifyStats, minified string, wroteFile bool, backupID string, compact bool) *mcp.CallToolResult {
	if compact {
		verb := "MINIFIED"
		if !wroteFile {
			verb = "MINIFY (dry-run)"
		}
		msg := fmt.Sprintf("%s %s | %d→%dB (-%d, %.1f%%) | comments:%d",
			verb, target, stats.InputBytes, stats.OutputBytes, stats.BytesSaved, stats.ReductionPercent, stats.CommentsStripped)
		if target != sourcePath {
			msg += fmt.Sprintf(" | from:%s", sourcePath)
		}
		if backupID != "" {
			shortID := backupID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			msg += fmt.Sprintf(" | UNDO:%s", shortID)
		}
		if stats.Truncated {
			msg += " | truncated:input-malformed"
		}
		return mcp.NewToolResultText(msg)
	}

	var sb strings.Builder
	if wroteFile {
		sb.WriteString("Minify complete\n")
	} else {
		sb.WriteString("Minify DRY RUN — no changes written\n")
	}
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("Source:      %s\n", sourcePath))
	if target != sourcePath {
		sb.WriteString(fmt.Sprintf("Output:      %s\n", target))
	}
	sb.WriteString(fmt.Sprintf("Input bytes: %d\n", stats.InputBytes))
	sb.WriteString(fmt.Sprintf("Output bytes: %d\n", stats.OutputBytes))
	sb.WriteString(fmt.Sprintf("Saved:       %d bytes (%.1f%%)\n", stats.BytesSaved, stats.ReductionPercent))
	sb.WriteString(fmt.Sprintf("Comments stripped: %d\n", stats.CommentsStripped))
	if stats.Truncated {
		sb.WriteString("⚠️ Input was malformed (unterminated string/comment). Output is truncated.\n")
	}
	if backupID != "" {
		sb.WriteString(fmt.Sprintf("\n✓ UNDO:%s\n", backupID))
	}
	if !wroteFile {
		sb.WriteString("\n--- Preview (first 500 chars) ---\n")
		preview := minified
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		sb.WriteString(preview)
		sb.WriteString("\n")
	}
	return mcp.NewToolResultText(sb.String())
}
