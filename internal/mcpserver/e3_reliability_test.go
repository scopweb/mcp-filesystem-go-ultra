package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

var retryPrefixRe = regexp.MustCompile(`prefix ([0-9a-fA-F]+):`)

func callNamed(t *testing.T, reg *toolRegistry, ctx context.Context, name string, args map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	h, ok := reg.handlers[name]
	if !ok {
		t.Fatalf("%s not registered", name)
	}
	res, err := h(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}})
	if err != nil {
		t.Fatalf("%s handler: %v", name, err)
	}
	return res
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustFile(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Fatalf("%s: got %q (%v), want %q", path, b, err, want)
	}
}

func retryPrefixFrom(t *testing.T, text string) string {
	t.Helper()
	m := retryPrefixRe.FindStringSubmatch(text)
	if len(m) != 2 {
		t.Fatalf("no server-lifetime prefix in:\n%s", text)
	}
	return m[1]
}

func TestE3_Handler_BatchRollbackRestoresOverwrittenCopy(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("original destination"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{
			Atomic: true,
			Operations: []core.FileOperation{
				{Type: "copy", Source: src, Destination: dst},
				{Type: "edit", Path: src, OldText: "missing", NewText: "x"},
			},
		}),
	})
	if !res.IsError {
		t.Fatalf("expected tool error, got success: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Rollback: complete") {
		t.Fatalf("want complete rollback, got:\n%s", text)
	}
	mustFile(t, dst, "original destination")
	mustFile(t, src, "source")
}

func TestE3_Handler_AtomicCreateDirRejected(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	created := filepath.Join(dir, "newdir")
	res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{
			Atomic:     true,
			Operations: []core.FileOperation{{Type: "create_dir", Path: created}},
		}),
	})
	if !res.IsError {
		t.Fatal("atomic create_dir must be rejected")
	}
	if !strings.Contains(resultText(t, res), "atomic create_dir") {
		t.Fatalf("got:\n%s", resultText(t, res))
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("directory created despite rejection: %v", err)
	}
}

func TestE3_Handler_PipelineFailureAndCopyRecovery(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	src := filepath.Join(dir, "src.txt")
	dstDir := filepath.Join(dir, "out")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"pipeline_json": mustJSON(t, core.PipelineRequest{
			Name:         "copy-fail",
			StopOnError:  true,
			CreateBackup: true,
			Steps: []core.PipelineStep{
				{ID: "copy", Action: "copy", Params: map[string]interface{}{"files": []string{src}, "destination": dstDir}},
				{ID: "fail", Action: "edit", InputFrom: "copy", Params: map[string]interface{}{"old_text": "missing", "new_text": "x"}},
			},
		}),
	})
	if !res.IsError {
		t.Fatalf("partial pipeline claimed success: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "Success: true") {
		t.Fatalf("partial presented as success:\n%s", text)
	}
	if !strings.Contains(text, "Rollback: complete") {
		t.Fatalf("want complete rollback, got:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "src.txt")); !os.IsNotExist(err) {
		t.Fatalf("copied file not removed: %v", err)
	}
	mustFile(t, src, "data")
}

func TestE3_Handler_PipelineRollbackWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"pipeline_json": mustJSON(t, core.PipelineRequest{
			Name:        "nobackup",
			StopOnError: true,
			Steps: []core.PipelineStep{
				{ID: "change", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "old", "new_text": "new"}},
				{ID: "fail", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "missing", "new_text": "x"}},
			},
		}),
	})
	if !res.IsError {
		t.Fatalf("expected failure, got: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "Rollback: complete") {
		t.Fatalf("got:\n%s", resultText(t, res))
	}
	mustFile(t, path, "old")
}

