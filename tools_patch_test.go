package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func callPatchTool(t *testing.T, reg *toolRegistry, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	h, ok := reg.handlers[name]
	if !ok {
		t.Fatalf("%s not registered", name)
	}
	res, err := h(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}})
	if err != nil {
		t.Fatal(err)
	}
	return res
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

func TestApplyPatch_OCCMismatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("x\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1 +1 @@\n-x\n+y\n"
	res := callPatchTool(t, reg, "apply_patch", map[string]any{
		"path": p, "patch": patch, "expected_hash": "deadbeef",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "OCC_MISMATCH") {
		t.Fatalf("got %s", resultText(t, res))
	}
}

func TestApplyPatch_PathEscapeHeader(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("x\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	patch := "--- a/other.txt\n+++ b/other.txt\n@@ -1 +1 @@\n-x\n+y\n"
	res := callPatchTool(t, reg, "apply_patch", map[string]any{"path": p, "patch": patch})
	if !res.IsError || !strings.Contains(resultText(t, res), "PATCH_APPLY_FAILED") {
		t.Fatalf("got %s", resultText(t, res))
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
