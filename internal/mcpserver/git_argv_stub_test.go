package mcpserver

// Stub-argv regression test for execGitCommand (Fase 1 del plan de endurecimiento).
//
// The cmd.exe fallback removed in v4.5.31 re-parsed git arguments through the
// command interpreter — a command-injection vector for commit messages or
// branch names containing &, |, %, ^. This test pins the safe behavior: a fake
// `git` (and a fake `cmd`, on Windows) built from testdata/argvstub records
// every invocation; the test asserts that arguments with shell metacharacters
// reach the git process VERBATIM and that the cmd interpreter is never invoked.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildArgvStub compiles testdata/argvstub under the given binary name into a
// temp dir and returns (stubDir, logFile). The stub appends one JSON array per
// invocation to logFile.
func buildArgvStub(t *testing.T, binNames ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv.log")
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, name := range binNames {
		out := filepath.Join(dir, name+ext)
		cmd := exec.Command("go", "build", "-o", out, "../../testdata/argvstub")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build stub %s: %v\n%s", name, err, b)
		}
	}
	return dir, log
}

// readArgvLog parses the stub log: one JSON argv array per line.
func readArgvLog(t *testing.T, log string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var argv []string
		if err := json.Unmarshal([]byte(line), &argv); err != nil {
			t.Fatalf("stub log line not valid JSON: %q: %v", line, err)
		}
		calls = append(calls, argv)
	}
	return calls
}

// TestExecGitCommand_ArgvPassthroughNoShell verifies that a commit message
// full of cmd.exe metacharacters arrives at the git process byte-identical,
// and that no cmd/cmd.exe invocation is ever attempted.
func TestExecGitCommand_ArgvPassthroughNoShell(t *testing.T) {
	stubDir, log := buildArgvStub(t, "git", "cmd")
	t.Setenv("STUB_ARGV_LOG", log)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	message := `fix: 100% done & dusted | pipe ^ caret <redir> "quoted"`
	out, err := execGitCommand(t.TempDir(), "git", "commit", "-m", message)
	if err != nil {
		t.Fatalf("execGitCommand: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "stub-ok") {
		t.Fatalf("expected stub output, got %q", out)
	}

	calls := readArgvLog(t, log)
	var commit []string
	for _, argv := range calls {
		if hasArg(argv, "commit") {
			commit = argv
			break
		}
	}
	if commit == nil {
		t.Fatalf("no commit invocation in %d calls: %v", len(calls), calls)
	}
	if !hasArg(commit, message) || !hasArg(commit, "-m") {
		t.Fatalf("commit message not verbatim: %q", commit)
	}
	fs := indexArg(commit, "core.fsmonitor=false")
	sub := indexArg(commit, "commit")
	if fs < 0 || sub < 0 || fs > sub {
		t.Fatalf("fsmonitor override missing or after subcommand: %q", commit)
	}
	for _, argv := range calls {
		joined := strings.ToLower(strings.Join(argv, " "))
		if strings.Contains(joined, "cmd.exe") || joined == "cmd" {
			t.Fatalf("shell interpreter invoked: %q", argv)
		}
	}
}
