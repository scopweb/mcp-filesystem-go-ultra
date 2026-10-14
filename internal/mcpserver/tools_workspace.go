package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func registerWorkspaceTools(reg *toolRegistry) {
	engine := reg.engine
	tool := mcp.NewTool("workspace",
		mcp.WithTitleAnnotation("Workspace"),
		mcp.WithDescription("workspace — Experimental real git worktree for agent edits. "+
			"begin creates a detached worktree at an already-allowed local dest; it does not copy dirty, untracked, or ignored files and does not widen allowed-paths. "+
			"status, diff, and export read that worktree. discard removes only a clean workspace this server created. "+
			"Not a sandbox, not crash-durable, and not a promote. Compilers see dest_path. Ultra only."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("action", mcp.Required(), mcp.Description("begin | status | diff | export | discard. promote and query are rejected.")),
		mcp.WithString("source_path", mcp.Description("begin: absolute path of the git toplevel")),
		mcp.WithString("dest_path", mcp.Description("begin: absolute path of a new directory under an already-allowed parent")),
		mcp.WithString("workspace_id", mcp.Description("status, diff, export, discard")),
		mcp.WithNumber("offset", mcp.Description("diff page start. Continuation requires expected_status_hash")),
		mcp.WithNumber("max_files", mcp.Description("diff page size. Default 20, max 50")),
		mcp.WithString("expected_status_hash", mcp.Description("diff continuation token from status or the previous page")),
		mcp.WithNumber("generation", mcp.Description("optional registry generation; mismatch is STALE_WORKSPACE")),
	)
	reg.addTool(tool, auditWrap(engine, "workspace", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := request.Params.Arguments.(map[string]interface{})
		if args == nil {
			args = map[string]interface{}{}
		}
		return workspaceCall(ctx, engine, args)
	}),
		`workspace(action:"begin", source_path:"C:/repo", dest_path:"C:/ws/job")`,
		`workspace(action:"status", workspace_id:"...")`,
		`workspace(action:"export", workspace_id:"...")`,
		`workspace(action:"discard", workspace_id:"...")`,
	)
}

func workspaceCall(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	if engine != nil && engine.PolicyEnabled() {
		return mcp.NewToolResultError(formatToolError(core.GitPolicyBlock())), nil
	}
	switch stringArg(args, "action") {
	case "begin":
		return workspaceBegin(ctx, engine, args)
	case "status":
		return workspaceStatus(ctx, engine, args)
	case "diff":
		return workspaceDiff(ctx, engine, args)
	case "export":
		return workspaceExport(ctx, engine, args)
	case "discard":
		return workspaceDiscard(ctx, engine, args)
	case "promote", "query":
		return pathErrorResult("WORKSPACE_DEFERRED", "promote and query are outside the experimental MVP", "", nil, "edit dest_path directly; use diff or export to review"), nil
	default:
		return pathErrorResult(errCodeInvalidParams, "action must be begin, status, diff, export, or discard", "", nil, ""), nil
	}
}
