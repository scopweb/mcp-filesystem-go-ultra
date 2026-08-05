package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Insert mode (feedback 2026-08-05, fricción 3): native anchor-based insert
// that never trips the accidental-rewrite guard and never fuses lines.

func TestInsert_AfterAnchorWithTrailingNewline(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "func a() {\n}\nfunc b() {\n}\n"
	testFile := filepath.Join(tempDir, "insert_after.go")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := engine.InsertAtAnchor(context.Background(), testFile, "func a() {\n}\n", "func inserted() {\n}\n", "after", false)
	if err != nil {
		t.Fatalf("InsertAtAnchor: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "func a() {\n}\nfunc inserted() {\n}\nfunc b() {\n}\n"
	if string(got) != want {
		t.Errorf("insert after mismatch.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestInsert_AfterAnchorWithoutTrailingNewline(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "line1\nANCHOR\nline3\n"
	testFile := filepath.Join(tempDir, "insert_after_nonl.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Anchor without trailing newline; text without trailing newline.
	_, err := engine.InsertAtAnchor(context.Background(), testFile, "ANCHOR", "INSERTED", "after", false)
	if err != nil {
		t.Fatalf("InsertAtAnchor: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "line1\nANCHOR\nINSERTED\nline3\n"
	if string(got) != want {
		t.Errorf("line fusion or missing separation.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestInsert_BeforeAnchor(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "header\nfooter\n"
	testFile := filepath.Join(tempDir, "insert_before.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := engine.InsertAtAnchor(context.Background(), testFile, "footer", "middle", "before", false)
	if err != nil {
		t.Fatalf("InsertAtAnchor: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "header\nmiddle\nfooter\n"
	if string(got) != want {
		t.Errorf("insert before mismatch.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestInsert_CRLF(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "aaa\r\nbbb\r\nccc\r\n"
	testFile := filepath.Join(tempDir, "insert_crlf.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := engine.InsertAtAnchor(context.Background(), testFile, "bbb", "XXX", "after", false)
	if err != nil {
		t.Fatalf("InsertAtAnchor: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "aaa\r\nbbb\r\nXXX\r\nccc\r\n"
	if string(got) != want {
		t.Errorf("CRLF not preserved.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestInsert_AnchorNotFound(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	testFile := filepath.Join(tempDir, "insert_missing.txt")
	if err := os.WriteFile(testFile, []byte("content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := engine.InsertAtAnchor(context.Background(), testFile, "NOPE", "x", "after", false)
	if err == nil || !strings.Contains(err.Error(), "anchor not found") {
		t.Errorf("expected anchor-not-found error, got %v", err)
	}
}

func TestInsert_AmbiguousAnchor(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	testFile := filepath.Join(tempDir, "insert_ambig.txt")
	if err := os.WriteFile(testFile, []byte("}\n}\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := engine.InsertAtAnchor(context.Background(), testFile, "}", "x", "after", false)
	if err == nil || !strings.Contains(err.Error(), "matches 2 times") {
		t.Errorf("expected ambiguous-anchor error, got %v", err)
	}
}

func TestInsert_DryRunDoesNotWrite(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "a\nb\n"
	testFile := filepath.Join(tempDir, "insert_dry.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := engine.InsertAtAnchor(context.Background(), testFile, "a", "X", "after", true)
	if err != nil {
		t.Fatalf("InsertAtAnchor: %v", err)
	}
	if !strings.Contains(result.ModifiedContent, "a\nX\nb\n") {
		t.Errorf("dry-run preview wrong: %q", result.ModifiedContent)
	}

	got, _ := os.ReadFile(testFile)
	if string(got) != content {
		t.Errorf("dry run modified the file: %q", string(got))
	}
}

func TestInsert_InvalidPosition(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	testFile := filepath.Join(tempDir, "insert_pos.txt")
	if err := os.WriteFile(testFile, []byte("a\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := engine.InsertAtAnchor(context.Background(), testFile, "a", "x", "sideways", false)
	if err == nil || !strings.Contains(err.Error(), "position") {
		t.Errorf("expected position validation error, got %v", err)
	}
}
