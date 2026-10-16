package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffFiles_NormalizeEquivalentAndRealDiff(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "preview.txt")
	b := filepath.Join(dir, "resolver.txt")
	beforeA := "order idSub=12 total=9\n"
	beforeB := "order idSub=99 total=9\n"
	if err := os.WriteFile(a, []byte(beforeA), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(beforeB), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	rules := []any{map[string]any{"pattern": `idSub=\d+`, "replacement": "idSub=<ignored>"}}
	res := callPatchTool(t, reg, "diff_files", map[string]any{"path_a": a, "path_b": b, "normalize": rules})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["status"] != "equivalent" || m["identical"] != false {
		t.Fatalf("%#v", m)
	}
	gotA, _ := os.ReadFile(a)
	gotB, _ := os.ReadFile(b)
	if string(gotA) != beforeA || string(gotB) != beforeB {
		t.Fatal("normalize must not modify files")
	}
	if err := os.WriteFile(b, []byte("order idSub=99 total=8\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res = callPatchTool(t, reg, "diff_files", map[string]any{"path_a": a, "path_b": b, "normalize": rules})
	m, _ = res.StructuredContent.(map[string]any)
	if m["equivalent"] != false || m["normalized_diff"] != true {
		t.Fatalf("%#v text=%s", m, resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "not an applicable patch") {
		t.Fatalf("missing label: %s", resultText(t, res))
	}
}
