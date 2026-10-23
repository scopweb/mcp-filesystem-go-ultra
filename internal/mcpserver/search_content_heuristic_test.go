package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearch_RegexIsNotAFilenameGlob(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("previewXrange\nfoo123\nReports.xyz\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.go"), []byte("package p\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Reports.dll"), []byte("binary-ish\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("previewXrange\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("previewXrange\n"), 0644); err != nil {
		t.Fatal(err)
	}

	assertScope := func(pattern string, args map[string]interface{}, want string) string {
		t.Helper()
		callArgs := map[string]interface{}{"path": dir, "pattern": pattern}
		for k, v := range args {
			callArgs[k] = v
		}
		res := callNamed(t, reg, context.Background(), "search_files", callArgs)
		if res.IsError {
			t.Fatalf("pattern %q: %s", pattern, resultText(t, res))
		}
		m, ok := res.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("pattern %q: no structured content\n%s", pattern, resultText(t, res))
		}
		msg, _ := m["message"].(string)
		if !strings.Contains(msg, want) {
			t.Fatalf("pattern %q: message %q, want %q\n%s", pattern, msg, want, resultText(t, res))
		}
		return resultText(t, res)
	}

	text := assertScope("preview.*range", nil, "contenido")
	if !strings.Contains(text, "previewXrange") {
		t.Fatalf(".* in the middle did not search content:\n%s", text)
	}
	text = assertScope("foo[0-9]+", nil, "contenido")
	if !strings.Contains(text, "foo123") {
		t.Fatalf("+ did not search content:\n%s", text)
	}
	text = assertScope("*.go", nil, "nombres de fichero")
	if !strings.Contains(text, "keep.go") {
		t.Fatalf("*.go should stay a name glob:\n%s", text)
	}
	text = assertScope("Reports.*", nil, "nombres de fichero")
	if !strings.Contains(text, "Reports.dll") || strings.Contains(text, "notes.txt") {
		t.Fatalf("Reports.* should match the name, not the content:\n%s", text)
	}

	text = assertScope("preview.*range", map[string]interface{}{"include_content": false}, "nombres de fichero")
	if strings.Contains(text, "previewXrange") {
		t.Fatalf("explicit false was overridden:\n%s", text)
	}
	text = assertScope("preview.*range", map[string]interface{}{
		"include_content": false,
		"include":         "*.go",
	}, "nombres de fichero")
	if strings.Contains(text, "previewXrange") {
		t.Fatalf("explicit false was overridden by include:\n%s", text)
	}

	text = assertScope("preview.*range", map[string]interface{}{"include": "*.go"}, "contenido")
	if !strings.Contains(text, "a.go") || strings.Contains(text, "b.txt") {
		t.Fatalf("include + regex should search content in matching files only:\n%s", text)
	}
}

func TestSearch_ContentIntentStillForcesContent(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "Reports.dll"), []byte("not text we need\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("prefix Reports.xyz suffix\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "Reports.*", "context_lines": float64(1),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	msg, _ := m["message"].(string)
	text := resultText(t, res)
	if !strings.Contains(msg, "contenido") || !strings.Contains(text, "Reports.xyz") {
		t.Fatalf("context_lines must still force content, got %q\n%s", msg, text)
	}
}
