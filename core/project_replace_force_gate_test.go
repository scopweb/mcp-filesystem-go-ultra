package core

// Regression coverage for the project_replace force-gate issue: the tool
// schema and the risk warning both advertised force=true, but the flag was
// never read — HIGH/CRITICAL batches applied silently and the "Use
// force=true to proceed" wording tricked callers into re-running with
// force=true, applying the replacement a SECOND time (e.g. producing
// examples/examples/... paths in 12 files).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectReplace_CriticalRiskBlockedWithoutForce(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	// 1000 occurrences in a single file => CRITICAL risk (>= 1000 replacements)
	path := filepath.Join(dir, "big.txt")
	original := strings.Repeat("foo ", 1000)
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 1) Without force: pure preview, disk untouched.
	blocked, err := engine.ProjectReplace(context.Background(), dir, "foo", "bar", true, true, "", nil, nil, false, false, false, 100, false)
	if err != nil {
		t.Fatalf("ProjectReplace (no force): %v", err)
	}
	if !blocked.Blocked {
		t.Errorf("expected Blocked=true on CRITICAL risk without force, got %+v", blocked)
	}
	if blocked.TotalReplaced != 1000 {
		t.Errorf("blocked preview should report 1000 would-be replacements, got %d", blocked.TotalReplaced)
	}
	if !strings.Contains(blocked.RiskWarning, "no files were modified") {
		t.Errorf("blocked warning must state nothing was written, got: %s", blocked.RiskWarning)
	}
	if got, _ := os.ReadFile(path); string(got) != original {
		t.Errorf("BLOCKED call modified the disk")
	}

	// 2) With force: applies exactly once.
	applied, err := engine.ProjectReplace(context.Background(), dir, "foo", "bar", true, true, "", nil, nil, false, false, false, 100, true)
	if err != nil {
		t.Fatalf("ProjectReplace (force): %v", err)
	}
	if applied.Blocked {
		t.Errorf("force=true must not be blocked")
	}
	if applied.TotalReplaced != 1000 {
		t.Errorf("expected 1000 replacements, got %d", applied.TotalReplaced)
	}
	if !strings.Contains(applied.RiskWarning, "force=true") {
		t.Errorf("applied warning must state it ran with force=true, got: %s", applied.RiskWarning)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "foo") || strings.Count(string(got), "bar") != 1000 {
		t.Errorf("forced replace did not apply correctly")
	}

	// 3) Repeating the same forced call is a no-op — the find string no
	//    longer exists, so there is nothing to double-apply.
	repeat, err := engine.ProjectReplace(context.Background(), dir, "foo", "bar", true, true, "", nil, nil, false, false, false, 100, true)
	if err != nil {
		t.Fatalf("ProjectReplace (repeat): %v", err)
	}
	if repeat.FilesChanged != 0 || repeat.TotalReplaced != 0 {
		t.Errorf("repeat call should be a no-op, got %d files / %d replacements", repeat.FilesChanged, repeat.TotalReplaced)
	}
	if got2, _ := os.ReadFile(path); string(got2) != string(got) {
		t.Errorf("repeat call modified the file")
	}
}

func TestAccessDeniedError_ListsAllowedDirectories(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = []string{`C:\allowed\one`, `D:\allowed\two`}

	err := engine.AccessDeniedError("read", `C:\somewhere\else\file.txt`)
	msg := err.Error()
	if !strings.Contains(msg, "access denied") {
		t.Errorf("message must keep the 'access denied' prefix, got: %s", msg)
	}
	for _, want := range []string{`C:\allowed\one`, `D:\allowed\two`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message must list allowed directory %q, got: %s", want, msg)
		}
	}

	// Open-access mode (no allowed paths): the suffix explains the rejection
	// came from the always-on path security policy instead of listing nothing.
	engine.config.AllowedPaths = nil
	openMsg := engine.AccessDeniedError("read", `\\?\C:\weird`).Error()
	if !strings.Contains(openMsg, "path security policy") {
		t.Errorf("open-access mode must explain the security-policy rejection, got: %s", openMsg)
	}
}
