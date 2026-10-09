package mcpserver

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRecovery_ToolPanicBecomesJSONRPCError(t *testing.T) {
	s := server.NewMCPServer("test", "0", server.WithRecovery())
	s.AddTool(mcp.NewTool("boom"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		panic("boom")
	})
	response := s.HandleMessage(t.Context(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom"}}`))
	errResp, ok := response.(mcp.JSONRPCError)
	if !ok {
		t.Fatalf("panic must become a JSON-RPC error, got %T", response)
	}
	if errResp.Error.Code != mcp.INTERNAL_ERROR {
		t.Fatalf("code %d", errResp.Error.Code)
	}
	if errResp.Error.Message == "" {
		t.Fatal("empty recovery message")
	}
}
