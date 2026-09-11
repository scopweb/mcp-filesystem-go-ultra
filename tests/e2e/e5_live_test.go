//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestE2E_E5_CTemp(t *testing.T) {
	root := `C:\temp`
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Skip("C:\\temp not present")
	}
	workDir := filepath.Join(root, "fsu-e5-live")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	runLiveE5(t, workDir)
}

func runLiveE5(t *testing.T, workDir string) {
	t.Helper()
	exe := buildServer(t)
	hit := filepath.Join(workDir, "hit.go")
	if err := os.WriteFile(hit, []byte("package hit\nfunc Target() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := startClient(t, exe, workDir)

	res := call(t, c, "write_file", map[string]any{"path": filepath.Join(workDir, "w.txt"), "content": "hello e5\n"})
	requireStructured(t, "write_file", res, "path", "bytes_written", "verified", "message", "status")
	sc, _ := res.StructuredContent.(map[string]any)
	if sc["status"] != "applied" {
		t.Fatalf("write status=%v", sc["status"])
	}

	res = call(t, c, "read_file", map[string]any{"path": filepath.Join(workDir, "w.txt")})
	requireStructured(t, "read_file", res, "content", "path", "status", "truncated", "content_hash")

	missing := filepath.Join(workDir, "missing.txt")
	res = call(t, c, "read_file", map[string]any{"paths": []any{filepath.Join(workDir, "w.txt"), missing}})
	if res.IsError {
		t.Fatalf("mixed batch read must not be global error: %v", res.Content)
	}
	requireStructured(t, "read_file batch", res, "content", "status", "files")
	sc, _ = res.StructuredContent.(map[string]any)
	if sc["status"] != "partial" {
		t.Fatalf("batch read status=%v", sc["status"])
	}

	res = call(t, c, "search_files", map[string]any{
		"path": workDir, "pattern": "Target", "include_content": true, "file_types": ".go", "output_format": "json",
	})
	requireStructured(t, "search_files", res, "status", "match_count", "truncated", "message")
	sc, _ = res.StructuredContent.(map[string]any)
	if sc["match_count"] == 0 || sc["match_count"] == float64(0) {
		t.Fatalf("search match_count=%v text=%s", sc["match_count"], textOf(t, res))
	}

	res = call(t, c, "search_files", map[string]any{
		"path": workDir, "pattern": "ZZZ_NO_MATCH", "include_content": true,
	})
	if res.IsError {
		t.Fatalf("empty search isError: %v", res.Content)
	}
	requireStructured(t, "search empty", res, "status", "match_count", "truncated")
	sc, _ = res.StructuredContent.(map[string]any)
	if sc["status"] != "empty" {
		t.Fatalf("empty status=%v", sc["status"])
	}

	res = call(t, c, "list_directory", map[string]any{"path": workDir})
	requireStructured(t, "list_directory", res, "path", "status", "truncated", "message")

	res = call(t, c, "edit_file", map[string]any{
		"path": filepath.Join(workDir, "w.txt"), "old_text": "hello", "new_text": "HELLO", "dry_run": true,
	})
	requireStructured(t, "edit dry_run", res, "status", "current_hash", "predicted_hash", "message")
	sc, _ = res.StructuredContent.(map[string]any)
	if sc["status"] != "simulated" {
		t.Fatalf("dry_run status=%v", sc["status"])
	}

	errText := callErr(t, c, "batch_operations", map[string]any{
		"request": map[string]any{
			"atomic": true,
			"operations": []any{
				map[string]any{"type": "edit", "path": filepath.Join(workDir, "w.txt"), "old_text": "missing", "new_text": "x"},
			},
		},
	})
	if errText == "" {
		t.Fatal("failed batch empty error text")
	}

	res = call(t, c, "backup", map[string]any{"action": "list"})
	requireStructured(t, "backup list", res, "status", "action", "message")
}
