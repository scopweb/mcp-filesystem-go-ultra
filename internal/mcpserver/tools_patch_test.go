package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func callPatchTool(t *testing.T, reg *toolRegistry, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	return callPatchToolCtx(t, context.Background(), reg, name, args)
}

func callPatchToolCtx(t *testing.T, ctx context.Context, reg *toolRegistry, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	h, ok := reg.handlers[name]
	if !ok {
		t.Fatalf("%s not registered", name)
	}
	res, err := h(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDirectoryTree_StructuredHiddenCount(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret.txt\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "ok.go"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "directory_tree", map[string]any{"path": dir})
	if res.IsError {
		t.Fatalf("%s", resultText(t, res))
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent %T", res.StructuredContent)
	}
	if m["truncated"] == nil {
		t.Fatal("truncated must be explicit")
	}
	hc, ok := m["hidden_count"].(int)
	if !ok {
		if f, ok := m["hidden_count"].(float64); ok {
			hc = int(f)
		} else {
			t.Fatalf("hidden_count type %T", m["hidden_count"])
		}
	}
	if hc < 1 {
		t.Fatalf("hidden_count=%d want >=1 payload=%#v", hc, m)
	}
	if strings.Contains(resultText(t, res), "secret.txt") {
		t.Fatalf("tree should hide secret.txt:\n%s", resultText(t, res))
	}
}

func TestDiffFiles_IdenticalAndChanged(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("same\n"), 0644)
	_ = os.WriteFile(b, []byte("same\n"), 0644)
	reg := newHelpTestRegistry(t, dir)

	res := callPatchTool(t, reg, "diff_files", map[string]any{"path_a": a, "path_b": b})
	if res.IsError || !strings.Contains(resultText(t, res), "identical") {
		t.Fatalf("got %v", resultText(t, res))
	}
	_ = os.WriteFile(b, []byte("other\n"), 0644)
	res = callPatchTool(t, reg, "diff_files", map[string]any{"path_a": a, "path_b": b})
	text := resultText(t, res)
	if res.IsError || !strings.Contains(text, "-same") || !strings.Contains(text, "+other") {
		t.Fatalf("got %s", text)
	}
}

