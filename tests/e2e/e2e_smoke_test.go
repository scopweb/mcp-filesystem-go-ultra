//go:build e2e

// E2E smoke battery: builds the REAL server binary, launches it over stdio
// with the strict mcp-go client, and drives every registered tool end to end.
//
// This is the test that would have caught the 2026-07-19 incident bugs from
// CI: unit tests exercise handlers in-process, but schema-declared tools that
// return plain text (the -32600 class) only surface when the result crosses
// the real MCP transport and gets validated by a strict client.
//
// Run explicitly (excluded from `go test ./...` by the e2e build tag):
//
//	go test -tags e2e ./tests/e2e/ -v
package e2e

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// buildServer compiles the server binary into a temp dir.
func buildServer(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "fsu-e2e")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", exe, "github.com/mcp/filesystem-ultra").CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return exe
}

// call invokes a tool and fails the test on transport or tool errors.
func call(t *testing.T, c *client.Client, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := c.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("%s: transport error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool error: %v", name, res.Content)
	}
	return res
}

// requireStructured asserts a schema-declared tool returned structuredContent.
// This is the assertion that fails with the -32600 bug class.
func requireStructured(t *testing.T, name string, res *mcp.CallToolResult, requiredKeys ...string) {
	t.Helper()
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s: StructuredContent is %T — plain-text result on a schema-declared tool", name, res.StructuredContent)
	}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: structured payload missing required key %q: %#v", name, k, m)
		}
	}
}

func TestE2E_SmokeBattery(t *testing.T) {
	exe := buildServer(t)
	workDir := t.TempDir()

	c, err := client.NewStdioMCPClient(exe, nil, workDir)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "e2e-smoke", Version: "0.1"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	target := filepath.Join(workDir, "a.txt")
	jsFile := filepath.Join(workDir, "b.js")

	// help — discovery
	call(t, c, "help", map[string]any{})

	// write_file (schema tool)
	res := call(t, c, "write_file", map[string]any{"path": target, "content": "alpha beta gamma\nsecond foo\nthird foo\n"})
	requireStructured(t, "write_file", res, "path", "bytes_written", "verified", "message")

	// read_file (schema tool) — full + range + base64
	res = call(t, c, "read_file", map[string]any{"path": target})
	requireStructured(t, "read_file", res, "content")
	call(t, c, "read_file", map[string]any{"path": target, "start_line": 1, "end_line": 1})
	call(t, c, "read_file", map[string]any{"path": target, "encoding": "base64"})

	// edit_file (schema tool) — every mode, the -32600 regression class
	res = call(t, c, "edit_file", map[string]any{"path": target, "old_text": "beta", "new_text": "BETA"})
	requireStructured(t, "edit_file replace", res, "path", "replacements", "lines_added", "lines_removed", "total_lines", "message")
	res = call(t, c, "edit_file", map[string]any{"path": target, "mode": "search_replace", "pattern": "foo", "replacement": "FOO"})
	requireStructured(t, "edit_file search_replace", res, "path", "replacements", "lines_added", "lines_removed", "total_lines", "message")
	res = call(t, c, "edit_file", map[string]any{"path": target, "old_text": "FOO", "new_text": "z", "occurrence": 1})
	requireStructured(t, "edit_file occurrence", res, "path", "replacements", "lines_added", "lines_removed", "total_lines", "message")
	res = call(t, c, "edit_file", map[string]any{"path": target, "mode": "regex", "pattern": "(alpha) (BETA)", "replacement": "$2 $1"})
	requireStructured(t, "edit_file regex", res, "path", "replacements", "lines_added", "lines_removed", "total_lines", "message")
	res = call(t, c, "edit_file", map[string]any{"path": target, "old_text": "alpha", "new_text": "X", "dry_run": true})
	requireStructured(t, "edit_file dry_run", res, "path", "replacements", "lines_added", "lines_removed", "total_lines", "message")

	// multi_edit (schema tool)
	res = call(t, c, "multi_edit", map[string]any{"path": target,
		"edits_json": `[{"old_text":"second","new_text":"2nd"},{"old_text":"third","new_text":"3rd"}]`})
	requireStructured(t, "multi_edit", res, "path", "successful_edits", "total_edits", "message")

	// search / listing / metadata
	call(t, c, "search_files", map[string]any{"path": workDir, "pattern": "a.txt"})
	call(t, c, "search_files", map[string]any{"path": workDir, "pattern": "BETA", "include_content": true})
	call(t, c, "list_directory", map[string]any{"path": workDir})
	call(t, c, "get_file_info", map[string]any{"path": target})

	// copy / move
	call(t, c, "copy_file", map[string]any{"source_path": target, "dest_path": filepath.Join(workDir, "copy.txt")})
	call(t, c, "move_file", map[string]any{"source_path": filepath.Join(workDir, "copy.txt"), "dest_path": filepath.Join(workDir, "moved.txt")})

	// analyze_operation (dry-run)
	call(t, c, "analyze_operation", map[string]any{"path": target, "operation": "edit", "old_text": "a", "new_text": "b"})

	// create_directory + batch_operations
	call(t, c, "create_directory", map[string]any{"path": filepath.Join(workDir, "sub")})
	call(t, c, "batch_operations", map[string]any{"request_json": `{"operations":[{"type":"write","path":"` + filepath.ToSlash(filepath.Join(workDir, "sub", "batch.txt")) + `","content":"batch MARK"},{"type":"edit","path":"` + filepath.ToSlash(filepath.Join(workDir, "sub", "batch.txt")) + `","old_text":"MARK","new_text":"OK"}],"atomic":true}`})

	// project_replace (no matches expected — safe no-op on unrelated content)
	call(t, c, "project_replace", map[string]any{"path": filepath.Join(workDir, "sub"), "find": "OK", "replace": "DONE"})

	// minify_js
	call(t, c, "write_file", map[string]any{"path": jsFile, "content": "// c\nfunction f() {\n  return 1; // n\n}\n"})
	call(t, c, "minify_js", map[string]any{"path": jsFile})

	// backup + server_info + wsl
	call(t, c, "backup", map[string]any{"action": "list"})
	call(t, c, "server_info", map[string]any{"action": "stats"})
	call(t, c, "wsl", map[string]any{"action": "status"})

	// git — only when git is installed (validates the execGitCommand path E2E)
	if _, err := exec.LookPath("git"); err == nil {
		call(t, c, "git", map[string]any{"action": "init", "path": workDir})
		call(t, c, "git", map[string]any{"action": "add", "path": workDir, "paths": []any{"a.txt"}})
		call(t, c, "git", map[string]any{"action": "commit", "path": workDir, "message": "e2e: commit with & metachar"})
		call(t, c, "git", map[string]any{"action": "status", "path": workDir})
		call(t, c, "git", map[string]any{"action": "log", "path": workDir, "limit": 1})
	} else {
		t.Log("git not found — skipping git tool battery")
	}

	// delete_file (soft delete)
	call(t, c, "delete_file", map[string]any{"path": filepath.Join(workDir, "moved.txt")})
}
