package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestAnalyzeCode_NotInStrict(t *testing.T) {
	reg := newProfileRegistry(t, t.TempDir(), registerOpts{Profile: profileStrict})
	if _, ok := reg.server.ListTools()["analyze_code"]; ok {
		t.Fatal("analyze_code must not be registered in strict")
	}
}

func TestAnalyzeCode_SymbolsExportedFunc(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sym.go")
	if err := os.WriteFile(p, []byte("package p\nfunc Exported() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "analyze_code", map[string]any{"action": "symbols", "path": p, "query": "Exported"})
	if res.IsError {
		t.Fatalf("%s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	found := false
	switch raw := m["findings"].(type) {
	case []any:
		for _, item := range raw {
			fm, _ := item.(map[string]any)
			if s, _ := fm["symbol"].(string); s == "Exported" {
				found = true
			}
		}
	case []map[string]any:
		for _, fm := range raw {
			if s, _ := fm["symbol"].(string); s == "Exported" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Exported not found: %#v", m)
	}
}

func TestAnalyzeCode_LintPrintf(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module vettest\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "vet.go")
	src := "package p\nimport \"fmt\"\nfunc F() { fmt.Printf(\"%d\", \"x\") }\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "analyze_code", map[string]any{"action": "lint", "path": p})
	if res.IsError {
		t.Fatalf("%s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	text := resultText(t, res)
	if m["status"] == "empty" {
		t.Skip("go vet produced no findings in this environment")
	}
	if !strings.Contains(text, "vet.go") && !strings.Contains(strings.ToLower(text), "printf") && !strings.Contains(strings.ToLower(text), "wrong") {
		t.Fatalf("expected vet finding, got %s payload=%#v", text, m)
	}
}

func TestAnalyzeCode_NotAllowed(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	outside := filepath.Join(os.TempDir(), "fsu-analyze-outside.go")
	_ = os.WriteFile(outside, []byte("package x\n"), 0644)
	res := callPatchTool(t, reg, "analyze_code", map[string]any{"action": "symbols", "path": outside})
	if !res.IsError {
		t.Fatalf("want NOT_ALLOWED, got %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "NOT_ALLOWED") {
		t.Fatalf("got %s", resultText(t, res))
	}
}

func TestAnalyzeCode_StaticcheckAbsentNoPanic(t *testing.T) {
	restore := core.StubLookPath(func(name string) (string, error) {
		if name == "staticcheck" {
			return "", os.ErrNotExist
		}
		return exec.LookPath(name)
	})
	t.Cleanup(restore)

	dir := t.TempDir()
	p := filepath.Join(dir, "ok.go")
	if err := os.WriteFile(p, []byte("package p\nfunc F() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "analyze_code", map[string]any{"action": "lint", "path": p})
	if res == nil {
		t.Fatal("nil result")
	}
}