func TestApplyPatch_DryRunAndApply(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("alpha\nbeta\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n alpha\n-beta\n+BETA\n"
	res := callPatchTool(t, reg, "apply_patch", map[string]any{"path": p, "patch": patch, "dry_run": true})
	if res.IsError || !strings.Contains(resultText(t, res), "DRY_RUN") {
		t.Fatalf("dry_run: %s", resultText(t, res))
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != "alpha\nbeta\n" {
		t.Fatal("dry_run must not write")
	}
	res = callPatchTool(t, reg, "apply_patch", map[string]any{"path": p, "patch": patch})
	if res.IsError {
		t.Fatalf("apply: %s", resultText(t, res))
	}
	raw, _ = os.ReadFile(p)
	if string(raw) != "alpha\nBETA\n" {
		t.Fatalf("got %q", raw)
	}
}

func TestApplyPatch_CRLFFileLFPatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "crlf.txt")
	if err := os.WriteFile(p, []byte("alpha\r\nbeta\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/crlf.txt\n+++ b/crlf.txt\n@@ -1,2 +1,2 @@\n alpha\n-beta\n+BETA\n"
	res := callPatchTool(t, reg, "apply_patch", map[string]any{"path": p, "patch": patch})
	if res.IsError {
		t.Fatalf("apply: %s", resultText(t, res))
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != "alpha\r\nBETA\r\n" {
		t.Fatalf("destination EOL must win, got %q", raw)
	}
}

func TestApplyPatch_OCCMismatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("x\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1 +1 @@\n-x\n+y\n"
	res := callPatchTool(t, reg, "apply_patch", map[string]any{
		"path": p, "patch": patch, "expected_hash": "deadbeef",
	})
	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, `"code":"OCC_MISMATCH"`) || !strings.Contains(text, `"retryable":true`) {
		t.Fatalf("got %s", text)
	}
}

func TestApplyPatch_PathEscapeHeader(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("x\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/other.txt\n+++ b/other.txt\n@@ -1 +1 @@\n-x\n+y\n"
	res := callPatchTool(t, reg, "apply_patch", map[string]any{"path": p, "patch": patch})
	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, `"code":"PATCH_FAILED"`) || !strings.Contains(text, `"retryable":false`) {
		t.Fatalf("got %s", text)
	}
}

func TestApplyPatch_ContextMismatch_ReleasesLock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "patch.txt")
	if err := os.WriteFile(p, []byte("real line\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/patch.txt\n+++ b/patch.txt\n@@ -1,1 +1,1 @@\n-WRONG CONTEXT\n+patched line\n"

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res := callPatchToolCtx(t, ctx, reg, "apply_patch", map[string]any{"path": p, "patch": patch})
	if !res.IsError {
		t.Fatalf("expected hunk mismatch error, got %s", resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(strings.ToLower(text), "new file") || strings.Contains(text, "PATCHED") {
		t.Fatalf("mismatch must not create/rewrite: %s", text)
	}
	if !strings.Contains(text, `"code":"PATCH_FAILED"`) || !strings.Contains(text, `"retryable":false`) {
		t.Fatalf("want PATCH_FAILED retryable=false, got %s", text)
	}
	if !strings.Contains(text, `"reason":"context_not_found"`) {
		t.Fatalf("want context_not_found, got %s", text)
	}
	if !strings.Contains(text, `"suggestion":"re-read the file and regenerate the patch"`) {
		t.Fatalf("want stable suggestion, got %s", text)
	}

	editCtx, editCancel := context.WithTimeout(context.Background(), time.Second)
	defer editCancel()
	h, ok := reg.handlers["edit_file"]
	if !ok {
		t.Fatal("edit_file not registered")
	}
	editRes, err := h(editCtx, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "edit_file",
		Arguments: map[string]any{"path": p, "old_text": "real line", "new_text": "edited"},
	}})
	if err != nil {
		t.Fatalf("edit after mismatch (lock held?): %v", err)
	}
	if editRes.IsError {
		t.Fatalf("edit_file after mismatch must succeed (lock released): %s", resultText(t, editRes))
	}
}

func TestApplyPatch_MissingFile_NotNewFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "missing.txt")
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/missing.txt\n+++ b/missing.txt\n@@ -1 +1 @@\n-x\n+y\n"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res := callPatchToolCtx(t, ctx, reg, "apply_patch", map[string]any{"path": p, "patch": patch})
	if !res.IsError {
		t.Fatalf("missing file without /dev/null must error, got %s", resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(strings.ToLower(text), "new file") || strings.Contains(text, "PATCHED") {
		t.Fatalf("must not treat missing as new file: %s", text)
	}
}

func TestApplyPatch_NewFileFromDevNull(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "created.txt")
	reg := newHelpTestRegistry(t, dir)
	patch := "--- /dev/null\n+++ b/created.txt\n@@ -0,0 +1 @@\n+hello\n"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res := callPatchToolCtx(t, ctx, reg, "apply_patch", map[string]any{"path": p, "patch": patch})
	if res.IsError {
		t.Fatalf("dev/null new file: %s", resultText(t, res))
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("got %q", raw)
	}
}

func twoFilePatch(aOld, aNew, bOld, bNew string) string {
	return "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-" + aOld + "\n+" + aNew + "\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-" + bOld + "\n+" + bNew + "\n"
}

func TestApplyPatch_MultiFile_TwoFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("a\n"), 0644)
	_ = os.WriteFile(b, []byte("b\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "apply_patch", map[string]any{"path": dir, "patch": twoFilePatch("a", "A", "b", "B")})
	if res.IsError {
		t.Fatalf("%s", resultText(t, res))
	}
	rawA, _ := os.ReadFile(a)
	rawB, _ := os.ReadFile(b)
	if string(rawA) != "A\n" || string(rawB) != "B\n" {
		t.Fatalf("got %q %q", rawA, rawB)
	}
}

func TestApplyPatch_MultiFile_SecondHunkFailsFirstIntact(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("a\n"), 0644)
	_ = os.WriteFile(b, []byte("b\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "apply_patch", map[string]any{"path": dir, "patch": twoFilePatch("a", "A", "NOPE", "B")})
	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, `"code":"PATCH_FAILED"`) || !strings.Contains(text, `"retryable":false`) {
		t.Fatalf("got %s", text)
	}
	rawA, _ := os.ReadFile(a)
	rawB, _ := os.ReadFile(b)
	if string(rawA) != "a\n" || string(rawB) != "b\n" {
		t.Fatalf("first file must stay intact, got %q %q", rawA, rawB)
	}
}

func TestApplyPatch_MultiFile_StaleHash(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("a\n"), 0644)
	_ = os.WriteFile(b, []byte("b\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "apply_patch", map[string]any{
		"path": dir, "patch": twoFilePatch("a", "A", "b", "B"), "expected_hash": "deadbeef",
	})
	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, `"retryable":false`) {
		t.Fatalf("got %s", text)
	}
	rawA, _ := os.ReadFile(a)
	rawB, _ := os.ReadFile(b)
	if string(rawA) != "a\n" || string(rawB) != "b\n" {
		t.Fatalf("stale hash must not write, got %q %q", rawA, rawB)
	}
}

