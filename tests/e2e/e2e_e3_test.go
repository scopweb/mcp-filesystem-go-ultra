//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var e3RetryPrefixRe = regexp.MustCompile(`prefix ([0-9a-fA-F]+):`)

func e3JSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func e3File(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Fatalf("%s: got %q (%v), want %q", path, b, err, want)
	}
}

func TestE2E_E3_BatchRollbackRetryAndPipeline(t *testing.T) {
	exe := buildServer(t)
	dir := t.TempDir()
	c := startClient(t, exe, dir)

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("original destination"), 0644); err != nil {
		t.Fatal(err)
	}
	rollback := callErr(t, c, "batch_operations", map[string]any{
		"request_json": e3JSON(t, map[string]any{
			"atomic": true,
			"operations": []map[string]any{
				{"type": "copy", "source": src, "destination": dst},
				{"type": "edit", "path": src, "old_text": "missing", "new_text": "x"},
			},
		}),
	})
	if !strings.Contains(rollback, "Rollback: complete") {
		t.Fatalf("want complete rollback, got:\n%s", rollback)
	}
	e3File(t, dst, "original destination")
	e3File(t, src, "source")

	created := filepath.Join(dir, "newdir")
	createDir := callErr(t, c, "batch_operations", map[string]any{
		"request_json": e3JSON(t, map[string]any{
			"atomic":     true,
			"operations": []map[string]any{{"type": "create_dir", "path": created}},
		}),
	})
	if !strings.Contains(createDir, "atomic create_dir") {
		t.Fatalf("got:\n%s", createDir)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("directory created: %v", err)
	}

	extractSrc := filepath.Join(dir, "lines.txt")
	extractDst := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(extractSrc, []byte("first\nsecond\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extractDst, []byte("prefix\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ops := []map[string]any{{
		"type": "extract", "source": extractSrc, "destination": extractDst,
		"start_line": 1, "end_line": 1, "append": true,
	}}
	probe := callErr(t, c, "batch_operations", map[string]any{
		"request_json": e3JSON(t, map[string]any{
			"retry_contract": "e3-v1", "operation_id": "probe", "atomic": true, "operations": ops,
		}),
	})
	m := e3RetryPrefixRe.FindStringSubmatch(probe)
	if len(m) != 2 {
		t.Fatalf("no retry prefix in:\n%s", probe)
	}
	payload := e3JSON(t, map[string]any{
		"retry_contract": "e3-v1", "operation_id": m[1] + ":append-once", "atomic": true, "operations": ops,
	})
	for i := 0; i < 2; i++ {
		call(t, c, "batch_operations", map[string]any{"request_json": payload})
	}
	e3File(t, extractSrc, "second\n")
	e3File(t, extractDst, "prefix\nfirst\n")

	pipeFile := filepath.Join(dir, "pipe.txt")
	if err := os.WriteFile(pipeFile, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	pipe := callErr(t, c, "batch_operations", map[string]any{
		"pipeline_json": e3JSON(t, map[string]any{
			"name": "nobackup", "stop_on_error": true,
			"steps": []map[string]any{
				{"id": "change", "action": "edit", "params": map[string]any{"files": []string{pipeFile}, "old_text": "old", "new_text": "new"}},
				{"id": "fail", "action": "edit", "params": map[string]any{"files": []string{pipeFile}, "old_text": "missing", "new_text": "x"}},
			},
		}),
	})
	if !strings.Contains(pipe, "Rollback: complete") {
		t.Fatalf("pipeline rollback:\n%s", pipe)
	}
	e3File(t, pipeFile, "old")
}

func TestE2E_E3_RetryMismatchRejected(t *testing.T) {
	exe := buildServer(t)
	dir := t.TempDir()
	c := startClient(t, exe, dir)
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	probe := callErr(t, c, "batch_operations", map[string]any{
		"request_json": e3JSON(t, map[string]any{
			"retry_contract": "e3-v1", "operation_id": "probe",
			"operations": []map[string]any{{"type": "edit", "path": path, "old_text": "v1", "new_text": "v2"}},
		}),
	})
	m := e3RetryPrefixRe.FindStringSubmatch(probe)
	if len(m) != 2 {
		t.Fatalf("no retry prefix in:\n%s", probe)
	}
	id := m[1] + ":once"
	call(t, c, "batch_operations", map[string]any{
		"request_json": e3JSON(t, map[string]any{
			"retry_contract": "e3-v1", "operation_id": id,
			"operations": []map[string]any{{"type": "edit", "path": path, "old_text": "v1", "new_text": "v2"}},
		}),
	})
	e3File(t, path, "v2")
	mismatch := callErr(t, c, "batch_operations", map[string]any{
		"request_json": e3JSON(t, map[string]any{
			"retry_contract": "e3-v1", "operation_id": id,
			"operations": []map[string]any{{"type": "edit", "path": path, "old_text": "v2", "new_text": "v3"}},
		}),
	})
	if !strings.Contains(mismatch, "different arguments") {
		t.Fatalf("got:\n%s", mismatch)
	}
	e3File(t, path, "v2")
}

func TestE2E_E3_CopyRecoveryRemovesDestination(t *testing.T) {
	exe := buildServer(t)
	dir := t.TempDir()
	c := startClient(t, exe, dir)
	src := filepath.Join(dir, "src.txt")
	dstDir := filepath.Join(dir, "out")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatal(err)
	}
	text := callErr(t, c, "batch_operations", map[string]any{
		"pipeline_json": e3JSON(t, map[string]any{
			"name": "copy", "stop_on_error": true, "create_backup": true,
			"steps": []map[string]any{
				{"id": "copy", "action": "copy", "params": map[string]any{"files": []string{src}, "destination": dstDir}},
				{"id": "fail", "action": "edit", "input_from": "copy", "params": map[string]any{"old_text": "missing", "new_text": "x"}},
			},
		}),
	})
	if !strings.Contains(text, "Rollback: complete") {
		t.Fatalf("got:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "src.txt")); !os.IsNotExist(err) {
		t.Fatalf("copy not removed: %v", err)
	}
	e3File(t, src, "data")
}
