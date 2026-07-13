package main

// Regression coverage for the help discovery surface and the
// filesystem-mismatch diagnostic. These tests were added together with the
// v4.5.29 host/sandbox separation fix to lock the contract that:
//   - help() returns the catalog of tools actually registered (no aliases),
//   - serverInstructions identifies the host filesystem without re-introducing
//     the prompt-injection-style imperative block removed in v4.3.6,
//   - read_file and write_file surface the host bind + verify rules.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

var _ = errors.Is

func newHelpTestRegistry(t *testing.T, allowedDir string) *toolRegistry {
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
	registerSearchTools(reg)
	registerFileTools(reg)
	registerBatchTools(reg)
	registerPlatformTools(reg)
	registerGitTools(reg)
	registerMinifyTools(reg)
	registerHelpTool(reg)
	return reg
}

func callHelp(t *testing.T, reg *toolRegistry, args map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "help",
			Arguments: args,
		},
	}
	// help is registered directly on the MCP server (not via reg.addTool) so the
	// dispatch map is not used. Route through the MCP server's handler chain.
	handlers := reg.server.ListTools()
	tool, ok := handlers["help"]
	if !ok {
		t.Fatal("help tool not registered on server")
	}
	if tool.Handler == nil {
		t.Fatal("help tool has no handler")
	}
	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("help handler: %v", err)
	}
	return result
}

func TestHelp_NoArgs_ListsAllRegisteredTools(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	result := callHelp(t, reg, nil)
	if result.IsError {
		t.Fatalf("help() returned error: %v", result.Content)
	}
	text := resultText(t, result)
	for _, want := range []string{
		"read_file", "write_file", "edit_file", "multi_edit", "list_directory",
		"get_file_info", "move_file", "copy_file", "delete_file", "create_directory",
		"search_files", "batch_operations", "backup", "analyze_operation",
		"wsl", "server_info", "git", "minify_js", "project_replace", "help",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help() missing %q", want)
		}
	}
}

func TestHelp_NoArgs_DoesNotAdvertiseDisabledAliases(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	text := resultText(t, callHelp(t, reg, nil))
	for _, banned := range []string{
		"create_file",
		"str_replace",
		"view",
		" GlobTool",
		" GrepTool",
		"directory_tree",
		"read_text_file",
	} {
		// names may still appear inside description prose, so we look for a
		// dedicated bullet/line — the catalog only emits them with a leading
		// "`-`" markdown prefix.
		if strings.Contains(text, "- `"+banned+"`") {
			t.Errorf("help() re-introduced disabled alias %q as a tool", banned)
		}
	}
}

func TestHelp_DescribesHostFilesystemScope(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	text := resultText(t, callHelp(t, reg, nil))
	if !strings.Contains(text, "real host filesystem") {
		t.Error("help() must explain the host filesystem scope")
	}
	if !strings.Contains(text, "sandbox") {
		t.Error("help() must warn about runtime-native tools running in a separate sandbox")
	}
}

func TestHelp_PerTool_StillReturnsSchema(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	result := callHelp(t, reg, map[string]interface{}{"tool": "write_file"})
	text := resultText(t, result)
	if !strings.Contains(text, "write_file") {
		t.Error("help(tool:write_file) must echo the tool name")
	}
	if !strings.Contains(text, "real host filesystem") {
		t.Error("help(tool:write_file) must surface the host-filesystem contract")
	}
}

func TestServerInstructions_IdentifiesHostScopeWithoutImperativeBlock(t *testing.T) {
	if !strings.Contains(serverInstructions, "real host filesystem") {
		t.Errorf("serverInstructions should identify the host filesystem scope: %q", serverInstructions)
	}
	if !strings.Contains(serverInstructions, "help()") {
		t.Errorf("serverInstructions should point at help() for the catalog: %q", serverInstructions)
	}
	// The v4.3.6 mitigation removed a long imperative block from serverInstructions
	// to avoid prompt-injection-style content. The post-v4.5.29 handshake text
	// must stay brief; the recovery rules live in help() and the skill.
	if strings.Contains(serverInstructions, "ALWAYS") || strings.Contains(serverInstructions, "CRITICAL") {
		t.Errorf("serverInstructions must not reintroduce imperative directives: %q", serverInstructions)
	}
}

func TestFilesystemMismatchSuffix_OnlyForMissingPath(t *testing.T) {
	if got := filesystemMismatchSuffix(nil); got != "" {
		t.Errorf("nil error must produce empty suffix, got %q", got)
	}

	other := &core.PathError{Op: "read", Path: "x", Err: errPermission}
	if got := filesystemMismatchSuffix(other); got != "" {
		t.Errorf("non-missing errors must produce empty suffix, got %q", got)
	}

	missing := &core.PathError{Op: "read", Path: "x", Err: errMissing}
	if got := filesystemMismatchSuffix(missing); !strings.Contains(got, "FILESYSTEM MISMATCH?") {
		t.Errorf("missing-path errors should append the recovery hint, got %q", got)
	}
}

func TestFormatToolError_PreservesPrefix(t *testing.T) {
	other := &core.PathError{Op: "read", Path: "x", Err: errPermission}
	otherMsg := formatToolError(other)
	if !strings.HasPrefix(otherMsg, "Error:") {
		t.Errorf("formatToolError must keep the Error: prefix, got %q", otherMsg)
	}
	if strings.Contains(otherMsg, "FILESYSTEM MISMATCH?") {
		t.Errorf("non-missing errors must not append the hint, got %q", otherMsg)
	}

	missing := &core.PathError{Op: "read", Path: "x", Err: errMissing}
	missingMsg := formatToolError(missing)
	if !strings.Contains(missingMsg, "FILESYSTEM MISMATCH?") {
		t.Errorf("missing-path errors should include the hint, got %q", missingMsg)
	}
}

// Handler-level coverage: the FILESYSTEM MISMATCH? hint must reach the wire
// for list_directory and edit_file too, not only read_file (gap found in the
// 2026-07-13 post-fix review).
func TestListDirectory_MissingPathAppendsMismatchHint(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "list_directory",
		Arguments: map[string]interface{}{"path": filepath.Join(dir, "does-not-exist")},
	}}
	result, err := reg.listDirHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("list_directory handler: %v", err)
	}
	if !result.IsError {
		t.Fatal("list_directory on missing path must return isError=true")
	}
	if text := resultText(t, result); !strings.Contains(text, "FILESYSTEM MISMATCH?") {
		t.Errorf("list_directory missing-path error must append the hint, got %q", text)
	}
}

func TestEditFile_MissingPathAppendsMismatchHint(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	handler, ok := reg.handlers["edit_file"]
	if !ok {
		t.Fatal("edit_file not registered")
	}
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "edit_file",
		Arguments: map[string]interface{}{
			"path":     filepath.Join(dir, "does-not-exist.txt"),
			"old_text": "a",
			"new_text": "b",
		},
	}}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("edit_file handler: %v", err)
	}
	if !result.IsError {
		t.Fatal("edit_file on missing path must return isError=true")
	}
	if text := resultText(t, result); !strings.Contains(text, "FILESYSTEM MISMATCH?") {
		t.Errorf("edit_file missing-path error must append the hint, got %q", text)
	}
}

// errPermission / errMissing are sentinel errors whose Errors.Is wire
// resolves or rejects against fs.ErrNotExist, so the matcher can distinguish
// "missing" from "other" without inspecting the message string.
var (
	errPermission = os.ErrPermission
	errMissing    = fs.ErrNotExist
)
