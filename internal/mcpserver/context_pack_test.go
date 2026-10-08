package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestContextPack_FocusRanksAndOmitsSecrets(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\nfunc Alpha() {}\n")
	write("b.go", "package b\nfunc Beta() {}\n")
	write(".env", "PASSWORD=secret\n")
	res, err := reg.handlers["context_pack"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "context_pack",
		Arguments: map[string]interface{}{
			"path": dir, "query": "Beta", "focus_paths": []interface{}{"a.go", ".env"}, "include_git": false,
		},
	}})
	if err != nil || res.IsError {
		t.Fatalf("%v %v", err, res)
	}
	text := textBlock(t, res)
	if !strings.Contains(text, "Alpha") || !strings.Contains(text, "Beta") {
		t.Fatalf("missing symbols:\n%s", text)
	}
	if strings.Contains(text, "PASSWORD") || strings.Contains(text, ".env") {
		t.Fatalf("secret leaked:\n%s", text)
	}
	payload, _ := res.StructuredContent.(map[string]any)
	files, _ := payload["files"].([]map[string]any)
	if len(files) < 2 {
		t.Fatalf("files=%#v", payload["files"])
	}
	first, _ := files[0]["path"].(string)
	if !strings.HasSuffix(first, "a.go") {
		t.Fatalf("focus path should rank first, got %s", first)
	}
	if payload["git_omitted"] != "disabled" {
		t.Fatalf("git_omitted=%v", payload["git_omitted"])
	}
}

func TestContextPack_BudgetTruncates(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for i := 0; i < 8; i++ {
		name := filepath.Join(dir, string(rune('a'+i))+".go")
		body := "package p\nfunc Exported" + strings.Repeat("X", 40) + "() {}\n"
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := reg.handlers["context_pack"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "context_pack",
		Arguments: map[string]interface{}{
			"path": dir, "max_files": float64(8), "budget_chars": float64(500), "include_git": false,
			"focus_paths": []interface{}{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go"},
		},
	}})
	if err != nil || res.IsError {
		t.Fatalf("%v %v", err, res)
	}
	text := textBlock(t, res)
	payload, _ := res.StructuredContent.(map[string]any)
	if len(text) > 600 || payload["truncated"] != true || !strings.Contains(text, "budget_chars") {
		t.Fatalf("truncated=%v len=%d\n%s", payload["truncated"], len(text), text)
	}
}
