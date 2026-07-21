package main

// Regression coverage for the multi_edit "context validation failed" issue:
// edits_json entries using old_string/new_string (Claude Code / rust-variant
// key convention) silently decoded to empty OldText — every edit was skipped
// and the batch failed with "none of the N edits match the current file
// content" even though each string existed verbatim (and single edit_file
// calls with the same strings succeeded).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildEditsJSON(t *testing.T, pairs [][2]string, oldKey, newKey string) string {
	t.Helper()
	edits := make([]map[string]string, 0, len(pairs))
	for _, p := range pairs {
		edits = append(edits, map[string]string{oldKey: p[0], newKey: p[1]})
	}
	raw, err := json.Marshal(edits)
	if err != nil {
		t.Fatalf("marshal edits: %v", err)
	}
	return string(raw)
}

func TestMultiEdit_OldStringAliasOnCRLFFile(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	path := filepath.Join(dir, "CLAUDE.md")
	original := "# Config\r\n\r\n- **Mandatory encryption**: data — at rest\r\n- **Backups**: nightly — offsite\r\n- **Logging**: jsonl — rotated\r\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edits := buildEditsJSON(t, [][2]string{
		{"- **Mandatory encryption**: data — at rest", "- **Mandatory encryption**: data — at rest and in transit"},
		{"- **Backups**: nightly — offsite", "- **Backups**: hourly — offsite"},
		{"- **Logging**: jsonl — rotated", "- **Logging**: jsonl — rotated and shipped"},
	}, "old_string", "new_string")

	result := callMultiEditHandler(t, reg, path, edits, nil)
	if result.IsError {
		t.Fatalf("multi_edit with old_string/new_string failed: %s", resultText(t, result))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	for _, want := range []string{"at rest and in transit", "hourly — offsite", "rotated and shipped"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("expected %q in file after multi_edit, got:\n%s", want, string(got))
		}
	}
	if !strings.Contains(string(got), "\r\n") {
		t.Errorf("CRLF line endings were not preserved: %q", string(got))
	}
}

func TestMultiEdit_ThreeEditsLFFile(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	path := filepath.Join(dir, "notes.md")
	original := "# Notes\n\nalpha one\nbeta two\ngamma three\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edits := buildEditsJSON(t, [][2]string{
		{"alpha one", "ALPHA one"},
		{"beta two", "BETA two"},
		{"gamma three", "GAMMA three"},
	}, "old_text", "new_text")

	result := callMultiEditHandler(t, reg, path, edits, nil)
	if result.IsError {
		t.Fatalf("multi_edit on LF file failed: %s", resultText(t, result))
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{"ALPHA one", "BETA two", "GAMMA three"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("expected %q in file after multi_edit, got:\n%s", want, string(got))
		}
	}
}

func TestMultiEdit_TolerantWhitespaceMixedIndentation(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	path := filepath.Join(dir, "mixed.go")
	original := "func f() {\r\n\tcall(a, b);\r\n\tcall(c, d);\r\n}\r\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// old_text uses 4-space indentation where the file uses a tab, and LF
	// where the file uses CRLF — only tolerant_whitespace reconciles this.
	edits := buildEditsJSON(t, [][2]string{
		{"    call(a, b);", "    call(a, z);"},
		{"    call(c, d);", "    call(c, z);"},
	}, "old_text", "new_text")

	result := callMultiEditHandler(t, reg, path, edits, map[string]any{"tolerant_whitespace": true})
	if result.IsError {
		t.Fatalf("multi_edit with tolerant_whitespace failed: %s", resultText(t, result))
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "call(a, z);") || !strings.Contains(string(got), "call(c, z);") {
		t.Errorf("tolerant edits did not apply, got:\n%s", string(got))
	}
}

func TestMultiEdit_UnrecognizedKeysFailLoud(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// camelCase keys are NOT a supported alias — must produce a clear parse
	// error naming the received keys, never the misleading "context
	// validation failed" message.
	edits := `[{"oldString": "hello", "newString": "goodbye"}]`
	result := callMultiEditHandler(t, reg, path, edits, nil)
	if !result.IsError {
		t.Fatal("expected an error for unrecognized edit keys")
	}
	body := resultText(t, result)
	if !strings.Contains(body, "no recognized old key") {
		t.Errorf("error should name the unrecognized keys, got: %s", body)
	}
	if strings.Contains(body, "context validation failed") {
		t.Errorf("misleading context-validation message must not appear, got: %s", body)
	}
}
