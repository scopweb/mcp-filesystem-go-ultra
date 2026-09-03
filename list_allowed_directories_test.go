package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func callListAllowed(t *testing.T, reg *toolRegistry, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	h, ok := reg.handlers["list_allowed_directories"]
	if !ok {
		t.Fatal("list_allowed_directories not registered")
	}
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "list_allowed_directories", Arguments: args}}
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return res
}

func TestListAllowedDirectories_ListsResolvedRoots(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	res := callListAllowed(t, reg, nil)
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	text := resultText(t, res)
	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	want = filepath.Clean(want)
	if !strings.Contains(text, want) {
		t.Errorf("missing resolved root %q in:\n%s", want, text)
	}
	if !strings.Contains(text, "Allowed directories:") {
		t.Errorf("verbose header missing:\n%s", text)
	}
}

func TestListAllowedDirectories_InsecureOpenStar(t *testing.T) {
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:       cacheInstance,
		ParallelOps: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })

	s := server.NewMCPServer("test", "0.0.0")
	reg := &toolRegistry{
		server:   s,
		engine:   engine,
		handlers: make(map[string]toolHandler),
	}
	registerDiscoveryTools(reg)

	res := callListAllowed(t, reg, nil)
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	text := resultText(t, res)
	if !strings.Contains(text, insecureOpenWarning) {
		t.Errorf("missing insecure-open warning:\n%s", text)
	}
	if !strings.Contains(text, "*") {
		t.Errorf("open-access must list * not a fake cwd:\n%s", text)
	}
}

func TestListAllowedDirectories_RejectsUnknownParams(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	res := callListAllowed(t, reg, map[string]any{"path": "C:\\nope"})
	if !res.IsError {
		t.Fatalf("unknown param must be rejected, got: %v", res.Content)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "unknown parameter") {
		t.Errorf("error should mention unknown parameter, got: %q", text)
	}
}

func TestListAllowedDirectories_ExperimentalPrefix(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	all := reg.server.ListTools()
	st, ok := all["list_allowed_directories"]
	if !ok {
		t.Fatal("tool not on server")
	}
	if !strings.Contains(st.Tool.Description, "[EXPERIMENTAL") {
		t.Errorf("experimental tool must carry the prefix, got: %q", st.Tool.Description)
	}
	if st.Tool.RawOutputSchema != nil || st.Tool.OutputSchema.Type != "" {
		t.Fatal("experimental tool must not declare an outputSchema")
	}
}
