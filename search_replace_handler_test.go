package main

// Regression tests for the edit_file handler in search_replace and regex modes.
//
// Bug 1: mode:"search_replace" with dry_run:true silently applied the edit
// because dryRun was never plumbed through to engine.SearchAndReplace.
//
// Bug 2: mode:"regex" rejected pattern+replacement and required patterns_json,
// even though the parameter description claimed pattern worked in both modes.
//
// See conversation log dated 2026-05-08.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// callEditWithName invokes editFileHandler the same way callEdit does.
// (callEdit lives in edit_diff_test.go.)

func TestSearchReplace_DryRun_DoesNotWriteFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "search.txt")
	original := "alpha beta alpha gamma alpha"
	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, true /* compact */)

	result := callEdit(t, reg, map[string]interface{}{
		"path":        file,
		"mode":        "search_replace",
		"pattern":     "alpha",
		"replacement": "DELTA",
		"dry_run":     true,
	})

	text := resultText(t, result)
	t.Logf("dry-run response:\n%s", text)

	if result.IsError {
		t.Fatalf("handler returned error: %s", text)
	}
	if !strings.Contains(text, "DRY RUN") {
		t.Errorf("response must announce DRY RUN, got: %s", text)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read after dry-run: %v", err)
	}
	if string(got) != original {
		t.Errorf("file modified during dry-run!\nexpected: %q\n     got: %q", original, string(got))
	}
}

func TestSearchReplace_NotDryRun_AppliesAndReports(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "search2.txt")
	original := "alpha beta alpha"
	expected := "DELTA beta DELTA"
	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, true /* compact */)

	result := callEdit(t, reg, map[string]interface{}{
		"path":        file,
		"mode":        "search_replace",
		"pattern":     "alpha",
		"replacement": "DELTA",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("handler returned error: %s", text)
	}
	if strings.Contains(text, "DRY RUN") {
		t.Errorf("non-dry-run must NOT say DRY RUN: %s", text)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != expected {
		t.Errorf("expected %q got %q", expected, string(got))
	}
}

// TestRegexMode_SynthesizesPatternsFromPatternAndReplacement verifies that
// mode:"regex" accepts pattern + replacement without requiring the caller to
// pre-build a patterns_json array.
func TestRegexMode_SynthesizesPatternsFromPatternAndReplacement(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regex.txt")
	original := "foo123 foo456 bar789"
	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, false)

	result := callEdit(t, reg, map[string]interface{}{
		"path":        file,
		"mode":        "regex",
		"pattern":     `foo(\d+)`,
		"replacement": "FOO[$1]",
	})

	text := resultText(t, result)
	t.Logf("regex synthesis response:\n%s", text)

	if result.IsError {
		t.Fatalf("handler returned error: %s", text)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	expected := "FOO[123] FOO[456] bar789"
	if string(got) != expected {
		t.Errorf("expected %q got %q", expected, string(got))
	}
}

// TestRegexMode_RequiresPatternOrPatternsJSON ensures we still reject calls
// that provide neither.
func TestRegexMode_RequiresPatternOrPatternsJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regex_empty.txt")
	if err := os.WriteFile(file, []byte("anything"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, false)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "edit_file",
			Arguments: map[string]interface{}{
				"path": file,
				"mode": "regex",
			},
		},
	}
	result, err := reg.editFileHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected error result when neither pattern nor patterns_json is provided")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "patterns_json") && !strings.Contains(text, "pattern") {
		t.Errorf("error message should mention pattern/patterns_json, got: %s", text)
	}
}
