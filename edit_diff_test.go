package main

// Integration tests for the "Unified Diff in edit responses" feature (v4.3.0).
//
// These tests verify that the edit_file handler appends a standard unified diff
// to its response in both normal and compact modes, and that dry-run calls
// do NOT include a diff (no changes applied).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// buildEditRegistry creates a minimal toolRegistry with only edit_file registered.
func buildEditRegistry(t *testing.T, allowedDir string, compact bool) *toolRegistry {
	t.Helper()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedDir},
		ParallelOps:  2,
		CompactMode:  compact,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })

	s := server.NewMCPServer("test", "0.0.0")
	reg := &toolRegistry{
		server:         s,
		engine:         engine,
		handlers:       make(map[string]toolHandler),
		regexTransform: core.NewRegexTransformer(engine),
	}
	registerCoreTools(reg)
	return reg
}

// callEdit invokes the edit_file handler directly.
func callEdit(t *testing.T, reg *toolRegistry, params map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "edit_file",
			Arguments: params,
		},
	}
	result, err := reg.editFileHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return result
}

// resultText extracts the text content from a CallToolResult.
func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if r == nil {
		t.Fatal("nil result")
	}
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no TextContent in result")
	return ""
}

// --- Tests ---

func TestEditFile_DiffInResponse_VerboseMode(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hello.go")
	original := "package main\n\nfunc Hello() string {\n\treturn \"old\"\n}\n"
	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, false /* verbose */)

	result := callEdit(t, reg, map[string]interface{}{
		"path":     file,
		"old_text": "\"old\"",
		"new_text": "\"new\"",
	})

	text := resultText(t, result)
	t.Logf("Response:\n%s", text)

	if result.IsError {
		t.Fatalf("edit failed: %s", text)
	}
	if !strings.Contains(text, "--- a/") {
		t.Error("response must contain unified diff header '--- a/'")
	}
	if !strings.Contains(text, "+++ b/") {
		t.Error("response must contain unified diff header '+++ b/'")
	}
	if !strings.Contains(text, "-\treturn \"old\"") {
		t.Error("diff must show the removed line")
	}
	if !strings.Contains(text, "+\treturn \"new\"") {
		t.Error("diff must show the added line")
	}
}

func TestEditFile_DiffInResponse_CompactMode(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "svc.go")
	original := "package main\n\nconst Version = \"1.0\"\n"
	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, true /* compact */)

	result := callEdit(t, reg, map[string]interface{}{
		"path":     file,
		"old_text": "\"1.0\"",
		"new_text": "\"2.0\"",
	})

	text := resultText(t, result)
	t.Logf("Compact response:\n%s", text)

	if result.IsError {
		t.Fatalf("edit failed: %s", text)
	}
	if !strings.Contains(text, "--- a/") {
		t.Error("compact response must also contain unified diff")
	}
	if !strings.Contains(text, "-const Version = \"1.0\"") {
		t.Error("diff must show removed line")
	}
	if !strings.Contains(text, "+const Version = \"2.0\"") {
		t.Error("diff must show added line")
	}
}

func TestEditFile_DryRun_NoDiff(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dry.go")
	original := "package main\n\nvar X = 1\n"
	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, false)

	result := callEdit(t, reg, map[string]interface{}{
		"path":     file,
		"old_text": "var X = 1",
		"new_text": "var X = 99",
		"dry_run":  true,
	})

	text := resultText(t, result)
	t.Logf("Dry-run response:\n%s", text)

	if result.IsError {
		t.Fatalf("dry-run failed: %s", text)
	}
	if !strings.Contains(strings.ToUpper(text), "DRY RUN") {
		t.Error("response must indicate DRY RUN")
	}
	if strings.Contains(text, "--- a/") {
		t.Error("dry-run must NOT contain a diff (no changes were applied)")
	}
}

func TestEditFile_NoMatchOldText_NoDiff(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "nomatch.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := buildEditRegistry(t, dir, false)

	result := callEdit(t, reg, map[string]interface{}{
		"path":     file,
		"old_text": "NONEXISTENT_TOKEN_XYZ",
		"new_text": "replacement",
	})

	text := resultText(t, result)

	if !result.IsError {
		t.Fatal("expected an error result when old_text not found")
	}
	if strings.Contains(text, "--- a/") {
		t.Error("error response must NOT contain a diff")
	}
}
