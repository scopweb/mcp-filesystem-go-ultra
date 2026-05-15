package main

import (
	"context"
	"fmt"
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
		mcp.WithDescription("list_directory — List directory contents (cached). Related: search_files, read_file, edit_file, create_directory, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to directory (WSL or Windows format)")),
	)
	reg.listDirHandler = auditWrap(engine, "list_directory", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		listing, err := engine.ListDirectoryContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(listing), nil
	})
	reg.addTool(listDirTool, reg.listDirHandler)

	// ============================================================================
	// 5. search_files — Search files (consolidated: mcp_search + smart_search + advanced_text_search + count_occurrences)
	// ============================================================================
	searchFilesTool := mcp.NewTool("search_files",
		mcp.WithTitleAnnotation("Search Files"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("search_files — Search and find files by name or content. Supports regex, count_only, include_content. "+
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
		mcp.WithString("output_format", mcp.Description("Output format: 'text' or 'json' (default: 'text'). Use 'json' for structured AI parsing.")),
		mcp.WithString("output", mcp.Description("Alias for output_format: content, files_with_matches, count")),
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
		outputFormat := "text"

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
			} else if out, ok := args["output"].(string); ok {
				// "output" is the ripgrep-compatible alias for output_format
				outputFormat = out
			}
		}

		// Count-only mode: dispatch to CountOccurrences
		if countOnly {
			result, err := engine.CountOccurrences(ctx, path, pattern, returnLines, caseSensitive, wholeWord)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(result), nil
		}

		// Advanced search mode (with content search, case sensitivity, whole word, context)
		// Bug #32: route ALL content searches through AdvancedTextSearch which properly
		// handles case_sensitive:false. SmartSearch (the default path) ignores this flag.
		if includeContent || wholeWord || includeContext {
			engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{
				"path": path, "pattern": pattern,
				"case_sensitive": caseSensitive, "whole_word": wholeWord,
				"include_context": includeContext, "context_lines": contextLines,
				"output_format": outputFormat,
			}}
			resp, err := engine.AdvancedTextSearch(ctx, engineReq)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resp.Content) > 0 {
				return mcp.NewToolResultText(resp.Content[0].Text), nil
			}
			return mcp.NewToolResultText("No matches"), nil
		}

		// Default: SmartSearch
		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{
			"path": path, "pattern": pattern,
			"include_content": includeContent, "file_types": fileTypes,
		}}
		resp, err := engine.SmartSearch(ctx, engineReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Content) > 0 {
			return mcp.NewToolResultText(resp.Content[0].Text), nil
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
