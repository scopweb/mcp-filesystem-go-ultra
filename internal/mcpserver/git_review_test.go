package mcpserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestExecGitCommand_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := execGitCommandCtx(ctx, t.TempDir(), "git", "status")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestGitReview_HashAndStale(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	call := func(args map[string]interface{}) *mcp.CallToolResult {
		t.Helper()
		args["action"] = "review"
		args["path"] = dir
		res, err := reg.handlers["git"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "git", Arguments: args}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := call(map[string]interface{}{})
	if res.IsError {
		t.Fatal(textBlock(t, res))
	}
	payload, _ := res.StructuredContent.(map[string]any)
	hash, _ := payload["status_hash"].(string)
	if hash == "" || payload["untracked"] == 0 {
		t.Fatalf("%#v", payload)
	}
	again := call(map[string]interface{}{"expected_status_hash": hash})
	if again.IsError {
		t.Fatal(textBlock(t, again))
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := call(map[string]interface{}{"expected_status_hash": hash})
	if !stale.IsError || !strings.Contains(textBlock(t, stale), "STALE_GIT") {
		t.Fatalf("want STALE_GIT, got %s", textBlock(t, stale))
	}
}

func TestGitDiff_OffsetPagesFiles(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt", "b.txt")
	gitRun(t, dir, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res, err := reg.handlers["git"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "git",
		Arguments: map[string]interface{}{
			"action": "diff", "path": dir, "offset": float64(0), "limit": float64(1), "output": "name-only",
		},
	}})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, textBlock(t, res))
	}
	payload, _ := res.StructuredContent.(map[string]any)
	files, _ := payload["files"].([]string)
	if len(files) != 1 || payload["truncated"] != true || payload["continuation"] == nil {
		t.Fatalf("%#v\n%s", payload, textBlock(t, res))
	}
}
