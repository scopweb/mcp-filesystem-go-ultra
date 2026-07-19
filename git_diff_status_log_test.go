package main

// Tests for git tool: status, diff, log, show, add, commit, branch handlers
// — the v4.5.25 upgrade (paths as native array, output enum, 4-layer diff
// guardrail, rev instead of commit_range/source, show action, help).
//
// Helpers (newGitTestEngine, initGitRepo, writeFile, readFile, commitAll)
// live in git_restore_test.go and are reused here.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

// mcpText extracts the first text content from an mcp.CallToolResult.
func mcpText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func newRegisteredGitHandler(engine *core.UltraFastEngine) toolHandler {
	s := server.NewMCPServer("test", "0.0.0")
	reg := &toolRegistry{
		server:   s,
		engine:   engine,
		handlers: make(map[string]toolHandler),
	}
	registerGitTools(reg)
	return reg.handlers["git"]
}

func callRegisteredGit(t *testing.T, handler toolHandler, args map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "git",
			Arguments: args,
		},
	}
	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("git handler: %v", err)
	}
	return result
}

// setupRepoWithFile: init repo + one committed file (f.txt). Returns dir and
// engine. Caller does `defer engine.Close()`.
func setupRepoWithFile(t *testing.T) (string, *core.UltraFastEngine) {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	if err := os.WriteFile(dir+"/f.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	commitAll(t, dir, "init")
	return dir, newGitTestEngine(t, dir)
}

func writeAndCommit(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(dir+"/"+name, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, args := range [][]string{
		{"add", "--", name},
		{"commit", "-q", "-m", msg},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// ----------------------------------------------------------------------------
// usageError helper unit tests
// ----------------------------------------------------------------------------

func TestUsageError_Format(t *testing.T) {
	res := usageError("missing 'action'", `git(action:"status")`)
	if res == nil {
		t.Fatal("nil result")
	}
	text := mcpText(t, res)
	if !strings.HasPrefix(text, "ERROR: missing 'action'") {
		t.Errorf("missing prefix; got: %s", text)
	}
	if !strings.Contains(text, "usage: git(action:\"status\")") {
		t.Errorf("missing usage line; got: %s", text)
	}
}

// ----------------------------------------------------------------------------
// pathsFromArgs unit tests
// ----------------------------------------------------------------------------

func TestPathsFromArgs_NativeArray(t *testing.T) {
	got, errRes := pathsFromArgs(map[string]interface{}{
		"paths": []interface{}{"a.go", "b.go"},
	})
	if errRes != nil {
		t.Fatalf("unexpected error")
	}
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("got %v", got)
	}
}

func TestPathsFromArgs_StringRejected(t *testing.T) {
	_, errRes := pathsFromArgs(map[string]interface{}{
		"paths": `["a.go"]`,
	})
	if errRes == nil {
		t.Fatal("expected usageError for string paths")
	}
	text := mcpText(t, errRes)
	if !strings.Contains(text, "must be a native array") {
		t.Errorf("missing guidance; got: %s", text)
	}
}

func TestPathsFromArgs_Absent(t *testing.T) {
	got, errRes := pathsFromArgs(map[string]interface{}{})
	if errRes != nil || got != nil {
		t.Errorf("expected (nil,nil); got (%v,%+v)", got, errRes)
	}
}

func TestPathsFromArgs_NonStringItem(t *testing.T) {
	_, errRes := pathsFromArgs(map[string]interface{}{
		"paths": []interface{}{"a.go", 42},
	})
	if errRes == nil {
		t.Fatal("expected usageError for non-string item")
	}
}

// ----------------------------------------------------------------------------
// truncateOutput unit tests
// ----------------------------------------------------------------------------

func TestTruncateOutput_NoTruncate(t *testing.T) {
	in := "a\nb\nc"
	if out := truncateOutput(in, 10); out != in {
		t.Errorf("expected no truncation; got %q", out)
	}
}

func TestTruncateOutput_WithFooter(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 250; i++ {
		b.WriteString("line\n")
	}
	out := truncateOutput(b.String(), 200)
	if !strings.HasPrefix(out, "line\nline\n") {
		t.Errorf("expected first 200 lines; got prefix %q", out[:50])
	}
	if !strings.Contains(out, "[TRUNCADO:") {
		t.Errorf("missing TRUNCADO banner")
	}
	if !strings.Contains(out, "output:\"stat\"") {
		t.Errorf("missing hint footer")
	}
}

// ----------------------------------------------------------------------------
// parseOutputArg unit tests
// ----------------------------------------------------------------------------

func TestParseOutputArg_Valid(t *testing.T) {
	v, errRes := parseOutputArg(map[string]interface{}{"output": "full"}, "output", "stat", []string{"stat", "name-only", "full"})
	if errRes != nil {
		t.Fatalf("unexpected error")
	}
	if v != "full" {
		t.Errorf("expected 'full'; got %q", v)
	}
}

func TestParseOutputArg_Default(t *testing.T) {
	v, _ := parseOutputArg(map[string]interface{}{}, "output", "stat", []string{"stat", "full"})
	if v != "stat" {
		t.Errorf("expected default 'stat'; got %q", v)
	}
}

func TestParseOutputArg_Invalid(t *testing.T) {
	_, errRes := parseOutputArg(map[string]interface{}{"output": "garbage"}, "output", "stat", []string{"stat", "full"})
	if errRes == nil {
		t.Fatal("expected usageError for invalid output")
	}
	text := mcpText(t, errRes)
	if !strings.Contains(text, "invalid output") {
		t.Errorf("expected invalid-output guidance; got: %s", text)
	}
}

// ----------------------------------------------------------------------------
// gitDiff tests
// ----------------------------------------------------------------------------

func TestGitToolHandler_FilePathIsImplicitPathspec(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	writeAndCommit(t, dir, "g.txt", "v1\n", "add g")
	writeAndCommit(t, dir, "f.txt", "v2\n", "touch f")
	writeAndCommit(t, dir, "g.txt", "v2\n", "touch g")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("dirty f\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("dirty g\n"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := newRegisteredGitHandler(engine)
	filePath := filepath.Join(dir, "f.txt")

	diffText := mcpText(t, callRegisteredGit(t, handler, map[string]interface{}{
		"action": "diff",
		"path":   filePath,
		"output": "name-only",
	}))
	if !strings.Contains(diffText, "f.txt") || strings.Contains(diffText, "g.txt") {
		t.Fatalf("implicit diff pathspec was not respected: %s", diffText)
	}

	statusText := mcpText(t, callRegisteredGit(t, handler, map[string]interface{}{
		"action": "status",
		"path":   filePath,
	}))
	if !strings.Contains(statusText, "f.txt") || strings.Contains(statusText, "g.txt") {
		t.Fatalf("implicit status pathspec was not respected: %s", statusText)
	}

	logText := mcpText(t, callRegisteredGit(t, handler, map[string]interface{}{
		"action": "log",
		"path":   filePath,
	}))
	if !strings.Contains(logText, "touch f") || strings.Contains(logText, "touch g") {
		t.Fatalf("implicit log pathspec was not respected: %s", logText)
	}
}

func TestGitDiff_DefaultStat(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/f.txt", []byte("v2\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := gitDiff(context.Background(), engine, dir, map[string]interface{}{
		"action": "diff",
	})
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "f.txt") || !strings.Contains(text, "|") {
		t.Errorf("expected stat format; got: %s", text)
	}
}

func TestGitDiff_GuardrailDowngradesFullWhenManyFiles(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("file_%02d.txt", i)
		writeAndCommit(t, dir, name, "v1\n", "many")
	}
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("file_%02d.txt", i)
		if err := os.WriteFile(dir+"/"+name, []byte("v2\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := gitDiff(context.Background(), engine, dir, map[string]interface{}{
		"action": "diff",
		"output": "full",
		// no paths → guardrail L2 fires
	})
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	text := mcpText(t, res)
	if !strings.HasPrefix(text, "[GUARDARRAIL:") {
		t.Errorf("expected GUARDRAIL banner at start; got prefix %q", text[:min(80, len(text))])
	}
	if !strings.Contains(text, "25 archivos modificados") {
		t.Errorf("expected '25 archivos modificados' in banner")
	}
}

func TestGitDiff_GuardrailBypassedWithPaths(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("file_%02d.txt", i)
		writeAndCommit(t, dir, name, "v1\n", "many")
	}
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("file_%02d.txt", i)
		if err := os.WriteFile(dir+"/"+name, []byte("v2\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := gitDiff(context.Background(), engine, dir, map[string]interface{}{
		"action": "diff",
		"output": "full",
		"paths":  []interface{}{"file_00.txt"},
	})
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	text := mcpText(t, res)
	if strings.HasPrefix(text, "[GUARDARRAIL:") {
		t.Errorf("guardrail should NOT fire with explicit paths; got: %s", text[:200])
	}
	if !strings.Contains(text, "file_00.txt") {
		t.Errorf("expected file_00.txt; got: %s", text[:200])
	}
}

func TestGitDiff_Truncation(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	writeAndCommit(t, dir, "big.txt", strings.Repeat("aaaaaaaaaaaaaaaaaaaaaa\n", 500), "big")
	if err := os.WriteFile(dir+"/big.txt", []byte(strings.Repeat("bbbbbbbbbbbbbbbbbbbbbb\n", 500)), 0644); err != nil {
		t.Fatal(err)
	}
	res, _ := gitDiff(context.Background(), engine, dir, map[string]interface{}{
		"action":    "diff",
		"output":    "full",
		"paths":     []interface{}{"big.txt"},
		"max_lines": 50,
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "[TRUNCADO:") {
		t.Errorf("expected TRUNCADO footer; tail=%q", text[max(0, len(text)-200):])
	}
}

func TestGitDiff_RevRange(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	writeAndCommit(t, dir, "f.txt", "v2\n", "second")
	res, err := gitDiff(context.Background(), engine, dir, map[string]interface{}{
		"action": "diff",
		"rev":    "HEAD~1",
		"output": "stat",
	})
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "f.txt") {
		t.Errorf("expected f.txt in diff; got: %s", text)
	}
}

// ----------------------------------------------------------------------------
// gitShow tests
// ----------------------------------------------------------------------------

func TestGitShow_MissingRev(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, err := gitShow(context.Background(), engine, dir, map[string]interface{}{
		"action": "show",
	})
	if err != nil {
		t.Fatalf("gitShow: %v", err)
	}
	text := mcpText(t, res)
	if !strings.HasPrefix(text, "ERROR: missing 'rev'") {
		t.Errorf("expected usageError; got: %s", text)
	}
}

func TestGitShow_DefaultStat(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, err := gitShow(context.Background(), engine, dir, map[string]interface{}{
		"action": "show",
		"rev":    "HEAD",
	})
	if err != nil {
		t.Fatalf("gitShow: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "f.txt") || !strings.Contains(text, "|") {
		t.Errorf("expected stat format; got: %s", text[:min(200, len(text))])
	}
}

// ----------------------------------------------------------------------------
// gitAdd tests
// ----------------------------------------------------------------------------

func TestGitToolHandler_NativePathsReachAdd(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := callRegisteredGit(t, newRegisteredGitHandler(engine), map[string]interface{}{
		"action": "add",
		"path":   dir,
		"paths":  []interface{}{"new.txt"},
	})
	if result.IsError {
		t.Fatalf("native paths rejected before add: %s", mcpText(t, result))
	}
	staged := mustGit(t, dir, "diff", "--cached", "--name-only")
	if strings.TrimSpace(staged) != "new.txt" {
		t.Fatalf("expected only new.txt staged, got %q", staged)
	}
}

func TestGitAdd_NativePathsOK(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/new.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
		"paths":  []interface{}{"new.txt"},
	})
	if err != nil {
		t.Fatalf("gitAdd: %v", err)
	}
	text := mcpText(t, res)
	if text == "" {
		t.Error("empty response")
	}
}

