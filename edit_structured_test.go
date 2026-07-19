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

// assertEditStructured verifies the edit_file output-schema contract on a
// result: StructuredContent must be a map containing every required key
// (path, replacements, lines_added, lines_removed, total_lines, message).
// Regression guard for the -32600 "output schema but no structured content"
// failures returned by the search_replace / occurrence / regex code paths,
// which returned plain text results.
func assertEditStructured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("edit_file error: %v", res.Content)
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T, want map[string]any — this path must not return a plain-text result", res.StructuredContent)
	}
	for _, key := range []string{"path", "replacements", "lines_added", "lines_removed", "total_lines", "message"} {
		if _, ok := m[key]; !ok {
			t.Errorf("structured payload missing required key %q: %#v", key, m)
		}
	}
	return m
}

func callEditArgs(t *testing.T, reg *toolRegistry, args map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "edit_file", Arguments: args}}
	res, err := reg.editFileHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

// num coerces a structured-payload numeric field (int in-process, float64
// after a JSON round-trip) to float64 for comparison.
func num(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return -1
}

// TestEditFile_SearchReplaceStructured — mode:"search_replace" must also
// satisfy the output schema (previously returned plain text → client -32600).
func TestEditFile_SearchReplaceStructured(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("foo bar foo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res := callEditArgs(t, reg, map[string]interface{}{
		"path": path, "mode": "search_replace", "pattern": "foo", "replacement": "baz",
	})
	m := assertEditStructured(t, res)
	if got := num(m["replacements"]); got != 2 {
		t.Errorf("replacements = %v, want 2", m["replacements"])
	}
	if h, _ := m["content_hash"].(string); h == "" {
		t.Errorf("search_replace response missing content_hash")
	}
}

// TestEditFile_OccurrenceStructured — occurrence mode must satisfy the output
// schema and report accurate line/hash data (previously plain text + empty stats).
func TestEditFile_OccurrenceStructured(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x x x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res := callEditArgs(t, reg, map[string]interface{}{
		"path": path, "old_text": "x", "new_text": "y", "occurrence": float64(2),
	})
	m := assertEditStructured(t, res)
	if got := num(m["replacements"]); got != 1 {
		t.Errorf("replacements = %v, want 1", m["replacements"])
	}
	if got := num(m["total_lines"]); got != 2 {
		t.Errorf("total_lines = %v, want 2", m["total_lines"])
	}
	if h, _ := m["content_hash"].(string); h == "" {
		t.Errorf("occurrence response missing content_hash")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "x y x\n" {
		t.Errorf("file content = %q, want %q", string(data), "x y x\n")
	}
}

// TestEditFile_RegexStructured — mode:"regex" must also satisfy the output
// schema (previously returned plain text → client -32600).
func TestEditFile_RegexStructured(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res := callEditArgs(t, reg, map[string]interface{}{
		"path": path, "mode": "regex", "pattern": "(hello) (world)", "replacement": "$2 $1",
	})
	m := assertEditStructured(t, res)
	if got := num(m["replacements"]); got != 1 {
		t.Errorf("replacements = %v, want 1", m["replacements"])
	}

	data, _ := os.ReadFile(path)
	if string(data) != "world hello\n" {
		t.Errorf("file content = %q, want %q", string(data), "world hello\n")
	}
}
