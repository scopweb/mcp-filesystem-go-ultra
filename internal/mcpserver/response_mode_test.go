package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestReadFile_StructuredModeKeepsBatchBodyOnce(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha-body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("beta-body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ResponseMode = core.ResponseModeStructured
	res := callPatchTool(t, reg, "read_file", map[string]any{"paths": []any{a, b}})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "alpha-body") || strings.Contains(text, "beta-body") {
		t.Fatalf("text fallback repeated bodies:\n%s", text)
	}
	m, _ := res.StructuredContent.(map[string]any)
	if strings.Contains(m["content"].(string), "alpha-body") {
		t.Fatalf("top-level content repeated body: %#v", m["content"])
	}
	files, _ := m["files"].([]map[string]any)
	if len(files) != 2 {
		raw, _ := m["files"].([]any)
		if len(raw) != 2 {
			t.Fatalf("files %#v", m["files"])
		}
		for _, item := range raw {
			fm := item.(map[string]any)
			body, _ := fm["content"].(string)
			if body == "" || fm["content_hash"] == "" {
				t.Fatalf("missing body or hash: %#v", fm)
			}
		}
		return
	}
	for _, fm := range files {
		if fm["content"] == "" || fm["content_hash"] == "" {
			t.Fatalf("%#v", fm)
		}
	}
}

func TestReadFile_CompatKeepsTextBody(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("alpha-body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "read_file", map[string]any{"path": p})
	if !strings.Contains(resultText(t, res), "alpha-body") {
		t.Fatalf("compat dropped body: %s", resultText(t, res))
	}
}
