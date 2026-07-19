package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunRipgrepSearch_PatternFlagInjection is a regression test for a flag
// injection vulnerability: the search pattern used to be appended to the
// ripgrep argv without an `-e` marker, so a pattern starting with '-' was
// parsed as a ripgrep flag. `--pre=<cmd>` in particular makes ripgrep execute
// an arbitrary preprocessor command for every searched file.
//
// The fix passes the pattern as `-e <pattern>` and terminates flag parsing
// with `--` before the path. With that in place, "--pre=..." must be treated
// as a regular expression (matching nothing here) instead of a flag, and
// ripgrep must exit normally without executing anything.
func TestRunRipgrepSearch_PatternFlagInjection(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not available in PATH")
	}

	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	matches, err := engine.RunRipgrepSearch(context.Background(), dir, "--pre=calc", false, false, false, 0)
	if err != nil {
		t.Fatalf("pattern starting with '-' must not break the search: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for literal pattern --pre=calc, got %d", len(matches))
	}
}

// TestRunRipgrepSearch_DashPattern verifies that a legitimate pattern that
// begins with a dash (e.g. a command-line flag searched in source code) is
// found literally.
func TestRunRipgrepSearch_DashPattern(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not available in PATH")
	}

	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("use --verbose flag\n"), 0644); err != nil {
		t.Fatal(err)
	}

	matches, err := engine.RunRipgrepSearch(context.Background(), dir, "--verbose", false, false, false, 0)
	if err != nil {
		t.Fatalf("search for dash-prefixed pattern failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for --verbose, got %d", len(matches))
	}
}

// TestRunRipgrepSearch_SkipDirs verifies that directories listed in
// searchSkipDirs (e.g. node_modules) are excluded from ripgrep results.
// Regression test for the invalid `--ignore <dir>` flag form, which is
// boolean in ripgrep and silently shifted the positional arguments.
func TestRunRipgrepSearch_SkipDirs(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not available in PATH")
	}

	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hit.txt"), []byte("needle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nm := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(nm, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nm, "skip.txt"), []byte("needle\n"), 0644); err != nil {
		t.Fatal(err)
	}

	matches, err := engine.RunRipgrepSearch(context.Background(), dir, "needle", false, false, false, 0)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 match (node_modules excluded), got %d", len(matches))
	}
	if filepath.Base(matches[0].File) != "hit.txt" {
		t.Fatalf("match came from wrong file: %s", matches[0].File)
	}
}

// buildRgStub compiles testdata/argvstub as `rg` and returns (stubDir, logFile).
func buildRgStub(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv.log")
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	out := exec.Command("go", "build", "-o", filepath.Join(dir, "rg"+ext), "../testdata/argvstub")
	if b, err := out.CombinedOutput(); err != nil {
		t.Fatalf("build rg stub: %v\n%s", err, b)
	}
	return dir, log
}

// TestRunRipgrepSearch_ArgvConstruction pins the argv contract: the pattern
// must follow -e (never parsed as a flag), -- must terminate flag parsing
// before the path, and skip-dirs must be expressed as exclusion globs.
// Regression test for the flag-injection (--pre=<cmd>) and the invalid
// --ignore <dir> form fixed in v4.5.31.
func TestRunRipgrepSearch_ArgvConstruction(t *testing.T) {
	stubDir, log := buildRgStub(t)
	t.Setenv("STUB_ARGV_LOG", log)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pattern deliberately hostile: would execute a preprocessor if parsed as a flag.
	if _, err := engine.RunRipgrepSearch(context.Background(), dir, "--pre=calc", false, false, false, 0); err != nil {
		t.Fatalf("RunRipgrepSearch: %v", err)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("stub log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 1 {
		t.Fatalf("expected at least 1 rg invocation, got 0")
	}
	// The engine probes `rg --version` at startup; the search is the LAST call.
	var argv []string
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &argv); err != nil {
		t.Fatalf("stub log line not valid JSON: %v", err)
	}

	joined := strings.Join(argv, " ")

	// 1. Pattern must be the value of -e, never a bare positional/flag.
	eIdx, patIdx, dashIdx, pathIdx := -1, -1, -1, -1
	for i, a := range argv {
		switch a {
		case "-e":
			eIdx = i
		case "--pre=calc":
			patIdx = i
		case "--":
			dashIdx = i
		case dir:
			pathIdx = i
		}
	}
	if eIdx == -1 || patIdx != eIdx+1 {
		t.Errorf("pattern must immediately follow -e; argv: %v", argv)
	}
	// 2. -- must appear after the pattern and before the path.
	if dashIdx == -1 || pathIdx == -1 || !(patIdx < dashIdx && dashIdx < pathIdx) {
		t.Errorf("expected '-e <pattern> -- <path>' ordering; argv: %v", argv)
	}
	// 3. Skip-dirs must be exclusion globs, not the invalid --ignore <dir> form.
	if !strings.Contains(joined, "--glob !**/node_modules/**") {
		t.Errorf("missing skip-dir exclusion glob; argv: %v", argv)
	}
	for _, a := range argv {
		if a == "--ignore" {
			t.Errorf("invalid boolean --ignore flag present; argv: %v", argv)
		}
	}
}
