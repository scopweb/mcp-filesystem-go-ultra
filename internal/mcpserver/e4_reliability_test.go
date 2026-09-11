package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

)

func TestE4_NativePathsEquivalentToJSON(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("B"), 0644); err != nil {
		t.Fatal(err)
	}
	jsonRes := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"paths": `["` + filepath.ToSlash(a) + `","` + filepath.ToSlash(b) + `"]`,
	})
	nativeRes := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"paths": []interface{}{a, b},
	})
	if jsonRes.IsError || nativeRes.IsError {
		t.Fatalf("json=%s native=%s", resultText(t, jsonRes), resultText(t, nativeRes))
	}
	if resultText(t, jsonRes) != resultText(t, nativeRes) {
		t.Fatalf("mismatch\njson:\n%s\nnative:\n%s", resultText(t, jsonRes), resultText(t, nativeRes))
	}
}

func TestE4_NativeEditsEquivalentToJSON(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "e.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	jsonRes := callNamed(t, reg, context.Background(), "multi_edit", map[string]interface{}{
		"path": path, "dry_run": true,
		"edits_json": `[{"old_text":"alpha","new_text":"ALPHA"}]`,
	})
	if jsonRes.IsError {
		t.Fatal(resultText(t, jsonRes))
	}
	nativeRes := callNamed(t, reg, context.Background(), "multi_edit", map[string]interface{}{
		"path": path, "dry_run": true,
		"edits": []interface{}{map[string]interface{}{"old_text": "alpha", "new_text": "ALPHA"}},
	})
	if nativeRes.IsError {
		t.Fatal(resultText(t, nativeRes))
	}
	if !strings.Contains(resultText(t, nativeRes), "ALPHA") && !strings.Contains(resultText(t, nativeRes), "dry") && !strings.Contains(strings.ToUpper(resultText(t, nativeRes)), "DRY") {
		t.Fatalf("native dry_run unexpected:\n%s", resultText(t, nativeRes))
	}
}

func TestE4_ConflictingEditsRejected(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "e.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "multi_edit", map[string]interface{}{
		"path":       path,
		"edits_json": `[{"old_text":"alpha","new_text":"A"}]`,
		"edits":      []interface{}{map[string]interface{}{"old_text": "alpha", "new_text": "B"}},
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "conflicting") {
		t.Fatalf("got: %s", resultText(t, res))
	}
	mustFile(t, path, "alpha\n")
}

func TestE4_InvalidModeNotSilent(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "e.txt")
	if err := os.WriteFile(path, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "mode": "typo", "old_text": "keep", "new_text": "gone",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "invalid") {
		t.Fatalf("got: %s", resultText(t, res))
	}
	mustFile(t, path, "keep")
}

func TestE4_StrictNoFallbackAndExpectedMatches(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "e.txt")
	if err := os.WriteFile(path, []byte("  hello\nhello\nhello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	trimmed := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_text": "hello", "new_text": "hi", "strict": true, "expected_matches": float64(1),
	})
	if !trimmed.IsError || !strings.Contains(resultText(t, trimmed), "expected 1") {
		t.Fatalf("want expected_matches error, got: %s", resultText(t, trimmed))
	}
	mustFile(t, path, "  hello\nhello\nhello\n")

	ok := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_text": "hello", "new_text": "hi", "expected_matches": float64(3),
	})
	if ok.IsError {
		t.Fatal(resultText(t, ok))
	}
	mustFile(t, path, "  hi\nhi\nhi\n")
	m, _ := ok.StructuredContent.(map[string]any)
	if m["match_method"] != "exact" {
		t.Fatalf("match_method=%v", m["match_method"])
	}
}

func TestE4_NativeBatchRequest(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "w.txt")
	res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request": map[string]interface{}{
			"atomic": true,
			"operations": []interface{}{
				map[string]interface{}{"type": "write", "path": path, "content": "ok"},
			},
		},
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	mustFile(t, path, "ok")
}

func TestE4_HelpExamplesNativeParse(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	res := callHelp(t, reg, map[string]interface{}{"tool": "multi_edit"})
	text := resultText(t, res)
	if !strings.Contains(text, "edits") {
		t.Fatalf("help(multi_edit) missing edits:\n%s", text)
	}
}
