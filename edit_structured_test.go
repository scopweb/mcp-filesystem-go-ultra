package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestEditFile_ReturnsStructuredContentHash verifies new points 1 + 3 end to
// end: edit_file returns a StructuredContent payload whose content_hash equals
// the hash a fresh read_file would report — so a caller can chain the next
// edit's expected_hash without re-reading the file.
func TestEditFile_ReturnsStructuredContentHash(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha beta gamma\n"), 0644); err != nil {
		t.Fatal(err)
	}

	editReq := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "edit_file",
		Arguments: map[string]interface{}{"path": path, "old_text": "beta", "new_text": "BETA"},
	}}
	res, err := reg.editFileHandler(context.Background(), editReq)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("edit_file error: %v", res.Content)
	}

	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T, want map[string]any", res.StructuredContent)
	}
	postEditHash, _ := m["content_hash"].(string)
	if postEditHash == "" {
		t.Fatalf("edit response missing structured content_hash. Got: %#v", m)
	}
	if _, ok := m["replacements"]; !ok {
		t.Errorf("edit response missing 'replacements' in structured payload: %#v", m)
	}

	// The post-edit hash must equal a fresh read's hash (re-read-free chaining).
	readReq := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "read_file",
		Arguments: map[string]interface{}{"path": path},
	}}
	rres, err := reg.readFileHandler(context.Background(), readReq)
	if err != nil {
		t.Fatal(err)
	}
	rm, _ := rres.StructuredContent.(map[string]any)
	readHash, _ := rm["content_hash"].(string)
	if readHash != postEditHash {
		t.Errorf("post-edit hash %q != read hash %q — chaining expected_hash would require a re-read", postEditHash, readHash)
	}
}
