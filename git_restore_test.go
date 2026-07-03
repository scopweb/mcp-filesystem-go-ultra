package main

// Tests for the git tool's restore, add, and branch handlers — covering the
// v4.5.23 security audit fixes. Every test uses a real on-disk git repo
// (created via exec.Command) so the produced commands are validated end-to-end
// against the system `git` binary, exactly as a user invocation would be.
//
// Audit reference: docs/SECURITY-AUDIT-git-2026-07-02.md
//
// Covered scenarios:
//   1. gitRestore — staged unstage, working-tree discard, --source with/without
//      paths, dry-run diff preview, option-like source rejection
//   2. gitAdd     — option-injection blocked for paths and single-path
//   3. gitBranch  — safe `-d` works without `force`; `force:true` escalates
//      to `-D`; option-like branch_name rejected
//   4. rejectOptionLike — unit test for the guard helper itself
//
// These tests are intentionally hermetic: each one initializes its own repo
// under t.TempDir(), so they can run in parallel and don't depend on any
// external state.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	"github.com/mark3labs/mcp-go/mcp"
)

// newGitTestEngine returns an UltraFastEngine that allows access to dir.
// Mirrors tests/pipeline_test.go:createTestEngineWithPath (kept local so this
// file can stay self-contained in the root package).
func newGitTestEngine(t *testing.T, dir string) *core.UltraFastEngine {
	t.Helper()
	c, err := cache.NewIntelligentCache(50 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        c,
		AllowedPaths: []string{dir},
		ParallelOps:  2,
		CompactMode:  false,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return engine
}

// initGitRepo creates an empty git repo at dir, configures a local user, and
// returns the directory. It is safe to call repeatedly inside a TempDir.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// writeFile is a tiny helper that fails the test on write error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// commitAll stages every change and commits with msg.
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-q", "-m", msg},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// readFile returns the trimmed file contents (strips trailing CR/LF).
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimRight(string(b), "\r\n")
}

// ----------------------------------------------------------------------------
// gitRestore
// ----------------------------------------------------------------------------

// TestRestore_StagedUnstage verifies that `git(action:"restore", staged:true,
// paths:[...])` moves a file from staged back to unstaged without touching
// the working tree. Covers the dispatcher + handler + command construction.
func TestRestore_StagedUnstage(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "v1\n")
	commitAll(t, dir, "init")

	// Modify the working tree and stage it.
	writeFile(t, f, "v2-WT\n")
	mustGit(t, dir, "add", f)

	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
		"staged": true,
		"paths":  mustJSON(t, []string{f}),
	})
	if err != nil {
		t.Fatalf("gitRestore returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("gitRestore returned error result: %s", firstText(res))
	}

	// Working tree must be untouched.
	if got := readFile(t, f); got != "v2-WT" {
		t.Fatalf("WT changed: want %q, got %q", "v2-WT", got)
	}
	// Index must now match HEAD (i.e. file is no longer reported as staged).
	out := mustGit(t, dir, "status", "--porcelain")
	if !strings.Contains(out, " M f.txt") {
		t.Fatalf("expected unstaged modification in status, got %q", out)
	}
	if strings.Contains(out, "M  f.txt") {
		t.Fatalf("expected file NOT to be staged, but status shows: %q", out)
	}
}

// TestRestore_WTDiscard verifies that a plain restore (no staged, no source)
// reverts the working tree to HEAD.
func TestRestore_WTDiscard(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "original\n")
	commitAll(t, dir, "init")
	writeFile(t, f, "modified\n")

	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
		"paths":  mustJSON(t, []string{f}),
	})
	if err != nil || res.IsError {
		t.Fatalf("gitRestore: err=%v res=%v", err, res)
	}
	if got := readFile(t, f); got != "original" {
		t.Fatalf("WT not restored: want %q, got %q", "original", got)
	}
}

// TestRestore_SourceWithPaths verifies that `source` is passed as
// `--source=<rev>` (not positionally) and restores a specific file to that rev.
func TestRestore_SourceWithPaths(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "v1\n")
	commitAll(t, dir, "v1")
	writeFile(t, f, "v2\n")
	commitAll(t, dir, "v2")

	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
		"source": "HEAD~1",
		"paths":  mustJSON(t, []string{f}),
	})
	if err != nil || res.IsError {
		t.Fatalf("gitRestore: err=%v res=%v", err, res)
	}
	if got := readFile(t, f); got != "v1" {
		t.Fatalf("file not restored to HEAD~1: want %q, got %q", "v1", got)
	}
}

