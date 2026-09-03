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
	if !strings.Contains(text, "Allowed directories") {
		t.Errorf("verbose header missing:\n%s", text)
	}
	if !strings.Contains(text, "source: cli") {
		t.Errorf("expected source: cli in:\n%s", text)
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

type mockRootsSession struct {
	id    string
	ch    chan mcp.JSONRPCNotification
	roots []mcp.Root
}

func (m *mockRootsSession) SessionID() string { return m.id }
func (m *mockRootsSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return m.ch
}
func (m *mockRootsSession) Initialize()       {}
func (m *mockRootsSession) Initialized() bool { return true }
func (m *mockRootsSession) ListRoots(_ context.Context, _ mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	return &mcp.ListRootsResult{Roots: m.roots}, nil
}

func toFileURI(p string) string {
	abs, _ := filepath.Abs(p)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	s := filepath.ToSlash(abs)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return "file://" + s
}

func TestListAllowedDirectories_ReconsultsRoots(t *testing.T) {
	cliDir := t.TempDir()
	rootDir := t.TempDir()
	t.Cleanup(func() { refreshClientRoots = nil })

	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache: cacheInstance, AllowedPaths: []string{cliDir}, ParallelOps: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })

	s := server.NewMCPServer("test", "0.0.0", server.WithRoots())
	reg := &toolRegistry{server: s, engine: engine, handlers: make(map[string]toolHandler)}
	registerDiscoveryTools(reg)
	registerRootsSync(s, engine, []string{cliDir}, core.RootsReplace)

	first := callListAllowed(t, reg, nil)
	if !strings.Contains(resultText(t, first), filepath.Base(cliDir)) {
		t.Fatalf("CLI root missing: %s", resultText(t, first))
	}

	sess := &mockRootsSession{
		id: "s1", ch: make(chan mcp.JSONRPCNotification, 4),
		roots: []mcp.Root{{URI: toFileURI(rootDir)}},
	}
	ctx := s.WithContext(context.Background(), sess)
	h := reg.handlers["list_allowed_directories"]
	res, err := h(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "list_allowed_directories"}})
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, res)
	want, _ := filepath.Abs(rootDir)
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if !strings.Contains(text, filepath.Clean(want)) {
		t.Fatalf("expected roots path %q in:\n%s", want, text)
	}
	if !strings.Contains(text, "source: roots") {
		t.Fatalf("expected source: roots:\n%s", text)
	}
}
