package mcpserver

import (
	"context"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func registerAnalyzeTools(reg *toolRegistry) {
	engine := reg.engine

	tool := mcp.NewTool("analyze_code",
		mcp.WithTitleAnnotation("Analyze Code"),
		mcp.WithRawOutputSchema(analyzeCodeOutputSchema),
		mcp.WithDescription("analyze_code — Read-only code analysis (symbols, lint, local sec, impact). "+
			"For editing use apply_patch/edit_file. For understanding code use analyze_code. Do not use git grep or bash. "+
			"Ultra profile only. Related: search_files, read_file, help."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("action", mcp.Required(), mcp.Description("symbols | lint | sec | impact"),
			mcp.Enum("symbols", "lint", "sec", "impact")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File or directory inside allowed-paths")),
		mcp.WithString("query", mcp.Description("Optional symbol or pattern (symbols filter, impact search)")),
		mcp.WithNumber("max_findings", mcp.Description("Cap findings (default 50)")),
	)
	reg.addTool(tool, auditWrap(engine, "analyze_code", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, err := request.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path = core.NormalizePath(path)
		if !engine.IsPathAllowed(path) {
			return pathErrorResult(errCodeNotAllowed, "access denied", path, nil, "call list_allowed_directories"), nil
		}
		if _, statErr := os.Stat(path); statErr != nil {
			return mcp.NewToolResultError(formatToolError(statErr)), nil
		}
		query := ""
		maxFindings := 50
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if q, ok := args["query"].(string); ok {
				query = q
			}
			if n, ok := args["max_findings"].(float64); ok && n > 0 {
				maxFindings = int(n)
			}
		}

		var result core.AnalyzeResult
		switch action {
		case "symbols":
			result = core.AnalyzeSymbols(path, query, maxFindings)
		case "lint":
			result = core.AnalyzeLint(ctx, path, maxFindings)
		case "sec":
			result = core.AnalyzeSec(path, maxFindings)
		case "impact":
			result = core.AnalyzeImpact(ctx, engine, path, query, maxFindings)
		default:
			return pathErrorResult(errCodeInvalidParams, "unknown action", path, nil, `action must be symbols|lint|sec|impact`), nil
		}
		if result.Status == "unavailable" {
			return pathErrorResult(errCodeUnavailable, result.Message, path, map[string]string{"tool": result.Tool},
				"install the missing binary on PATH; analyze_code will not download it"), nil
		}
		return mcp.NewToolResultStructured(analyzeStructured(result), result.Message), nil
	}),
		`analyze_code(action:"symbols", path:"pkg/", query:"Foo")`,
		`analyze_code(action:"lint", path:"pkg/")`,
		`analyze_code(action:"sec", path:"pkg/file.go")`,
		`analyze_code(action:"impact", path:"pkg/", query:"Foo")`,
	)
}

func analyzeStructured(r core.AnalyzeResult) map[string]any {
	findings := make([]map[string]any, 0, len(r.Findings))
	for _, f := range r.Findings {
		item := map[string]any{"path": f.Path, "message": f.Message}
		if f.Line > 0 {
			item["line"] = f.Line
		}
		if f.Col > 0 {
			item["col"] = f.Col
		}
		if f.Symbol != "" {
			item["symbol"] = f.Symbol
		}
		if f.Kind != "" {
			item["kind"] = f.Kind
		}
		if f.Severity != "" {
			item["severity"] = f.Severity
		}
		if f.Suggestion != "" {
			item["suggestion"] = f.Suggestion
		}
		findings = append(findings, item)
	}
	status := r.Status
	if status == "" {
		status = statusOK
	}
	return map[string]any{
		"status":    status,
		"action":    r.Action,
		"findings":  findings,
		"truncated": r.Truncated,
		"tool":      r.Tool,
		"retryable": false,
		"message":   r.Message,
	}
}
