package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

// bindProgress attaches a reporter only when the request carries a progress
// token and the call has a client session. No token means no notification.
// A notification does not extend the client timeout.
func bindProgress(ctx context.Context, srv *server.MCPServer, request mcp.CallToolRequest) context.Context {
	if srv == nil || request.Params.Meta == nil || request.Params.Meta.ProgressToken == nil {
		return ctx
	}
	token := request.Params.Meta.ProgressToken
	base := ctx
	return core.ContextWithProgress(base, func(done, total int, message string) {
		if base.Err() != nil {
			return
		}
		_ = srv.SendNotificationToClient(base, string(mcp.MethodNotificationProgress), map[string]any{
			"progressToken": token,
			"progress":      float64(done),
			"total":         float64(total),
			"message":       message,
		})
	})
}