func TestGitAdd_StringPathsRejected(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, _ := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
		"paths":  `["f.txt"]`,
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "must be a native array") {
		t.Errorf("expected native-array guidance; got: %s", text)
	}
}

func TestGitAdd_NoPathsRejected(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, _ := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "git add requires explicit") {
		t.Errorf("expected refusal; got: %s", text)
	}
}

// ----------------------------------------------------------------------------
// gitRestore tests
// ----------------------------------------------------------------------------

func TestGitRestore_NoPathsRejected(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, _ := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "explicit 'paths'") {
		t.Errorf("expected paths-required refusal; got: %s", text)
	}
}

func TestGitRestore_StagedUnstage(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/f.txt", []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("git", "add", "--", "f.txt")
	c.Dir = dir
	if _, err := c.CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
		"staged": true,
		"paths":  []interface{}{"f.txt"},
	})
	if err != nil {
		t.Fatalf("gitRestore: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "Restored") && !strings.Contains(text, "OK") {
		t.Errorf("expected restoration confirmation; got: %s", text)
	}
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = dir
	statusOut, _ := statusCmd.CombinedOutput()
	if !strings.Contains(string(statusOut), " M f.txt") {
		t.Errorf("expected ' M f.txt' in status (unstaged); got: %s", statusOut)
	}
}

