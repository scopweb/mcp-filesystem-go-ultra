package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestHardenGitArgv_CallerCannotReenableFsmonitor(t *testing.T) {
	argv := hardenGitArgv([]string{"-c", "core.fsmonitor=./payload", "status", "--porcelain"}, gitSafeConfigPairs(t.TempDir()))
	fs := lastPairValue(argv, "core.fsmonitor")
	if fs != "false" {
		t.Fatalf("core.fsmonitor=%q, want false; argv=%q", fs, argv)
	}
	if indexArg(argv, "status") < indexArg(argv, "-c") {
		t.Fatalf("global -c after subcommand: %q", argv)
	}
	status := indexArg(argv, "status")
	if status < 0 || status+1 >= len(argv) || argv[status+1] != "--ignore-submodules=dirty" {
		t.Fatalf("status hardening missing: %q", argv)
	}
	if !hasArg(argv, "--no-pager") {
		t.Fatalf("missing --no-pager: %q", argv)
	}
}

func TestHardenGitArgv_DiffFlags(t *testing.T) {
	argv := hardenGitArgv([]string{"diff", "--cached"}, gitSafeConfigPairs(t.TempDir()))
	diff := indexArg(argv, "diff")
	if diff < 0 || !hasArg(argv[diff:], "--no-ext-diff") || !hasArg(argv[diff:], "--no-textconv") {
		t.Fatalf("diff flags: %q", argv)
	}
	argv = hardenGitArgv([]string{"-c", "core.hooksPath=.git/hooks", "diff"}, gitSafeConfigPairs("C:/empty-hooks"))
	if lastPairValue(argv, "core.hooksPath") != "C:/empty-hooks" {
		t.Fatalf("hooksPath lost: %q", argv)
	}
}

func lastPairValue(argv []string, key string) string {
	want := key + "="
	got := ""
	found := false
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-c" && strings.HasPrefix(argv[i+1], want) {
			got = strings.TrimPrefix(argv[i+1], want)
			found = true
		}
	}
	if !found {
		return "<missing>"
	}
	return got
}

func TestExecGitCommand_RepoConfigCannotRunPrograms(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran.txt")
	exe := buildMarkerExe(t, marker)
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "t@example.com")
	mustGit(t, dir, "config", "user.name", "T")
	mustGit(t, dir, "config", "core.fsmonitor", exe)
	mustGit(t, dir, "config", "core.pager", exe)
	mustGit(t, dir, "config", "diff.external", exe)
	mustGit(t, dir, "config", "filter.evil.clean", exe)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execGitCommand(dir, "git", "config", "--get", "core.fsmonitor")
	if err != nil {
		t.Fatalf("effective fsmonitor: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "false" {
		t.Fatalf("effective fsmonitor %q, want false", out)
	}
	hooks, err := execGitCommand(dir, "git", "config", "--get", "core.hooksPath")
	if err != nil {
		t.Fatalf("effective hooksPath: %v\n%s", err, hooks)
	}
	hooks = strings.TrimSpace(hooks)
	if hooks == "" || strings.Contains(strings.ToLower(hooks), ".git") {
		t.Fatalf("hooksPath %q still points at the repository", hooks)
	}

	if _, err := execGitCommand(dir, "git", "status", "--porcelain"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if _, err := execGitCommand(dir, "git", "add", "--", "a.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := execGitCommand(dir, "git", "diff", "--", "a.txt"); err != nil {
		t.Fatalf("diff: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository config executed a program (marker %v)", err)
	}
}

func buildMarkerExe(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	prog := "package main\nimport \"os\"\nfunc main() { _ = os.WriteFile(" + strconv.Quote(marker) + ", []byte(\"ran\"), 0o644) }\n"
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "marker")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build marker: %v\n%s", err, b)
	}
	return out
}