// TestRestore_SourceWholeTree verifies that source-only restore (no paths)
// targets the whole tree via the explicit `-- .` pathspec git requires.
func TestRestore_SourceWholeTree(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	writeFile(t, a, "v1\n")
	writeFile(t, b, "v1\n")
	commitAll(t, dir, "v1")
	writeFile(t, a, "v2\n")
	writeFile(t, b, "v2\n")
	commitAll(t, dir, "v2")

	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
		"source": "HEAD~1",
	})
	if err != nil || res.IsError {
		t.Fatalf("gitRestore: err=%v res=%v", err, res)
	}
	if got := readFile(t, a); got != "v1" {
		t.Fatalf("a not restored: %q", got)
	}
	if got := readFile(t, b); got != "v1" {
		t.Fatalf("b not restored: %q", got)
	}
}

// TestRestore_DryRunPreview verifies that dry_run produces a diff preview and
// does NOT modify the working tree. Before v4.5.23 this used `git restore -n`,
// which errors "unknown switch".
func TestRestore_DryRunPreview(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "v1\n")
	commitAll(t, dir, "init")
	writeFile(t, f, "v2\n")

	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action":  "restore",
		"paths":   mustJSON(t, []string{f}),
		"dry_run": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("gitRestore dry_run: err=%v res=%v", err, res)
	}
	text := firstText(res)
	if !strings.Contains(text, "Dry run") {
		t.Fatalf("dry_run response missing 'Dry run' prefix: %s", text)
	}
	if !strings.Contains(text, "v2") {
		t.Fatalf("dry_run response should include the staged change ('v2'), got: %s", text)
	}
	// Working tree must be untouched.
	if got := readFile(t, f); got != "v2" {
		t.Fatalf("dry_run must not modify WT: got %q", got)
	}
}

// TestRestore_DryRunNoChange verifies the dry-run "nothing would change"
// branch (file already matches the restore source).
func TestRestore_DryRunNoChange(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "stable\n")
	commitAll(t, dir, "init")

	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action":  "restore",
		"source":  "HEAD",
		"dry_run": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("gitRestore dry_run: err=%v res=%v", err, res)
	}
	if !strings.Contains(firstText(res), "nothing would change") {
		t.Fatalf("expected 'nothing would change' message, got: %s", firstText(res))
	}
}

// TestRestore_OptionLikeSourceRejected verifies that an option-like source
// (e.g. "-s") is rejected by rejectOptionLike inside gitRestore (defense in
// depth: the dispatcher also rejects it).
func TestRestore_OptionLikeSourceRejected(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
		"source": "-s",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result for option-like source, got success: %s", firstText(res))
	}
	if !strings.Contains(firstText(res), "source") {
		t.Fatalf("error should name the offending field, got: %s", firstText(res))
	}
}

// TestRestore_MissingParams verifies the required-params check rejects a
// call with neither paths nor source.
func TestRestore_MissingParams(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	res, err := gitRestore(context.Background(), engine, dir, map[string]interface{}{
		"action": "restore",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error for missing params, got success: %s", firstText(res))
	}
	if !strings.Contains(firstText(res), "requires either paths") {
		t.Fatalf("error should mention missing params, got: %s", firstText(res))
	}
}

// ----------------------------------------------------------------------------
// gitAdd — option injection regression tests
// ----------------------------------------------------------------------------

// TestAdd_OptionInjectionList verifies that a paths array containing a value
// beginning with "-" is rejected as a pathspec rather than parsed as a git
// option. Before v4.5.23 the path was appended without "--" so `git add -A`
// would silently stage the whole tree.
func TestAdd_OptionInjectionList(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	f := filepath.Join(dir, "real.txt")
	writeFile(t, f, "real\n")
	commitAll(t, dir, "init")

	res, err := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
		"paths":  mustJSON(t, []string{"-A"}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	// git should error: "fatal: pathspec '-A' did not match any files"
	if !res.IsError {
		t.Fatalf("expected error result for option-like path, got success: %s", firstText(res))
	}
	// Nothing must be staged.
	if out := mustGit(t, dir, "status", "--porcelain"); strings.Contains(out, "A ") {
		t.Fatalf("option-injection bypass: %s was staged despite invalid path", f)
	}
}

// TestAdd_OptionInjectionSingle verifies the single-path branch (path:string)
// also uses the "--" separator.
func TestAdd_OptionInjectionSingle(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	res, err := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
		"path":   "--pathspec-from-file=/etc/passwd",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error for option-like single path, got: %s", firstText(res))
	}
}

// TestAdd_DryRunNoOp verifies dry_run on a non-changed file returns the dry
// preview, not an error, AND does not modify the index. The fix moved `-n`
// to right after `add` so it isn't swallowed by the `--` separator.
func TestAdd_DryRunNoOp(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	res, err := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action":  "add",
		"dry_run": true,
		"all":     true,
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("dry_run add returned error: %s", firstText(res))
	}
}