// ----------------------------------------------------------------------------
// gitCommit tests
// ----------------------------------------------------------------------------

func TestGitCommit_NothingStaged(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, _ := gitCommit(context.Background(), engine, dir, map[string]interface{}{
		"action":  "commit",
		"message": "should fail",
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "nothing staged") {
		t.Errorf("expected 'nothing staged' message; got: %s", text)
	}
}

func TestGitCommit_MissingMessage(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, _ := gitCommit(context.Background(), engine, dir, map[string]interface{}{
		"action": "commit",
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "missing 'message'") {
		t.Errorf("expected missing-message refusal; got: %s", text)
	}
}

func TestGitCommit_Success(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/new.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
		"paths":  []interface{}{"new.txt"},
	}); err != nil {
		t.Fatalf("gitAdd: %v", err)
	}
	res, err := gitCommit(context.Background(), engine, dir, map[string]interface{}{
		"action":  "commit",
		"message": "add new.txt",
	})
	if err != nil {
		t.Fatalf("gitCommit: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "Commit:") || !strings.Contains(text, "add new.txt") {
		t.Errorf("expected commit summary; got: %s", text)
	}
}

// ----------------------------------------------------------------------------
// gitBranch tests
// ----------------------------------------------------------------------------

func TestGitBranch_List(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, err := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action": "branch",
	})
	if err != nil {
		t.Fatalf("gitBranch: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "main") && !strings.Contains(text, "master") {
		t.Errorf("expected current branch in list; got: %s", text)
	}
}