func TestApplyPatch_MultiFile_DryRunNoWrites(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("a\n"), 0644)
	_ = os.WriteFile(b, []byte("b\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "apply_patch", map[string]any{"path": dir, "patch": twoFilePatch("a", "A", "b", "B"), "dry_run": true})
	if res.IsError || !strings.Contains(resultText(t, res), "DRY_RUN") {
		t.Fatalf("dry_run: %s", resultText(t, res))
	}
	rawA, _ := os.ReadFile(a)
	rawB, _ := os.ReadFile(b)
	if string(rawA) != "a\n" || string(rawB) != "b\n" {
		t.Fatalf("dry_run must not write, got %q %q", rawA, rawB)
	}
}

func TestApplyPatch_MultiFile_PatchFailedNotRetried(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("a\n"), 0644)
	_ = os.WriteFile(b, []byte("b\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	args := map[string]any{"path": dir, "patch": twoFilePatch("a", "A", "NOPE", "B")}
	res1 := callPatchTool(t, reg, "apply_patch", args)
	text1 := resultText(t, res1)
	if !res1.IsError || !strings.Contains(text1, `"code":"PATCH_FAILED"`) || !strings.Contains(text1, `"retryable":false`) {
		t.Fatalf("first: %s", text1)
	}
	res2 := callPatchTool(t, reg, "apply_patch", args)
	text2 := resultText(t, res2)
	if !res2.IsError || !strings.Contains(text2, `"code":"PATCH_FAILED"`) || !strings.Contains(text2, `"retryable":false`) {
		t.Fatalf("retry same patch: %s", text2)
	}
	rawA, _ := os.ReadFile(a)
	rawB, _ := os.ReadFile(b)
	if string(rawA) != "a\n" || string(rawB) != "b\n" {
		t.Fatalf("retry must not apply, got %q %q", rawA, rawB)
	}
}

func TestWriteFile_Append(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "log.txt")
	_ = os.WriteFile(p, []byte("one\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "write_file", map[string]any{"path": p, "content": "two\n", "mode": "append"})
	if res.IsError {
		t.Fatalf("%s", resultText(t, res))
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != "one\ntwo\n" {
		t.Fatalf("got %q", raw)
	}
}

func TestRollbackIncompleteResultSurfacesPartial(t *testing.T) {
	res := rollbackIncompleteResult(`C:\repo`, &core.RollbackError{
		Status: "partial", Failures: []string{`C:\repo\a.txt: changed`}, Err: fmt.Errorf("write conflict"),
	})
	if res == nil || !res.IsError {
		t.Fatal(res)
	}
	text := resultText(t, res)
	if !strings.Contains(text, `"code":"ROLLBACK_PARTIAL"`) || !strings.Contains(text, `"retryable":false`) || !strings.Contains(text, "a.txt") {
		t.Fatal(text)
	}
	complete := rollbackIncompleteResult(`C:\repo`, &core.RollbackError{Status: "complete", Err: &core.OCCMismatchError{Expected: "old", Actual: "new"}})
	text = resultText(t, complete)
	if !strings.Contains(text, `"code":"ROLLBACK_COMPLETE"`) || !strings.Contains(text, `"rollback_status":"complete"`) || strings.Contains(text, `"code":"OCC_MISMATCH"`) {
		t.Fatal(text)
	}
}

func TestPatchFailedMissingBaseDoesNotAskToRegenerate(t *testing.T) {
	res := patchFailedResult(`C:\missing`, &core.PatchError{Reason: core.PatchReasonMalformed, Msg: "base directory does not exist"}, nil)
	text := resultText(t, res)
	if strings.Contains(text, "regenerate the patch") || !strings.Contains(text, "base directory does not exist") || !strings.Contains(text, suggestionBaseMissing) {
		t.Fatal(text)
	}
}
