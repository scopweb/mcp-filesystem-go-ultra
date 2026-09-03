package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
	localmcp "github.com/mcp/filesystem-ultra/mcp"
)

// registerSearchTools registers list_directory, search_files, analyze_operation
func registerSearchTools(reg *toolRegistry) {
	engine := reg.engine

	// ============================================================================
	// 4. list_directory — List directory (consolidated: mcp_list + list_directory)
	// ============================================================================
	listDirTool := mcp.NewTool("list_directory",
		mcp.WithTitleAnnotation("List Directory"),
		mcp.WithDescription("list_directory — List directory contents on the real host filesystem; use it to verify a host creation/edit independently. "+
			"Replaces bash ls/dir/tree — NEVER use the shell. "+
			"output_format: 'compact' (default), 'json' (structured entries with name/type/size/modified), 'tree' (recursive JSON tree, use max_depth). "+
			"Related: search_files, read_file, edit_file, create_directory, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to directory (WSL or Windows format)")),
		mcp.WithString("output_format", mcp.Description("Output format: 'compact' (default), 'json', 'tree' (recursive tree), 'sizes'")),
		mcp.WithNumber("max_depth", mcp.Description("Recursion depth for output_format:'tree' (default: 2)")),
		mcp.WithString("exclude", mcp.Description("JSON array or comma-separated globs to skip in tree")),
		mcp.WithBoolean("respect_ignore", mcp.Description("Honor .gitignore/.cursorignore/.fsultraignore in tree (default: true)")),
		mcp.WithNumber("max_nodes", mcp.Description("Cap nodes in tree output (default: 500)")),
	)
	reg.listDirHandler = auditWrap(engine, "list_directory", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		outputFormat := ""
		maxDepth := 2
		maxNodes := 500
		respectIgnore := true
		var exclude []string
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if of, ok := args["output_format"].(string); ok {
				outputFormat = of
			}
			if md, ok := args["max_depth"].(float64); ok && md > 0 {
				maxDepth = int(md)
			}
			if mn, ok := args["max_nodes"].(float64); ok && mn > 0 {
				maxNodes = int(mn)
			}
			if ri, ok := args["respect_ignore"].(bool); ok {
				respectIgnore = ri
			}
			exclude = parseExcludeArg(args["exclude"])
			if request.Params.Name == "directory_tree" && outputFormat == "" {
				outputFormat = "tree"
			}
		} else if request.Params.Name == "directory_tree" {
			outputFormat = "tree"
		}

		var listing string
		switch outputFormat {
		case "json":
			listing, err = engine.ListDirectoryJSON(ctx, path)
		case "tree":
			format := "json"
			if request.Params.Name == "directory_tree" {
				format = "compact"
			}
			listing, err = engine.ListDirectoryTreeOpts(ctx, core.NormalizePath(path), core.TreeOpts{
				MaxDepth: maxDepth, MaxNodes: maxNodes, Exclude: exclude, RespectIgnore: respectIgnore, Format: format,
			})
		case "sizes":
			listing, err = engine.ListDirectoryJSON(ctx, path)
		default: // "" / "compact" / "text" — current behaviour
			listing, err = engine.ListDirectoryContent(ctx, path)
		}
		if err != nil {
			return mcp.NewToolResultError(formatToolError(err)), nil
		}
		return mcp.NewToolResultText(listing), nil
	})
	reg.addTool(listDirTool, reg.listDirHandler)

	dirTreeTool := mcp.NewTool("directory_tree",
		mcp.WithTitleAnnotation("Directory Tree"),
		mcp.WithDescription("directory_tree — Recursive compact tree of a directory. Alias of list_directory with output_format=tree. Respects .gitignore. Related: list_directory, search_files, read_file."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to directory")),
		mcp.WithString("output_format", mcp.Description("tree (default compact) or json")),
		mcp.WithNumber("max_depth", mcp.Description("Recursion depth (default: 2)")),
		mcp.WithString("exclude", mcp.Description("JSON array or comma-separated globs to skip")),
		mcp.WithBoolean("respect_ignore", mcp.Description("Honor gitignore-style files (default: true)")),
		mcp.WithNumber("max_nodes", mcp.Description("Cap nodes (default: 500)")),
	)
	reg.addTool(dirTreeTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if of, _ := args["output_format"].(string); of == "" {
				args["output_format"] = "tree"
			}
			request.Params.Arguments = args
		} else {
			request.Params.Arguments = map[string]interface{}{"output_format": "tree"}
		}
		request.Params.Name = "directory_tree"
		return reg.listDirHandler(ctx, request)
	})

	// ============================================================================
	// 5. search_files — Search files (consolidated: mcp_search + smart_search + advanced_text_search + count_occurrences)
	// ============================================================================
	searchFilesTool := mcp.NewTool("search_files",
		mcp.WithTitleAnnotation("Search Files"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("search_files — Search and find files by name or content. Replaces bash grep/find/rg — NEVER use the shell. "+
			"Supports regex, count_only, include_content, include_context. "+
			"Default output auto-adapts to match count (ripgrep-style 'path:line:content' for ≤5 matches, verbose with emojis otherwise). "+
			"Use search_files to find, then edit_file to modify. Related: edit_file, read_file, multi_edit, batch_operations."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Base directory or file (WSL or Windows format)")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
		mcp.WithBoolean("include_content", mcp.Description("Include file content search (default: false)")),
		mcp.WithString("file_types", mcp.Description("Comma-separated file extensions (e.g., '.go,.txt')")),
		mcp.WithString("include", mcp.Description("Glob pattern to filter files — alias for file_types (e.g., '*.go', '**/*.ts')")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive search (default: false)")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false)")),
		mcp.WithBoolean("include_context", mcp.Description("Include context lines (default: false)")),
		mcp.WithNumber("context_lines", mcp.Description("Number of context lines (default: 3)")),
		mcp.WithBoolean("count_only", mcp.Description("Count pattern occurrences without full search (default: false)")),
		mcp.WithString("return_lines", mcp.Description("Return line numbers of count matches (true/false, for count_only mode)")),
		mcp.WithString("output_format", mcp.Description("Output format. 'text' = verbose with emojis (legacy default), 'json' = structured for AI parsing. If omitted: auto-detect — ripgrep-style 'path:line:content' when ≤5 matches, verbose when more. Pass 'text' explicitly to force the legacy verbose format regardless of match count.")),
		mcp.WithString("output", mcp.Description("Alias for output_format. Accepts 'text' or 'json'. Legacy values 'content'|'files_with_matches'|'count' are NOT implemented and fall through to the default text branch.")),
		mcp.WithNumber("max_results", mcp.Description("Maximum number of filenames to return (default: uses engine config; cap recommended for large trees)")),
		mcp.WithBoolean("no_ignore", mcp.Description("If true, do not honor .gitignore/.cursorignore (default: false)")),
	)
	reg.searchFilesHandler = auditWrap(engine, "search_files", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// Normalize WSL/Windows paths (Bug #19: was missing, causing /mnt/c/ → C:\mnt\c\)
		path = core.NormalizePath(path)

		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Extract optional parameters
		countOnly := false
		includeContent := false
		caseSensitive := true // Bug #32: default is case-sensitive (true), user must explicitly set false
		wholeWord := false
		includeContext := false
		contextLines := 3
		fileTypes := []interface{}{}
		returnLines := false
		outputFormat := ""     // empty → engine defaults to "auto"; explicit "text" preserves legacy verbose/compact
		contentIntent := false // content-only params passed → content search implied
		noIgnore := false

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if co, ok := args["count_only"].(bool); ok {
				countOnly = co
			}
			if ic, ok := args["include_content"].(bool); ok {
				includeContent = ic
			}
			if cs, ok := args["case_sensitive"].(bool); ok {
				caseSensitive = cs
			}
			if ww, ok := args["whole_word"].(bool); ok {
				wholeWord = ww
			}
			if ic, ok := args["include_context"].(bool); ok {
				includeContext = ic
			}
			if cl, ok := args["context_lines"].(float64); ok {
				contextLines = int(cl)
			}
			if ft, ok := args["file_types"].(string); ok && ft != "" {
				parts := strings.Split(ft, ",")
				for _, part := range parts {
					fileTypes = append(fileTypes, strings.TrimSpace(part))
				}
			} else if inc, ok := args["include"].(string); ok && inc != "" {
				// "include" is the ripgrep-compatible alias for file_types
				parts := strings.Split(inc, ",")
				for _, part := range parts {
					fileTypes = append(fileTypes, strings.TrimSpace(part))
				}
			}
			if rl, ok := args["return_lines"].(string); ok {
				returnLines = (rl == "true" || rl == "True" || rl == "TRUE")
			} else if rl, ok := args["return_lines"].(bool); ok {
				returnLines = rl
			}
			if of, ok := args["output_format"].(string); ok {
				outputFormat = of
				contentIntent = true
			} else if out, ok := args["output"].(string); ok {
				// "output" is the ripgrep-compatible alias for output_format
				outputFormat = out
				contentIntent = true
			}
			if _, ok := args["context_lines"]; ok {
				contentIntent = true
			}
			if ni, ok := args["no_ignore"].(bool); ok {
				noIgnore = ni
			}
		}

		// v4.5.24 false-negative guards:
		// (1) path is a regular FILE → filename search is meaningless, force content search.
		// (2) content-only params (output_format/output/context_lines) without
		//     include_content imply content-search intent — honor it instead of
		//     silently ignoring them in the filename-only SmartSearch path.
		if !includeContent {
			if contentIntent {
				includeContent = true
			} else if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
				includeContent = true
			}
		}

		// Count-only mode: dispatch to CountOccurrences
		if countOnly {
			result, err := engine.CountOccurrences(ctx, path, pattern, returnLines, caseSensitive, wholeWord, noIgnore)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(result), nil
		}

		// Advanced search mode (with content search, case sensitivity, whole word, context)
		// Bug #32: route ALL content searches through AdvancedTextSearch which properly
		// handles case_sensitive:false. SmartSearch (the default path) ignores this flag.
		if includeContent || wholeWord || includeContext {
			advArgs := map[string]interface{}{
				"path": path, "pattern": pattern,
				"case_sensitive": caseSensitive, "whole_word": wholeWord,
				"include_context": includeContext, "context_lines": contextLines,
				"output_format": outputFormat, "no_ignore": noIgnore,
			}
			// Forward optional max_results if caller set one (new param v4.5.26)
			if rawArgs, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if mr, ok := rawArgs["max_results"].(float64); ok && mr > 0 {
					advArgs["max_results"] = mr
				}
			}
			engineReq := localmcp.CallToolRequest{Arguments: advArgs}
			resp, err := engine.AdvancedTextSearch(ctx, engineReq)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resp.Content) > 0 {
				return mcp.NewToolResultText(capSearchOutput(resp.Content[0].Text, engine)), nil
			}
			return mcp.NewToolResultText("No matches"), nil
		}

		// Default: SmartSearch
		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{
			"path": path, "pattern": pattern,
			"include_content": includeContent, "file_types": fileTypes,
			"no_ignore": noIgnore,
		}}
		// Forward optional max_results (new param v4.5.26) — engine falls back to
		// config default when absent, so this is purely advisory.
		if rawArgs, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if mr, ok := rawArgs["max_results"].(float64); ok && mr > 0 {
				engineReq.Arguments["max_results"] = mr
			}
		}
		resp, err := engine.SmartSearch(ctx, engineReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Content) > 0 {
			out := capSearchOutput(resp.Content[0].Text, engine)
			// Fix #3 (v4.5.26): If the call omitted file_types/include AND returned
			// many matches (>200), surface a soft hint. The proxy log showed dozens
			// of search_files calls in 5–45 s on unwalkable trees like CRM/SmartAdmin
			// because the model relied on defaults. The hint costs ~30 output tokens
			// but prevents the next call from re-walking the whole tree.
			if len(out) > 8000 && len(fileTypes) == 0 && !includeContent {
				out += "\n\n💡 hint: this search returned many matches across many files. Next time, pass `file_types` (e.g. \".razor,.cs\") or `include` to skip unrelated trees and keep latency under 1s."
			}
			return mcp.NewToolResultText(out), nil
		}
		return mcp.NewToolResultText("No matches"), nil
	})
	reg.addTool(searchFilesTool, reg.searchFilesHandler)

	// ============================================================================
	// 6. analyze_operation — Analyze operations (Plan Mode / dry-run)
	// ============================================================================
	analyzeOpTool := mcp.NewTool("analyze_operation",
		mcp.WithTitleAnnotation("Analyze Operation"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("analyze_operation — Dry-run preview before executing. Operations: file, optimize, write, edit, delete. "+
			"Related: edit_file, multi_edit, search_files, batch_operations, backup."),
		mcp.WithString("operation", mcp.Required(), mcp.Description("Operation to analyze: file, optimize, write, edit, delete")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithString("content", mcp.Description("Content for write analysis")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced (for edit analysis)")),
		mcp.WithString("new_text", mcp.Description("Replacement text (for edit analysis)")),
	)
	reg.addTool(analyzeOpTool, auditWrap(engine, "analyze_operation", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		operation, err := request.RequireString("operation")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid operation: %v", err)), nil
		}
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		switch operation {
		case "file":
			analysis, err := engine.GetFileAnalysis(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(analysis), nil

		case "optimize":
			suggestion, err := engine.GetOptimizationSuggestion(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(suggestion), nil

		case "write":
			content := ""
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if c, ok := args["content"].(string); ok {
					content = c
				}
			}
			if content == "" {
				return mcp.NewToolResultError("content parameter is required for write analysis"), nil
			}
			analysis, err := engine.AnalyzeWriteChange(ctx, path, content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
			}
			return mcp.NewToolResultText(formatChangeAnalysis(analysis)), nil

		case "edit":
			oldText := ""
			newText := ""
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if ot, ok := args["old_text"].(string); ok {
					oldText = ot
				}
				if nt, ok := args["new_text"].(string); ok {
					newText = nt
				}
			}
			if oldText == "" {
				return mcp.NewToolResultError("old_text parameter is required for edit analysis"), nil
			}
			analysis, err := engine.AnalyzeEditChange(ctx, path, oldText, newText)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
			}
			return mcp.NewToolResultText(formatChangeAnalysis(analysis)), nil

		case "delete":
			analysis, err := engine.AnalyzeDeleteChange(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
			}
			return mcp.NewToolResultText(formatChangeAnalysis(analysis)), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown operation: %s. Valid: file, optimize, write, edit, delete", operation)), nil
		}
	}))
}

// capSearchOutput truncates a search_files response if it exceeds the configured
// output cap. Appends a marker so the model knows the response was truncated
// and how to recover (count_only:true or a narrower path).
//
// Improvement M1+M2: prevents accidental multi-MB responses that waste tokens
// (a single 2.28MB search response was observed in the proxy log, costing
// ~570K output tokens).
func parseExcludeArg(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		return t
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		if strings.HasPrefix(s, "[") {
			var arr []string
			if err := json.Unmarshal([]byte(s), &arr); err == nil {
				return arr
			}
		}
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

func capSearchOutput(text string, engine *core.UltraFastEngine) string {
	maxBytes := core.DefaultMaxSearchOutputBytes
	if cfg := engine.GetConfig(); cfg != nil && cfg.MaxSearchOutputBytes > 0 {
		maxBytes = cfg.MaxSearchOutputBytes
	}
	if len(text) <= maxBytes {
		return text
	}
	truncated := text[:maxBytes]
	marker := fmt.Sprintf("\n\n⚠️ truncated: response exceeded %d KB. Use count_only:true or narrow the path/pattern.\n", maxBytes/1024)
	return truncated + marker
}
