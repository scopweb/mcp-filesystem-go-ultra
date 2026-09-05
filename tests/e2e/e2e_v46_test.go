//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func startClient(t *testing.T, exe string, args ...string) *client.Client {
	t.Helper()
	c, err := client.NewStdioMCPClient(exe, nil, args...)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "e2e-v46", Version: "0.1"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

func callErr(t *testing.T, c *client.Client, name string, args map[string]any) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := c.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("%s: transport error: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s: expected tool error, got success: %v", name, res.Content)
	}
	return textOf(t, res)
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T", res.Content[0])
	}
	return tc.Text
}

func TestE2E_V46_LiveUsage(t *testing.T) {
	runLiveV46(t, t.TempDir())
}

func TestE2E_CTemp(t *testing.T) {
	root := `C:\temp`
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Skip("C:\\temp not present")
	}
	workDir := filepath.Join(root, "fsu-v46-live")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })
	runLiveV46(t, workDir)
}

func runLiveV46(t *testing.T, workDir string) {
	t.Helper()
	exe := buildServer(t)
	a := filepath.Join(workDir, "a.txt")
	b := filepath.Join(workDir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("alpha\nBETA\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "sub", "c.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".env"), []byte("SECRET=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := startClient(t, exe, workDir)

	res := call(t, c, "list_allowed_directories", map[string]any{})
	if !strings.Contains(textOf(t, res), workDir) {
		t.Fatalf("list_allowed missing workDir: %s", textOf(t, res))
	}

	res = call(t, c, "help", map[string]any{})
	help := textOf(t, res)
	for _, n := range []string{"list_allowed_directories", "directory_tree", "diff_files", "apply_patch"} {
		if !strings.Contains(help, n) {
			t.Errorf("help missing %s", n)
		}
	}

	res = call(t, c, "directory_tree", map[string]any{"path": workDir, "max_depth": 2})
	tree := textOf(t, res)
	if !strings.Contains(tree, "a.txt") {
		t.Fatalf("directory_tree missing a.txt: %s", tree)
	}

	res = call(t, c, "get_file_info", map[string]any{"path": a})
	info := textOf(t, res)
	if !strings.Contains(info, "SHA-256") && !strings.Contains(strings.ToLower(info), "sha256") {
		t.Errorf("get_file_info missing sha256: %s", info)
	}
	if !strings.Contains(info, "MIME") && !strings.Contains(strings.ToLower(info), "text/plain") {
		t.Errorf("get_file_info missing mime: %s", info)
	}

	res = call(t, c, "diff_files", map[string]any{"path_a": a, "path_b": b})
	diff := textOf(t, res)
	if !strings.Contains(diff, "-beta") || !strings.Contains(diff, "+BETA") {
		t.Fatalf("diff_files: %s", diff)
	}

	patch := "--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,2 @@\n alpha\n-beta\n+gamma\n"
	res = call(t, c, "apply_patch", map[string]any{"path": a, "patch": patch, "dry_run": true})
	if !strings.Contains(textOf(t, res), "DRY_RUN") {
		t.Fatalf("dry_run: %s", textOf(t, res))
	}
	raw, _ := os.ReadFile(a)
	if string(raw) != "alpha\nbeta\n" {
		t.Fatalf("dry_run wrote disk: %q", raw)
	}

	res = call(t, c, "apply_patch", map[string]any{"path": a, "patch": patch})
	if !strings.Contains(textOf(t, res), "PATCHED") {
		t.Fatalf("apply: %s", textOf(t, res))
	}
	raw, _ = os.ReadFile(a)
	if string(raw) != "alpha\ngamma\n" {
		t.Fatalf("after patch: %q", raw)
	}

	call(t, c, "write_file", map[string]any{"path": a, "content": "tail\n", "mode": "append"})
	raw, _ = os.ReadFile(a)
	if string(raw) != "alpha\ngamma\ntail\n" {
		t.Fatalf("after append: %q", raw)
	}

	secret := callErr(t, c, "read_file", map[string]any{"path": filepath.Join(workDir, ".env")})
	if !strings.Contains(secret, "SECRET") && !strings.Contains(strings.ToLower(secret), "denied") && !strings.Contains(secret, "NOT_ALLOWED") {
		t.Fatalf("secret denial: %s", secret)
	}

	denied := callErr(t, c, "read_file", map[string]any{"path": `C:\Windows\System32\drivers\etc\hosts`})
	if !strings.Contains(denied, "NOT_ALLOWED") {
		t.Fatalf("NOT_ALLOWED envelope: %s", denied)
	}

	ro := startClient(t, exe, "--readonly", workDir)
	blocked := callErr(t, ro, "write_file", map[string]any{"path": a, "content": "nope"})
	if !strings.Contains(blocked, "READ_ONLY") {
		t.Fatalf("readonly: %s", blocked)
	}
	call(t, ro, "list_allowed_directories", map[string]any{})
}

func contentHashOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T", res.StructuredContent)
	}
	h, _ := m["content_hash"].(string)
	if h == "" {
		t.Fatalf("missing content_hash: %#v", m)
	}
	return h
}