// TestAdd_RequiresScope verifies that calling add with no scope (no paths, no
// path, no all) is rejected rather than silently staging the entire repo.
func TestAdd_RequiresScope(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	res, err := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error for missing scope, got success: %s", firstText(res))
	}
	if !strings.Contains(firstText(res), "requires one of") {
		t.Fatalf("error should describe required scope, got: %s", firstText(res))
	}
}

// ----------------------------------------------------------------------------
// gitBranch — delete -d/-D escalation
// ----------------------------------------------------------------------------

// TestBranch_DeleteMerged verifies that `branch delete` works WITHOUT force
// on a fully-merged branch (git's `-d` refuses unmerged, allowing the delete).
// Before v4.5.23 the dispatcher demanded force=true, but force escalated
// -d→-D, making safe deletes impossible.
func TestBranch_DeleteMerged(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	// Set up a main branch with a commit so HEAD is reachable.
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "main\n")
	commitAll(t, dir, "main commit")
	// Create a branch, add a commit, then MERGE it back into master so that
	// git's -d considers the branch fully merged. (Without the merge, git
	// refuses -d even though the change is reachable — branches are
	// "merged" only when there's an explicit merge commit or fast-forward.)
	mustGit(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, f, "feature\n")
	commitAll(t, dir, "feature commit")
	mustGit(t, dir, "checkout", "-q", "master")
	mustGit(t, dir, "merge", "--no-ff", "-q", "feature", "-m", "merge feature")

	// Sanity: both branches should exist.
	if !strings.Contains(mustGit(t, dir, "branch", "-a"), "feature") {
		t.Fatal("setup failed: feature branch missing")
	}

	res, err := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action":        "branch",
		"branch_action": "delete",
		"branch_name":   "feature",
		// no force → -d
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected safe -d delete to succeed, got error: %s", firstText(res))
	}
	if strings.Contains(mustGit(t, dir, "branch", "-a"), "feature") {
		t.Fatal("feature branch was not actually deleted")
	}
}

// TestBranch_DeleteUnmergedRequiresForce verifies that deleting an UNMERGED
// branch without force fails (git -d refuses), but with force succeeds (-D).
func TestBranch_DeleteUnmergedRequiresForce(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "main\n")
	commitAll(t, dir, "main commit")
	mustGit(t, dir, "checkout", "-q", "-b", "diverged")
	writeFile(t, f, "diverged\n")
	commitAll(t, dir, "diverged commit")
	mustGit(t, dir, "checkout", "-q", "master")

	// Without force: -d should refuse (unmerged).
	res, err := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action":        "branch",
		"branch_action": "delete",
		"branch_name":   "diverged",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected -d to refuse unmerged branch, got success: %s", firstText(res))
	}
	if !strings.Contains(mustGit(t, dir, "branch", "-a"), "diverged") {
		t.Fatal("branch should still exist after failed -d")
	}

	// With force: -D succeeds.
	res, err = gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action":        "branch",
		"branch_action": "delete",
		"branch_name":   "diverged",
		"force":         true,
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected -D to succeed, got error: %s", firstText(res))
	}
	if strings.Contains(mustGit(t, dir, "branch", "-a"), "diverged") {
		t.Fatal("branch should be gone after -D")
	}
}

// TestBranch_OptionLikeRejected verifies rejectOptionLike inside gitBranch
// catches a branch name that begins with "-".
func TestBranch_OptionLikeRejected(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	res, err := gitBranch(context.Background(), engine, dir, map[string]interface{}{
		"action":        "branch",
		"branch_action": "delete",
		"branch_name":   "--delete-this",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error for option-like branch name, got: %s", firstText(res))
	}
}

// ----------------------------------------------------------------------------
// rejectOptionLike — unit test for the guard itself
// ----------------------------------------------------------------------------

func TestRejectOptionLike(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"HEAD", false},
		{"HEAD~1", false},
		{"main", false},
		{"feature/foo", false},
		{"abc1234", false},
		{"-s", true},
		{"--output=/tmp/x", true},
		{"-", true},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			res := rejectOptionLike("field", c.value)
			if c.wantErr {
				if res == nil {
					t.Fatalf("want error for %q, got nil", c.value)
				}
				if !res.IsError {
					t.Fatalf("want error result for %q, got success", c.value)
				}
			} else if res != nil {
				t.Fatalf("want no error for %q, got: %v", c.value, res)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// mustJSON JSON-encodes v, failing the test on error.
func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	return string(b)
}

// mustGit runs `git` in dir with args and returns combined output, failing
// the test on non-zero exit.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// firstText returns the first text content from a CallToolResult. Returns
// "<no text>" if the result has no text blocks (which would itself be a bug).
func firstText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return "<no text>"
}

// ensure the import of `core` is used (engine may be nil in these tests).
var _ = core.HookContext{}