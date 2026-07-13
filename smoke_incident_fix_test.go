package main

// End-to-end integration test for the 2026-07-13 host/sandbox separation fix.
// Drives the MCP tool handlers directly through the registered server so we
// validate the wire contract (initialization, tools/list, help, write_file,
// list_directory, read_file missing) without depending on the stdio
// transport. The matching "spawn the binary on stdio" smoke lives in
// smoke_search_test.go; this test complements it by exercising the
// in-process dispatch where the structured payload and host-scope rules
// are easy to assert.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// newIncidentFixServer returns an in-process MCPServer with the production
// tool set. It does not use the stdio transport; tests call the registered
// handlers directly to avoid the timing complexity of piped JSON-RPC.
func newIncidentFixServer(t *testing.T, allowedDir string) (*server.MCPServer, *core.UltraFastEngine) {
	t.Helper()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedDir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })

	s := server.NewMCPServer("filesystem-ultra", "4.5.29")
	if err := registerTools(s, engine); err != nil {
		t.Fatalf("registerTools: %v", err)
	}
	return s, engine
}

func callServer(t *testing.T, s *server.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	handler, ok := s.ListTools()[name]
	if !ok {
		t.Fatalf("tool %q not registered", name)
	}
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}}
	result, err := handler.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("tool %q: %v", name, err)
	}
	return result
}

func textFromResult(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatalf("no content in result")
	}
	tc, ok := r.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("first content is %T, expected TextContent", r.Content[0])
	}
	return tc.Text
}

func TestIncidentFix_EndToEndContract(t *testing.T) {
	dir := t.TempDir()
	s, _ := newIncidentFixServer(t, dir)

	tools := s.ListTools()
	if got, want := len(tools), 20; got != want {
		t.Errorf("registered tool count = %d, want %d (names=%v)", got, want, toolNames(tools))
	}
	for _, banned := range []string{"create_file", "str_replace", "view", "fs"} {
		if _, ok := tools[banned]; ok {
			t.Errorf("disabled alias %q re-registered", banned)
		}
	}

	helpResult := callServer(t, s, "help", map[string]any{})
	helpText := textFromResult(t, helpResult)
	if !strings.Contains(helpText, "read_file") || !strings.Contains(helpText, "git") {
		t.Errorf("help() catalog is missing core tools: %q", helpText)
	}
	if strings.Contains(helpText, "- `create_file`") {
		t.Errorf("help() still advertises create_file: %q", helpText)
	}
	if !strings.Contains(helpText, "real host filesystem") {
		t.Errorf("help() must surface the host-filesystem scope: %q", helpText)
	}

	path := filepath.Join(dir, "incident.txt")
	writeResult := callServer(t, s, "write_file", map[string]any{
		"path":    path,
		"content": "verified post-write evidence\n",
	})
	if writeResult.IsError {
		t.Fatalf("write_file returned error: %s", textFromResult(t, writeResult))
	}
	sc, ok := writeResult.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("write_file StructuredContent is %T, expected map", writeResult.StructuredContent)
	}
	if got, want := sc["verified"], true; got != want {
		t.Errorf("write_file verified = %v, want %v", got, want)
	}
	if got, want := sc["bytes_written"].(int), len("verified post-write evidence\n"); got != want {
		t.Errorf("write_file bytes_written = %v, want %d", got, want)
	}
	if pathField, _ := sc["path"].(string); !strings.HasSuffix(pathField, "incident.txt") {
		t.Errorf("write_file path = %v, want suffix incident.txt", pathField)
	}

	listResult := callServer(t, s, "list_directory", map[string]any{"path": dir})
	listText := textFromResult(t, listResult)
	if !strings.Contains(listText, "incident.txt") {
		t.Errorf("list_directory immediately after write_file must include the new file: %q", listText)
	}

	missing := callServer(t, s, "read_file", map[string]any{"path": filepath.Join(dir, "does-not-exist.txt")})
	if !missing.IsError {
		t.Fatal("read_file on missing path must return isError=true")
	}
	if !strings.Contains(textFromResult(t, missing), "FILESYSTEM MISMATCH?") {
		t.Errorf("read_file on missing path must append FILESYSTEM MISMATCH? hint, got %q", textFromResult(t, missing))
	}
}

func toolNames(tools map[string]*server.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	return names
}

var _ = os.Getenv