func TestE2E_MultiAgent_CTemp(t *testing.T) {
	root := `C:\temp`
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(root, "fsu-multi-agent")
	_ = os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })

	exe := buildServer(t)
	shared := filepath.Join(workDir, "shared.txt")

	writer := startClient(t, exe, workDir)
	reviewer := startClient(t, exe, "--readonly", workDir)
	peer := startClient(t, exe, "--auto-occ", "block", workDir)

	for name, c := range map[string]*client.Client{"writer": writer, "reviewer": reviewer, "peer": peer} {
		got := textOf(t, call(t, c, "list_allowed_directories", map[string]any{}))
		if !strings.Contains(got, workDir) {
			t.Fatalf("%s list_allowed missing workDir: %s", name, got)
		}
	}

	call(t, writer, "write_file", map[string]any{"path": shared, "content": "from-writer\n"})
	got := textOf(t, call(t, reviewer, "read_file", map[string]any{"path": shared}))
	if !strings.Contains(got, "from-writer") {
		t.Fatalf("reviewer did not see writer bytes: %s", got)
	}
	listing := textOf(t, call(t, reviewer, "list_directory", map[string]any{"path": workDir}))
	if !strings.Contains(listing, "shared.txt") {
		t.Fatalf("reviewer list_directory: %s", listing)
	}

	ro := callErr(t, reviewer, "write_file", map[string]any{"path": shared, "content": "reviewer-nope\n"})
	if !strings.Contains(ro, "READ_ONLY") {
		t.Fatalf("reviewer write: %s", ro)
	}
	roEdit := callErr(t, reviewer, "edit_file", map[string]any{"path": shared, "old_text": "from-writer", "new_text": "nope"})
	if !strings.Contains(roEdit, "READ_ONLY") {
		t.Fatalf("reviewer edit: %s", roEdit)
	}

	read := call(t, peer, "read_file", map[string]any{"path": shared})
	hash := contentHashOf(t, read)
	call(t, writer, "write_file", map[string]any{"path": shared, "content": "from-writer-2\n"})

	stale := callErr(t, peer, "edit_file", map[string]any{
		"path": shared, "old_text": "from-writer", "new_text": "peer-clobber", "expected_hash": hash,
	})
	if !strings.Contains(strings.ToLower(stale), "stale") && !strings.Contains(stale, "OCC") && !strings.Contains(stale, "hash") {
		t.Fatalf("expected stale/OCC on expected_hash, got: %s", stale)
	}
	raw, _ := os.ReadFile(shared)
	if string(raw) != "from-writer-2\n" {
		t.Fatalf("peer must not clobber writer: %q", raw)
	}

	blocked := callErr(t, peer, "edit_file", map[string]any{
		"path": shared, "old_text": "from-writer-2", "new_text": "peer-auto",
	})
	if !strings.Contains(blocked, "changed on disk") && !strings.Contains(blocked, "stale") {
		t.Fatalf("auto-occ=block: %s", blocked)
	}
	raw, _ = os.ReadFile(shared)
	if string(raw) != "from-writer-2\n" {
		t.Fatalf("auto-occ must not write: %q", raw)
	}

	fresh := call(t, peer, "read_file", map[string]any{"path": shared})
	body := textOf(t, fresh)
	if !strings.Contains(body, "from-writer-2") {
		t.Fatalf("peer read_file stale after other agent write (cache?): %s", body)
	}
	h2 := contentHashOf(t, fresh)
	ok := call(t, peer, "edit_file", map[string]any{
		"path": shared, "old_text": "from-writer-2", "new_text": "from-peer", "expected_hash": h2,
	})
	if !strings.Contains(textOf(t, ok), "from-peer") && !strings.Contains(textOf(t, ok), "1") {
		t.Logf("peer edit after re-read: %s", textOf(t, ok))
	}
	raw, _ = os.ReadFile(shared)
	if !strings.Contains(string(raw), "from-peer") {
		t.Fatalf("peer edit after re-read failed: %q", raw)
	}

	seen := textOf(t, call(t, writer, "read_file", map[string]any{"path": shared}))
	if !strings.Contains(seen, "from-peer") {
		t.Fatalf("writer did not see peer edit: %s", seen)
	}
}
