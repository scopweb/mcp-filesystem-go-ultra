package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
