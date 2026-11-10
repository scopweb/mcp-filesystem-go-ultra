package mcpserver

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// applyAuthTraceSchema adds optional orchestrator trace parameters.
// They are logged and echoed. They do not authorize the call.
func applyAuthTraceSchema(tool *mcp.Tool) {
	if tool == nil {
		return
	}
	if tool.InputSchema.Properties == nil {
		tool.InputSchema.Properties = map[string]any{}
	}
	// Descriptions stay in llms.txt. Repeating them on every mutating tool
	// blows the tools/list byte budget.
	opts := []mcp.ToolOption{
		mcp.WithString("mandate_id"),
		mcp.WithString("policy_version"),
		mcp.WithString("strength", mcp.Enum("explicit", "inferred")),
		mcp.WithString("ticket_id"),
		mcp.WithString("scope_note"),
	}
	for _, opt := range opts {
		opt(tool)
	}
}

// attachAuthTrace echoes caller trace on a successful structured receipt.
// Errors are left unchanged. Empty fields are omitted.
func attachAuthTrace(res *mcp.CallToolResult, trace core.AuthTrace, notice string) {
	if res == nil || res.IsError || (trace.Empty() && notice == "") {
		return
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok || m == nil {
		return
	}
	put := func(k, v string) {
		if v != "" {
			m[k] = v
		}
	}
	put("mandate_id", trace.MandateID)
	put("policy_version", trace.PolicyVersion)
	put("strength", trace.Strength)
	put("ticket_id", trace.TicketID)
	put("scope_note", trace.ScopeNote)
	put("auth_notice", notice)
}

// effectiveAuthTrace is the session trace with non-empty call fields overlaid.
// set_trace stores the call as-is, so it must not inherit the previous context.
func effectiveAuthTrace(engine *core.UltraFastEngine, tool string, args map[string]interface{}) core.AuthTrace {
	call := core.ParseAuthTrace(args)
	if tool == "set_trace" || engine == nil {
		return call
	}
	return engine.AuthTrace().Overlay(call)
}