func TestGitBranch_CreatePlusCheckout(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, err := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action":   "branch",
		"name":     "feature-x",
		"checkout": true,
	})
	if err != nil {
		t.Fatalf("gitBranch: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "feature-x") {
		t.Errorf("expected 'feature-x' in response; got: %s", text)
	}
	revCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	revCmd.Dir = dir
	revOut, _ := revCmd.CombinedOutput()
	if strings.TrimSpace(string(revOut)) != "feature-x" {
		t.Errorf("expected HEAD on feature-x; got: %s", revOut)
	}
}

func TestGitBranch_DeleteForce(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if _, err := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action": "branch",
		"name":   "ephemeral",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action": "branch",
		"name":   "ephemeral",
		"force":  true,
	})
	if err != nil {
		t.Fatalf("delete with force: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "Deleted") && !strings.Contains(text, "OK") {
		t.Errorf("expected delete confirmation; got: %s", text)
	}
}

func TestGitBranch_OptionLikeNameRejected(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, _ := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action": "branch",
		"name":   "-fancy-name",
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "must not start with '-'") {
		t.Errorf("expected option-injection rejection; got: %s", text)
	}
}

// ----------------------------------------------------------------------------
// gitStatus tests
// ----------------------------------------------------------------------------

func TestGitStatusCompact_PorcelainFormatsAreEquivalent(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	v1 := "## main...origin/main\nM  staged.txt\n M modified.txt\n?? new.txt\n"
	v2 := "# branch.oid abc123\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +0 -0\n1 M. N... 100644 100644 100644 abc abc staged.txt\n1 .M N... 100644 100644 100644 abc abc modified.txt\n? new.txt\n"

	v1Result, err := gitStatusCompact(repoRoot, v1)
	if err != nil {
		t.Fatal(err)
	}
	v2Result, err := gitStatusCompact(repoRoot, v2)
	if err != nil {
		t.Fatal(err)
	}
	v1Text, v2Text := mcpText(t, v1Result), mcpText(t, v2Result)
	if v1Text != v2Text {
		t.Fatalf("porcelain summaries differ:\nv1: %s\nv2: %s", v1Text, v2Text)
	}
	if !strings.Contains(v1Text, "(main...origin/main) | +1 ~1 ?1 | dirty") {
		t.Fatalf("unexpected compact summary: %s", v1Text)
	}
}

