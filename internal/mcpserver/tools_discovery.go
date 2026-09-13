package mcpserver

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const insecureOpenWarning = "WARNING: sandbox disabled (--insecure-open). Entire disk is reachable."

func registerDiscoveryTools(reg *toolRegistry) {
	engine := reg.engine

	tool := mcp.NewTool("list_allowed_directories",
		mcp.WithTitleAnnotation("List Allowed Directories"),
		mcp.WithRawOutputSchema(listAllowedDirectoriesOutputSchema),
		mcp.WithDescription("list_allowed_directories — Return the sandbox roots this server may read and write. Call this before the first read. Zero parameters. Structured also includes profile, roots_mode, readonly, tool_count for subagent handoff. Related: list_directory, read_file, help."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
	reg.addTool(tool, auditWrap(engine, "list_allowed_directories", func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if refreshClientRoots != nil {
			refreshClientRoots(ctx)
		}
		text := formatAllowedDirectories(engine)
		return mcp.NewToolResultStructured(allowedDirectoriesStructured(reg, text), text), nil
	}),
		`list_allowed_directories()`,
		`Call this before the first read so the sandbox roots are visible.`,
	)
}

func formatAllowedDirectories(engine *core.UltraFastEngine) string {
	paths := engine.ListedAllowedPaths()
	source := engine.AllowedSource()
	if len(paths) == 0 {
		if engine.IsCompactMode() {
			return insecureOpenWarning + "\n*"
		}
		return insecureOpenWarning + "\n\n*\n"
	}
	if engine.IsCompactMode() {
		return strings.Join(paths, "\n")
	}
	header := "Allowed directories"
	if source != "" && source != core.AllowedSourceInsecure {
		header += " (source: " + source + ")"
	}
	return header + ":\n" + strings.Join(paths, "\n") + "\n"
}

func allowedDirectoriesStructured(reg *toolRegistry, text string) map[string]any {
	engine := reg.engine
	paths := engine.ListedAllowedPaths()
	if paths == nil {
		paths = []string{}
	}
	source := engine.AllowedSource()
	status := statusOK
	if len(paths) == 0 {
		status = statusEmpty
	}
	profile := string(reg.profile)
	if profile == "" {
		profile = string(profileUltra)
	}
	rootsMode := string(core.RootsReplace)
	if cfg := engine.GetConfig(); cfg != nil && cfg.RootsMode != "" {
		rootsMode = string(cfg.RootsMode)
	}
	toolCount := 0
	if reg.server != nil {
		toolCount = len(reg.server.ListTools())
	}
	return map[string]any{
		"status":        status,
		"paths":         paths,
		"source":        source,
		"insecure_open": source == core.AllowedSourceInsecure,
		"profile":       profile,
		"roots_mode":    rootsMode,
		"readonly":      engine.IsReadOnly(),
		"tool_count":    toolCount,
		"message":       text,
	}
}
