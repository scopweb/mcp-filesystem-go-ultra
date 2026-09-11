package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func e5Maps(v any) []map[string]any {
	if ms, ok := v.([]map[string]any); ok {
		return ms
	}
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func TestE5_ReadStructuredHasPathHashAndTruncation(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "r.txt")
	body := strings.Repeat("line\n", 400)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{"path": path})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["path"] == nil || m["content_hash"] == nil {
		t.Fatalf("missing path/hash: %#v", m)
	}
	if m["truncated"] != true {
		t.Fatalf("auto-truncate must set truncated: %#v", m)
	}
	if m["continuation"] == nil {
		t.Fatal("truncated read must include continuation")
	}
	if resultText(t, res) == "" {
		t.Fatal("text fallback missing")
	}
}

func TestE5_ReadBatchPerFileErrorsNotHidden(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	good := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(good, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"paths": []interface{}{good, missing},
	})
	if res.IsError {
		t.Fatalf("mixed batch must not be a global error: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["status"] != statusPartial {
		t.Fatalf("status=%v", m["status"])
	}
	rawFiles := e5Maps(m["files"])
	if len(rawFiles) != 2 {
		t.Fatalf("files=%#v", m["files"])
	}
	var sawErr, sawOK bool
	for _, f := range rawFiles {
		if _, has := f["error"]; has {
			sawErr = true
		}
		if _, has := f["content"]; has {
			sawOK = true
		}
	}
	if !sawErr || !sawOK {
		t.Fatalf("per-file error hidden: %#v", rawFiles)
	}
}

func TestE5_SearchEmptyIsNotError(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "ZZZ_NO_MATCH", "include_content": true,
	})
	if res.IsError {
		t.Fatalf("empty search must not be isError: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["status"] != statusEmpty || m["match_count"] != 0 || m["truncated"] != false {
		t.Fatalf("%#v", m)
	}
}

func TestE5_SearchMatchesWithoutScrapingText(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "hit.go")
	if err := os.WriteFile(path, []byte("package hit\nfunc Target() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "Target", "include_content": true, "file_types": ".go", "output_format": "json",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["match_count"] == 0 {
		t.Fatalf("expected matches: %#v text=%s", m, resultText(t, res))
	}
	matches := e5Maps(m["matches"])
	if len(matches) == 0 {
		t.Fatalf("structured matches empty (client would have to regex the text): %#v", m)
	}
}

func TestE5_ListDirectoryStructured(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "list_directory", map[string]interface{}{"path": dir})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["truncated"] != false || m["status"] == nil {
		t.Fatalf("%#v", m)
	}
	if m["entries"] == nil {
		t.Fatal("entries missing")
	}
}

func TestE5_BatchItemErrorsNotGlobalSuccess(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request": map[string]interface{}{
			"atomic": true,
			"operations": []interface{}{
				map[string]interface{}{"type": "edit", "path": path, "old_text": "missing", "new_text": "x"},
			},
		},
	})
	if !res.IsError {
		t.Fatal("failed batch must set isError")
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["success"] != false {
		t.Fatalf("%#v", m)
	}
	if m["retryable"] != false {
		t.Fatal("must not recommend retry after a mutation attempt")
	}
	items := e5Maps(m["items"])
	if len(items) == 0 {
		t.Fatalf("per-item results missing: %#v", m)
	}
	mustFile(t, path, "keep")
}

func TestE5_BackupListStructured(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	res := callNamed(t, reg, context.Background(), "backup", map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["action"] != "list" || m["status"] == nil {
		t.Fatalf("%#v", m)
	}
}

func TestE5_EditDryRunStatusSimulated(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "e.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_text": "alpha", "new_text": "beta", "dry_run": true,
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["status"] != statusSimulated {
		t.Fatalf("status=%v", m["status"])
	}
	if m["current_hash"] == nil || m["predicted_hash"] == nil {
		t.Fatalf("dry_run hashes missing: %#v", m)
	}
	mustFile(t, path, "alpha\n")
}

func TestE5_ErrorEnvelopeRetryableFalse(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": filepath.Join(dir, "nope.txt"),
	})
	if !res.IsError {
		t.Fatal("missing file must be isError")
	}
	var env pathErrorEnvelope
	if json.Unmarshal([]byte(resultText(t, res)), &env) != nil {
		t.Fatalf("want JSON envelope, got %s", resultText(t, res))
	}
	if env.Error.Code != errCodeNotFound || env.Error.Retryable {
		t.Fatalf("%+v", env.Error)
	}
}

func TestE5_TruncationAlwaysDetectableOnCappedSearch(t *testing.T) {
	got := capSearchOutput(strings.Repeat("x", 200), newEngineWithCap(t, 50))
	if !contentWasTruncated(got) {
		t.Fatalf("cap marker not detectable: %q", got)
	}
}
