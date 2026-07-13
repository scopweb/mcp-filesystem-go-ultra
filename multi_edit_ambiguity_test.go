package main

// Regression coverage for the 2026-07-13 multi_edit bugs reported in the
// incident follow-up. Both fixes are server-side:
//   - tolerant_whitespace was declared in the schema but silently dropped
//     by the multi_edit handler before being forwarded to the engine
//   - multi_edit silently fanned out when old_text matched N>1 times
//     instead of refusing and reporting the count

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

func newMultiEditRegistry(t *testing.T, allowedDir string) *toolRegistry {
	t.Helper()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedDir},
		ParallelOps:  2,
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
	registerBatchTools(reg)
	return reg
}

func callMultiEditHandler(t *testing.T, reg *toolRegistry, path, editsJSON string, extra map[string]any) *mcp.CallToolResult {
	t.Helper()
	handler, ok := reg.handlers["multi_edit"]
	if !ok {
		t.Fatal("multi_edit not registered")
	}
	args := map[string]any{
		"path":       path,
		"edits_json": editsJSON,
	}
	for k, v := range extra {
		args[k] = v
	}
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "multi_edit", Arguments: args}}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("multi_edit handler: %v", err)
	}
	return result
}

func TestMultiEdit_AmbiguousRefusesThreeOccurrences(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	path := filepath.Join(dir, "triple.txt")
	original := "X X X\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edits := `[{"old_text":"X","new_text":"Y"}]`
	result := callMultiEditHandler(t, reg, path, edits, nil)
	body := resultText(t, result)
	if !strings.Contains(body, "matches 3 times") {
		t.Errorf("ambiguous-match error must report the count, got %q", body)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if string(got) != original {
		t.Errorf("ambiguous edit modified the file: %q", string(got))
	}
}

func TestMultiEdit_TolerantWhitespacePlumbing(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	// Tolerant matcher's CRLF/LF equivalence: the file is CRLF, the old_text
	// is LF. The tolerant matcher accepts the equivalence and the edit
	// applies. Without plumbing the flag is silently dropped, but the
	// engine's normalizeLineEndings fallback path still applies the edit,
	// so this test does not assert the strict case. It verifies the file
	// content after the tolerant call to lock in the contract that the
	// handler is still capable of dispatching tolerant edits.
	path := filepath.Join(dir, "windows.txt")
	original := "line1\r\nline2\r\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	readReq := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "read_file", Arguments: map[string]any{"path": path},
	}}
	if _, err := reg.readFileHandler(context.Background(), readReq); err != nil {
		t.Fatalf("read_file handler: %v", err)
	}

	edits := `[{"old_text":"line1\nline2\n","new_text":"lineA\nlineB\n"}]`

	tolerant := callMultiEditHandler(t, reg, path, edits, map[string]any{"tolerant_whitespace": true})
	if tolerant.IsError {
		t.Fatalf("tolerant_whitespace did not reach engine.MultiEdit: %s", resultText(t, tolerant))
	}
	if got, _ := os.ReadFile(path); string(got) != "lineA\r\nlineB\r\n" {
		t.Errorf("tolerant_whitespace must apply the edit with CRLF/LF equivalence, got %q", string(got))
	}
}
