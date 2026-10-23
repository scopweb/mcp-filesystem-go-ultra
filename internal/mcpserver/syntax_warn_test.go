package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyntaxWarn_BOMValidDoesNotWarn(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "appsettings.json")
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte("{\n  \"name\": \"alpha\"\n}\n")...)
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_text": `"alpha"`, "new_text": `"beta"`,
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if strings.Contains(resultText(t, res), "⚠ syntax") {
		t.Fatalf("valid BOM JSON warned:\n%s", resultText(t, res))
	}
}

func TestSyntaxWarn_PriorJSONCDoesNotBlameEdit(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "appsettings.json")
	body := "{\n  \"name\": \"alpha\" // keep\n}\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_text": `"alpha"`, "new_text": `"beta"`,
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if strings.Contains(resultText(t, res), "after this edit") {
		t.Fatalf("prior JSONC blamed the edit:\n%s", resultText(t, res))
	}
}

func TestSyntaxWarn_EditBreakingStrictJSONIncludesLineCol(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "appsettings.json")
	body := "{\n  \"name\": \"alpha\"\n}\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_text": `"name": "alpha"`, "new_text": `"name": "alpha",`,
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "line ") || !strings.Contains(text, "column ") {
		t.Fatalf("broken strict JSON missing line/column:\n%s", text)
	}
	if !strings.Contains(text, "strict JSON failed") {
		t.Fatalf("trailing comma should be strict JSON failed, not corruption:\n%s", text)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"alpha",`) {
		t.Fatalf("syntax warning rewrote the file: %q", got)
	}

	multiPath := filepath.Join(dir, "other.json")
	if err := os.WriteFile(multiPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	multi := callNamed(t, reg, context.Background(), "multi_edit", map[string]interface{}{
		"path": multiPath,
		"edits": []interface{}{
			map[string]interface{}{"old_text": `"name": "alpha"`, "new_text": `"name": "alpha",`},
		},
	})
	if multi.IsError {
		t.Fatal(resultText(t, multi))
	}
	multiText := resultText(t, multi)
	if !strings.Contains(multiText, "line ") || !strings.Contains(multiText, "column ") {
		t.Fatalf("multi_edit warning missing line/column:\n%s", multiText)
	}
}
