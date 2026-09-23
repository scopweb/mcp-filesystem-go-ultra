package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestCompletions_NotAdvertised(t *testing.T) {
	s := newFilesystemMCPServer()
	resp := s.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2025-11-25",
			"capabilities": {},
			"clientInfo": {"name": "completions-test", "version": "0.0.0"}
		}
	}`))
	jr, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("initialize: got %T", resp)
	}
	init, ok := jr.Result.(mcp.InitializeResult)
	if !ok {
		t.Fatalf("initialize result: got %T", jr.Result)
	}
	if init.Capabilities.Completions != nil {
		t.Fatal("initialize advertised completions; spec 2025-11-25 has no legal ref for tool paths or actions")
	}
	raw, err := json.Marshal(jr.Result)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire.Capabilities["completions"]; ok {
		t.Fatalf("wire initialize included completions: %s", raw)
	}
}

func TestCompletions_CompleteMethodNotFound(t *testing.T) {
	s := newFilesystemMCPServer()
	resp := s.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "completion/complete",
		"params": {
			"ref": {"type": "ref/resource", "uri": "file:///{+path}"},
			"argument": {"name": "path", "value": ""}
		}
	}`))
	errResp, ok := resp.(mcp.JSONRPCError)
	if !ok {
		t.Fatalf("completion/complete: got %T %#v", resp, resp)
	}
	if errResp.Error.Code != mcp.METHOD_NOT_FOUND {
		t.Fatalf("code %d want METHOD_NOT_FOUND", errResp.Error.Code)
	}
	if !strings.Contains(errResp.Error.Message, "completions") {
		t.Fatalf("message %q", errResp.Error.Message)
	}
}
