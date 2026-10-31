package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Claude Code and OpenCode probe server/discover when negotiating protocol
// 2026-07-28. A server that ignores the probe makes the client wait up to its
// startup timeout. Ultra does not implement that revision; it must answer
// -32601 immediately, before and after initialize.
func TestServerDiscover_MethodNotFoundImmediate(t *testing.T) {
	s := newFilesystemMCPServer()
	assertServerDiscoverRejected(t, s)

	resp := s.HandleMessage(context.Background(), []byte("{" +
		"\"jsonrpc\": \"2.0\"," +
		"\"id\": 1," +
		"\"method\": \"initialize\"," +
		"\"params\": {" +
		"\"protocolVersion\": \"2025-11-25\"," +
		"\"capabilities\": {}," +
		"\"clientInfo\": {\"name\": \"discover-test\", \"version\": \"0.0.0\"}" +
		"}}"))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("initialize: got %T", resp)
	}
	assertServerDiscoverRejected(t, s)
}

func assertServerDiscoverRejected(t *testing.T, s *server.MCPServer) {
	t.Helper()
	const probe = "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"server/discover\",\"params\":{}}"
	ch := make(chan any, 1)
	go func() {
		ch <- s.HandleMessage(context.Background(), []byte(probe))
	}()

	var resp any
	select {
	case resp = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("server/discover did not answer within 2s; clients wait the startup timeout when the probe is ignored")
	}
	if resp == nil {
		t.Fatal("server/discover returned no response")
	}
	errResp, ok := resp.(mcp.JSONRPCError)
	if !ok {
		t.Fatalf("server/discover: got %T %#v", resp, resp)
	}
	if errResp.Error.Code != -32601 {
		t.Fatalf("code %d, want -32601", errResp.Error.Code)
	}

	raw, err := json.Marshal(errResp)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	var errObj map[string]any
	if err := json.Unmarshal(wire["error"], &errObj); err != nil {
		t.Fatalf("error object: %v (%s)", err, raw)
	}
	code, _ := errObj["code"].(float64)
	if code != -32601 {
		t.Fatalf("wire code %v, want -32601 (%s)", errObj["code"], raw)
	}
	if string(wire["id"]) != "9" {
		t.Fatalf("wire id %s, want 9", wire["id"])
	}
}