func TestGitStatus_NameOnlyDefault(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/dirty.txt", []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := gitStatus(context.Background(), engine, dir, map[string]interface{}{
		"action": "status",
	})
	if err != nil {
		t.Fatalf("gitStatus: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "dirty.txt") {
		t.Errorf("expected dirty.txt; got: %s", text)
	}
}

func TestGitStatus_Full(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/dirty.txt", []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, _ := gitStatus(context.Background(), engine, dir, map[string]interface{}{
		"action": "status",
		"output": "full",
	})
	text := mcpText(t, res)
	if text == "" {
		t.Error("empty full status")
	}
}

func TestGitStatus_PathsFilter(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/x.txt", []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/y.txt", []byte("y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, _ := gitStatus(context.Background(), engine, dir, map[string]interface{}{
		"action": "status",
		"paths":  []interface{}{"x.txt"},
	})
	text := mcpText(t, res)
	if !strings.Contains(text, "x.txt") {
		t.Errorf("expected x.txt; got: %s", text)
	}
	if strings.Contains(text, "y.txt") {
		t.Errorf("y.txt should be filtered out; got: %s", text)
	}
}

// ----------------------------------------------------------------------------
// gitLog tests
// ----------------------------------------------------------------------------

func TestGitLog_DefaultOneline(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	res, err := gitLog(context.Background(), engine, dir, map[string]interface{}{
		"action": "log",
	})
	if err != nil {
		t.Fatalf("gitLog: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "init") {
		t.Errorf("expected 'init' commit; got: %s", text)
	}
}

func TestGitLog_LimitAndRev(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	writeAndCommit(t, dir, "g.txt", "x\n", "second")
	writeAndCommit(t, dir, "h.txt", "y\n", "third")
	res, err := gitLog(context.Background(), engine, dir, map[string]interface{}{
		"action": "log",
		"limit":  2,
		"rev":    "HEAD~1",
		"output": "oneline",
	})
	if err != nil {
		t.Fatalf("gitLog: %v", err)
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "second") {
		t.Errorf("expected 'second'; got: %s", text)
	}
	if strings.Contains(text, "third") {
		t.Errorf("'third' should be excluded by limit:2 + rev:HEAD~1; got: %s", text)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
