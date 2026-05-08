package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSearchAndReplace_DryRunDoesNotWrite is a regression test for the bug
// where mode:"search_replace" with dry_run:true silently applied changes
// because the dryRun flag was never plumbed through to the engine.
// See conversation log dated 2026-05-08.
func TestSearchAndReplace_DryRunDoesNotWrite(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	original := "alpha beta alpha gamma alpha"
	testFile := createTestFile(t, tempDir, "dryrun.txt", original)

	ctx := context.Background()

	// Act: dry-run replace
	resp, err := engine.SearchAndReplace(ctx, testFile, "alpha", "DELTA", true, true)
	if err != nil {
		t.Fatalf("SearchAndReplace returned error: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatalf("SearchAndReplace returned empty content")
	}
	respText := resp.Content[0].Text

	// Assert: response indicates dry run
	if !strings.Contains(respText, "DRY RUN") {
		t.Errorf("expected response to contain 'DRY RUN', got:\n%s", respText)
	}

	// Assert: file content unchanged on disk
	got, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}
	if string(got) != original {
		t.Errorf("file was modified during dry-run!\nexpected: %q\n     got: %q", original, string(got))
	}
}

// TestSearchAndReplace_AppliesWhenNotDryRun confirms the non-dry-run path
// still writes correctly after the refactor.
func TestSearchAndReplace_AppliesWhenNotDryRun(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	original := "alpha beta alpha gamma alpha"
	expected := "DELTA beta DELTA gamma DELTA"
	testFile := createTestFile(t, tempDir, "apply.txt", original)

	ctx := context.Background()

	_, err := engine.SearchAndReplace(ctx, testFile, "alpha", "DELTA", true, false)
	if err != nil {
		t.Fatalf("SearchAndReplace returned error: %v", err)
	}

	got, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}
	if string(got) != expected {
		t.Errorf("file content mismatch after non-dry-run replace\nexpected: %q\n     got: %q", expected, string(got))
	}
}

// TestSearchAndReplaceInFile_DryRun verifies the lower-level helper as well,
// since batch_operations.go calls it directly.
func TestSearchAndReplaceInFile_DryRun(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	original := "foo bar foo"
	testFile := filepath.Join(tempDir, "lowlevel.txt")
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	count, err := engine.searchAndReplaceInFile(testFile, "foo", "FOO", true, true)
	if err != nil {
		t.Fatalf("searchAndReplaceInFile returned error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 would-be replacements, got %d", count)
	}

	got, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}
	if string(got) != original {
		t.Errorf("file modified during dry-run!\nexpected: %q\n     got: %q", original, string(got))
	}
}
