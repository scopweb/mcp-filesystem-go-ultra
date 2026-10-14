package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func newWorkspaceReg(t *testing.T, allowed, receipt string, readOnly bool) *toolRegistry {
	t.Helper()
	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        c,
		AllowedPaths: []string{allowed},
		ParallelOps:  2,
		ReceiptDir:   receipt,
		ReadOnly:     readOnly,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	reg := &toolRegistry{server: server.NewMCPServer("test", "0.0.0"), engine: engine, handlers: map[string]toolHandler{}}
	registerWorkspaceTools(reg)
	return reg
}

func callWorkspace(t *testing.T, reg *toolRegistry, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := reg.handlers["workspace"](context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "workspace", Arguments: args},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return res
}

func workspaceBody(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &m); err != nil {
		t.Fatalf("json: %v\n%s", err, resultText(t, res))
	}
	return m
}

func gitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestWorkspace_BeginEditExportDiscard(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wsParent := filepath.Join(root, "ws")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wsParent, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "old\n")
	commitAll(t, repo, "base")
	reg := newWorkspaceReg(t, root, filepath.Join(root, "receipts"), false)
	dest := filepath.Join(wsParent, "job")
	began := workspaceBody(t, callWorkspace(t, reg, map[string]any{
		"action": "begin", "source_path": repo, "dest_path": dest,
	}))
	id, _ := began["workspace_id"].(string)
	if id == "" || began["base_commit"] == "" {
		t.Fatalf("begin: %#v", began)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Fatalf("worktree file missing: %v", err)
	}
	writeFile(t, filepath.Join(dest, "a.txt"), "new\n")
	src, err := os.ReadFile(filepath.Join(repo, "a.txt"))
	if err != nil || string(src) != "old\n" {
		t.Fatalf("source changed: %q %v", src, err)
	}
	status := workspaceBody(t, callWorkspace(t, reg, map[string]any{"action": "status", "workspace_id": id}))
	if status["identity_ok"] != true || status["tracked_changes"].(float64) != 1 {
		t.Fatalf("status: %#v", status)
	}
	exported := workspaceBody(t, callWorkspace(t, reg, map[string]any{"action": "export", "workspace_id": id}))
	if exported["complete"] != true || exported["export_id"] == "" {
		t.Fatalf("export: %#v", exported)
	}
	files, _ := exported["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["content"] != "new\n" {
		t.Fatalf("export missing new bytes: %#v", exported["files"])
	}
	dirty := callWorkspace(t, reg, map[string]any{"action": "discard", "workspace_id": id})
	if !dirty.IsError || !strings.Contains(resultText(t, dirty), "WORKSPACE_DIRTY") {
		t.Fatalf("discard dirty: %s", resultText(t, dirty))
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal("dirty discard removed dest")
	}
	gitOK(t, dest, "checkout", "--", "a.txt")
	done := workspaceBody(t, callWorkspace(t, reg, map[string]any{"action": "discard", "workspace_id": id}))
	if done["lifecycle_state"] != "discarded" {
		t.Fatalf("discard: %#v", done)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("dest still present: %v", err)
	}
	again := workspaceBody(t, callWorkspace(t, reg, map[string]any{"action": "discard", "workspace_id": id}))
	if again["already_discarded"] != true {
		t.Fatalf("second discard: %#v", again)
	}
}

func TestWorkspace_RejectsDirtyTrackedAndNonGit(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "v1\n")
	commitAll(t, repo, "base")
	writeFile(t, filepath.Join(repo, "a.txt"), "v2\n")
	reg := newWorkspaceReg(t, root, filepath.Join(root, "receipts"), false)
	dest := filepath.Join(root, "ws", "job")
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatal(err)
	}
	dirty := callWorkspace(t, reg, map[string]any{"action": "begin", "source_path": repo, "dest_path": dest})
	if !dirty.IsError || !strings.Contains(resultText(t, dirty), "WORKSPACE_DIRTY") {
		t.Fatalf("dirty: %s", resultText(t, dirty))
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("dirty begin created dest")
	}
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatal(err)
	}
	ng := callWorkspace(t, reg, map[string]any{"action": "begin", "source_path": plain, "dest_path": filepath.Join(root, "ws", "other")})
	if !ng.IsError || !strings.Contains(resultText(t, ng), "WORKSPACE_NOT_GIT") {
		t.Fatalf("non-git: %s", resultText(t, ng))
	}
}

