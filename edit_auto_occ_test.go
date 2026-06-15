package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// TestEditFile_AutoOCC_WarnsOnExternalChange wires new point 4 end to end:
// read a file (records the known hash), have an external process change it on
// disk, then edit WITHOUT expected_hash. In warn mode the edit still succeeds
// but the response carries an external-change warning.
func TestEditFile_AutoOCC_WarnsOnExternalChange(t *testing.T) {
	core.SetAutoOCCMode("warn")
	defer core.SetAutoOCCMode("warn")

	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "occ.txt")
	if err := os.WriteFile(path, []byte("alpha beta\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 1) Read → records the known hash for this session.
	readReq := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "read_file", Arguments: map[string]interface{}{"path": path},
	}}
	if _, err := reg.readFileHandler(context.Background(), readReq); err != nil {
		t.Fatal(err)
	}

	// 2) External change on disk (different content → different hash).
	if err := os.WriteFile(path, []byte("alpha beta gamma\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3) Edit without expected_hash → auto-OCC should warn (not block).
	editReq := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "edit_file",
		Arguments: map[string]interface{}{"path": path, "old_text": "alpha", "new_text": "ALPHA"},
	}}
	res, err := reg.editFileHandler(context.Background(), editReq)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("warn mode must not block the edit, got error: %v", res.Content)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "changed on disk") {
		t.Errorf("expected an external-change warning in the response text, got:\n%s", text)
	}

	// Finding 1: the warning must also be in the structured payload, not just
	// the text fallback, so structured-only clients see it.
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T, want map[string]any", res.StructuredContent)
	}
	if ec, _ := m["external_change"].(string); ec == "" || !strings.Contains(ec, "changed on disk") {
		t.Errorf("expected 'external_change' in structured payload, got: %#v", m)
	}
}

// TestEditFile_AutoOCC_NoFalsePositiveOnOwnEdits verifies that a session's own
// edit updates the baseline, so a second edit doesn't falsely warn.
func TestEditFile_AutoOCC_NoFalsePositiveOnOwnEdits(t *testing.T) {
	core.SetAutoOCCMode("warn")
	defer core.SetAutoOCCMode("warn")

	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "occ2.txt")
	if err := os.WriteFile(path, []byte("one two three\n"), 0644); err != nil {
		t.Fatal(err)
	}

	readReq := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "read_file", Arguments: map[string]interface{}{"path": path},
	}}
	if _, err := reg.readFileHandler(context.Background(), readReq); err != nil {
		t.Fatal(err)
	}

	edit := func(oldT, newT string) string {
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name:      "edit_file",
			Arguments: map[string]interface{}{"path": path, "old_text": oldT, "new_text": newT},
		}}
		res, err := reg.editFileHandler(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("edit error: %v", res.Content)
		}
		return resultText(t, res)
	}

	// First edit (no external change) and a second edit right after — neither
	// should warn, because our own write updated the known hash.
	if out := edit("one", "ONE"); strings.Contains(out, "changed on disk") {
		t.Errorf("first edit should not warn: %s", out)
	}
	if out := edit("two", "TWO"); strings.Contains(out, "changed on disk") {
		t.Errorf("second consecutive edit should not warn (own write tracked): %s", out)
	}
}
