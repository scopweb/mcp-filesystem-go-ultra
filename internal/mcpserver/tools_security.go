package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

func registerSecurityTools(reg *toolRegistry) {
	engine := reg.engine
	tool := mcp.NewTool("security_policy",
		mcp.WithTitleAnnotation("File Security Policy"),
		mcp.WithDescription("security_policy — Read-only description of the file protection policy. "+
			"Reports whether a policy is active, the level names, and that the agent cannot change rules. "+
			"Does not list patterns, protected paths, or the configuration location. "+
			"The policy is enforced even if this tool is never called. Restart the server to change it."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	reg.addTool(tool, auditWrap(engine, "security_policy", func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"enabled":          engine.PolicyEnabled(),
			"levels":           []string{"normal", "read_only", "protected", "hidden"},
			"precedence":       "most_restrictive",
			"agent_can_modify": false,
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return mcp.NewToolResultError("security_policy unavailable"), nil
		}
		return mcp.NewToolResultText(string(raw)), nil
	}),
		`security_policy()`,
	)
}