func TestE3_Handler_RetryDoesNotAppendTwice(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("first\nsecond\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("prefix\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ops := []core.FileOperation{{Type: "extract", Source: src, Destination: dst, StartLine: 1, EndLine: 1, Append: true}}
	probe := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{RetryContract: "e3-v1", OperationID: "probe", Atomic: true, Operations: ops}),
	})
	if !probe.IsError {
		t.Fatal("bare operation_id must be rejected")
	}
	id := retryPrefixFrom(t, resultText(t, probe)) + ":append-once"
	req := core.BatchRequest{RetryContract: "e3-v1", OperationID: id, Atomic: true, Operations: ops}
	payload := mustJSON(t, req)
	for i := 0; i < 2; i++ {
		res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{"request_json": payload})
		if res.IsError {
			t.Fatalf("retry %d: %s", i, resultText(t, res))
		}
	}
	mustFile(t, src, "second\n")
	mustFile(t, dst, "prefix\nfirst\n")
}

func TestE3_Handler_RetryMismatchAndMissingContract(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	missing := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{
			OperationID: "x",
			Operations:  []core.FileOperation{{Type: "edit", Path: path, OldText: "v1", NewText: "v2"}},
		}),
	})
	if !missing.IsError || !strings.Contains(resultText(t, missing), "retry_contract") {
		t.Fatalf("want retry_contract error, got: %s", resultText(t, missing))
	}
	mustFile(t, path, "v1")

	probe := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{
			RetryContract: "e3-v1", OperationID: "probe",
			Operations: []core.FileOperation{{Type: "edit", Path: path, OldText: "v1", NewText: "v2"}},
		}),
	})
	id := retryPrefixFrom(t, resultText(t, probe)) + ":once"
	first := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{
			RetryContract: "e3-v1", OperationID: id,
			Operations: []core.FileOperation{{Type: "edit", Path: path, OldText: "v1", NewText: "v2"}},
		}),
	})
	if first.IsError {
		t.Fatalf("first apply: %s", resultText(t, first))
	}
	mustFile(t, path, "v2")
	mismatch := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{
			RetryContract: "e3-v1", OperationID: id,
			Operations: []core.FileOperation{{Type: "edit", Path: path, OldText: "v2", NewText: "v3"}},
		}),
	})
	if !mismatch.IsError || !strings.Contains(resultText(t, mismatch), "different arguments") {
		t.Fatalf("want mismatch, got: %s", resultText(t, mismatch))
	}
	mustFile(t, path, "v2")
}

func TestE3_Handler_CancelledContextDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := callNamed(t, reg, ctx, "batch_operations", map[string]interface{}{
		"request_json": mustJSON(t, core.BatchRequest{
			Atomic: true,
			Operations: []core.FileOperation{
				{Type: "edit", Path: path, OldText: "keep", NewText: "changed"},
			},
		}),
	})
	if !res.IsError {
		t.Fatalf("cancelled batch succeeded: %s", resultText(t, res))
	}
	mustFile(t, path, "keep")
}

func TestE3_Handler_PipelineRetryDoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	probe := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{
		"pipeline_json": mustJSON(t, core.PipelineRequest{
			RetryContract: "e3-v1", OperationID: "probe", Name: "once", StopOnError: true,
			Steps: []core.PipelineStep{
				{ID: "change", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "old", "new_text": "new"}},
			},
		}),
	})
	if !probe.IsError {
		t.Fatal("bare pipeline operation_id must be rejected")
	}
	id := retryPrefixFrom(t, resultText(t, probe)) + ":pipe-once"
	payload := mustJSON(t, core.PipelineRequest{
		RetryContract: "e3-v1", OperationID: id, Name: "once", StopOnError: true,
		Steps: []core.PipelineStep{
			{ID: "change", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "old", "new_text": "new"}},
		},
	})
	for i := 0; i < 2; i++ {
		res := callNamed(t, reg, context.Background(), "batch_operations", map[string]interface{}{"pipeline_json": payload})
		if res.IsError {
			t.Fatalf("retry %d: %s", i, resultText(t, res))
		}
	}
	mustFile(t, path, "new")
}
