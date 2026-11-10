package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func registerTraceTools(reg *toolRegistry) {
	tool := mcp.NewTool("set_trace",
		mcp.WithTitleAnnotation("Set Trace"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithRawOutputSchema(setTraceOutputSchema),
		mcp.WithDescription("set_trace — Store orchestrator trace for later mutations in this process. Empty call clears it. A call field still wins. Does not authorize paths or bypass OCC. Related: write_file, edit_file."),
	)
	applyAuthTraceSchema(&tool)
	reg.addTool(tool, auditWrap(reg.engine, "set_trace", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx
		args, _ := request.Params.Arguments.(map[string]interface{})
		trace := core.ParseAuthTrace(args)
		reg.engine.SetAuthTrace(trace)
		return setTraceResult(trace), nil
	}))
}

func setTraceResult(trace core.AuthTrace) *mcp.CallToolResult {
	payload := map[string]any{
		"status":  "cleared",
		"message": "trace cleared",
	}
	text := "trace cleared"
	if !trace.Empty() {
		payload["status"] = "set"
		text = fmt.Sprintf("trace set ticket_id=%s mandate_id=%s", trace.TicketID, trace.MandateID)
		payload["message"] = text
		put := func(k, v string) {
			if v != "" {
				payload[k] = v
			}
		}
		put("mandate_id", trace.MandateID)
		put("policy_version", trace.PolicyVersion)
		put("strength", trace.Strength)
		put("ticket_id", trace.TicketID)
		put("scope_note", trace.ScopeNote)
	}
	return mcp.NewToolResultStructured(payload, text)
}
