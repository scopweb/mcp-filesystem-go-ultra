package main

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
		mcp.WithDescription("list_allowed_directories — Return the sandbox roots this server may read and write. Call this before the first read. Zero parameters. Related: list_directory, read_file, help."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
	reg.addTool(tool, auditWrap(engine, "list_allowed_directories", func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(formatAllowedDirectories(engine)), nil
	}),
		`list_allowed_directories()`,
		`Call this before the first read so the sandbox roots are visible.`,
	)
}

func formatAllowedDirectories(engine *core.UltraFastEngine) string {
	paths := engine.ListedAllowedPaths()
	if len(paths) == 0 {
		if engine.IsCompactMode() {
			return insecureOpenWarning + "\n*"
		}
		return insecureOpenWarning + "\n\n*\n"
	}
	if engine.IsCompactMode() {
		return strings.Join(paths, "\n")
	}
	return "Allowed directories:\n" + strings.Join(paths, "\n") + "\n"
}