func TestWorkspace_DoesNotWidenRoots(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	outside := t.TempDir()
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "v\n")
	commitAll(t, repo, "base")
	reg := newWorkspaceReg(t, repo, filepath.Join(root, "receipts"), false)
	dest := filepath.Join(outside, "job")
	res := callWorkspace(t, reg, map[string]any{"action": "begin", "source_path": repo, "dest_path": dest})
	if !res.IsError || !strings.Contains(resultText(t, res), "NOT_ALLOWED") {
		t.Fatalf("outside: %s", resultText(t, res))
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("unauthorized dest created")
	}
}

func TestWorkspace_ReadOnlyAndPolicyBlockBegin(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(root, "ws"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "v\n")
	commitAll(t, repo, "base")
	ro := newWorkspaceReg(t, root, filepath.Join(root, "receipts"), true)
	res := callWorkspace(t, ro, map[string]any{
		"action": "begin", "source_path": repo, "dest_path": filepath.Join(root, "ws", "job"),
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "READONLY") {
		t.Fatalf("readonly: %s", resultText(t, res))
	}
	status := callWorkspace(t, ro, map[string]any{"action": "status", "workspace_id": "missing"})
	if !status.IsError || !strings.Contains(resultText(t, status), "NOT_FOUND") {
		t.Fatalf("readonly status should still read: %s", resultText(t, status))
	}
	pol := newWorkspaceReg(t, root, filepath.Join(root, "receipts2"), false)
	cfg := filepath.Join(root, "policy.json")
	writeFile(t, cfg, `{"version":1,"rules":[{"pattern":"*.ini","level":"protected"}]}`)
	policy, err := core.LoadFilePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pol.engine.SetFilePolicy(policy)
	blocked := callWorkspace(t, pol, map[string]any{
		"action": "status", "workspace_id": "x",
	})
	if !blocked.IsError || !strings.Contains(resultText(t, blocked), "git is unavailable") {
		t.Fatalf("policy: %s", resultText(t, blocked))
	}
}

func TestWorkspace_RejectsSymlinkModeAndDeferredActions(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(root, "ws"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "v\n")
	commitAll(t, repo, "base")
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader("target\n")
	blob, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	gitOK(t, repo, "config", "core.symlinks", "false")
	gitOK(t, repo, "update-index", "--add", "--cacheinfo", "120000,"+strings.TrimSpace(string(blob))+",link")
	gitOK(t, repo, "commit", "-q", "-m", "link")
	gitOK(t, repo, "reset", "--hard", "HEAD")
	reg := newWorkspaceReg(t, root, filepath.Join(root, "receipts"), false)
	res := callWorkspace(t, reg, map[string]any{
		"action": "begin", "source_path": repo, "dest_path": filepath.Join(root, "ws", "job"),
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "symlink") {
		t.Fatalf("symlink: %s", resultText(t, res))
	}
	deferred := callWorkspace(t, reg, map[string]any{"action": "promote"})
	if !deferred.IsError || !strings.Contains(resultText(t, deferred), "promote") {
		t.Fatalf("promote: %s", resultText(t, deferred))
	}
}

func TestWorkspace_ExportTooLargeIsNotComplete(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(root, "ws"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "v\n")
	commitAll(t, repo, "base")
	reg := newWorkspaceReg(t, root, filepath.Join(root, "receipts"), false)
	dest := filepath.Join(root, "ws", "job")
	began := workspaceBody(t, callWorkspace(t, reg, map[string]any{
		"action": "begin", "source_path": repo, "dest_path": dest,
	}))
	writeFile(t, filepath.Join(dest, "a.txt"), strings.Repeat("x", 80))
	old := workspaceExportCap
	workspaceExportCap = 40
	t.Cleanup(func() { workspaceExportCap = old })
	res := callWorkspace(t, reg, map[string]any{"action": "export", "workspace_id": began["workspace_id"]})
	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, "EXPORT_TOO_LARGE") || strings.Contains(text, `"complete":true`) {
		t.Fatalf("export cap: %s", text)
	}
}

func TestWorkspace_RegistrySurvivesNewEngine(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(root, "ws"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "v\n")
	commitAll(t, repo, "base")
	receipt := filepath.Join(root, "receipts")
	reg := newWorkspaceReg(t, root, receipt, false)
	dest := filepath.Join(root, "ws", "job")
	began := workspaceBody(t, callWorkspace(t, reg, map[string]any{
		"action": "begin", "source_path": repo, "dest_path": dest,
	}))
	reg2 := newWorkspaceReg(t, root, receipt, false)
	status := workspaceBody(t, callWorkspace(t, reg2, map[string]any{
		"action": "status", "workspace_id": began["workspace_id"],
	}))
	if status["identity_ok"] != true {
		t.Fatalf("reloaded status: %#v", status)
	}
}
