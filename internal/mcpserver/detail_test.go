package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func callDetailTool(t *testing.T, reg *toolRegistry, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	if name == "help" {
		return callHelp(t, reg, args)
	}
	return callPatchTool(t, reg, name, args)
}

func TestDirectoryTree_DetailSummary_PathsNoSizes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callDetailTool(t, reg, "directory_tree", map[string]any{"path": dir, "detail": "summary"})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "B") && strings.Contains(text, "a.go  ") {
		t.Fatalf("summary must omit sizes: %s", text)
	}
	sc, _ := res.StructuredContent.(map[string]any)
	if sc["hidden_count"] == nil || sc["truncated"] == nil {
		t.Fatalf("missing truncated/hidden_count: %#v", sc)
	}
	entries, _ := sc["entries"].([]map[string]any)
	if len(entries) == 0 {
		t.Fatalf("entries empty: %#v", sc)
	}
	for _, e := range entries {
		if _, ok := e["size"]; ok {
			t.Fatalf("summary entries must omit size: %#v", e)
		}
	}
}

func TestSearchFiles_DetailSummary_PathsAndCount(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("needle here\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.go"), []byte("needle too\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callDetailTool(t, reg, "search_files", map[string]any{
		"path": dir, "pattern": "needle", "include_content": true, "detail": "summary",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	sc, _ := res.StructuredContent.(map[string]any)
	matches, _ := sc["matches"].([]map[string]any)
	if len(matches) == 0 {
		t.Fatalf("no matches: %#v", sc)
	}
	for _, m := range matches {
		if _, ok := m["text"]; ok {
			t.Fatalf("summary must omit snippets: %#v", m)
		}
		if m["path"] == nil {
			t.Fatalf("path missing: %#v", m)
		}
	}
	if sc["match_count"] == nil {
		t.Fatal("match_count missing")
	}
}

func TestHelp_DetailSummary_NamesOnly(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	res := callHelp(t, reg, map[string]interface{}{"detail": "summary"})
	text := resultText(t, res)
	if strings.Contains(text, "Host project workflow") {
		t.Fatalf("summary must be names only: %s", text)
	}
	if !strings.Contains(text, "read_file") {
		t.Fatalf("missing tool name: %s", text)
	}
	sc, _ := res.StructuredContent.(map[string]any)
	if sc["count"] == nil {
		t.Fatalf("count missing: %#v", sc)
	}
}

func TestHelp_DetailFull_IncludesExamples(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	res := callHelp(t, reg, map[string]interface{}{"detail": "full"})
	text := resultText(t, res)
	if !strings.Contains(text, "## Examples") || !strings.Contains(text, `read_file(path:"file.go")`) {
		t.Fatalf("full catalog missing examples")
	}
}

func TestReadFile_BatchDetailSummary_OmitsBodies(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("secret-body-a"), 0644)
	_ = os.WriteFile(b, []byte("secret-body-b"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callDetailTool(t, reg, "read_file", map[string]any{
		"paths": []any{a, b}, "detail": "summary",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "secret-body") {
		t.Fatalf("summary leaked bodies: %s", text)
	}
	sc, _ := res.StructuredContent.(map[string]any)
	files, _ := sc["files"].([]map[string]any)
	if len(files) != 2 {
		t.Fatalf("files=%#v", sc["files"])
	}
	for _, f := range files {
		if _, ok := f["content"]; ok {
			t.Fatalf("content present: %#v", f)
		}
		if f["content_hash"] == nil {
			t.Fatalf("hash missing: %#v", f)
		}
	}
}

func TestDirectoryTree_DetailInvalid_Validation(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	res := callDetailTool(t, reg, "directory_tree", map[string]any{"path": dir, "detail": "adaptive"})
	if !res.IsError || !strings.Contains(resultText(t, res), `"code":"VALIDATION"`) {
		t.Fatalf("want VALIDATION, got %s", resultText(t, res))
	}
}
