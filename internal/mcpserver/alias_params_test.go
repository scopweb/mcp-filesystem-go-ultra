package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

// A model that sends old_string/new_string must get the edit applied, not a
// validation error: the alias is renamed to the canonical parameter names
// before validation runs, so the call succeeds on the first attempt.
func TestEditFileAcceptsOldStringAlias(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callEdit(t, reg, map[string]interface{}{
		"path":       path,
		"old_string": "world",
		"new_string": "there",
	})
	if res.IsError {
		t.Fatalf("edit_file with old_string: %s", resultText(t, res))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello there" {
		t.Fatalf("alias edit not applied: %q", got)
	}
}

// The same alias inside each item of a multi_edit batch.
func TestMultiEditAcceptsOldStringAlias(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	path := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callMultiEditHandler(t, reg, path, `[{"old_string":"world","new_string":"there"}]`, map[string]any{})
	if res.IsError {
		t.Fatalf("multi_edit with old_string: %s", resultText(t, res))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello there" {
		t.Fatalf("alias edit not applied: %q", got)
	}
}
