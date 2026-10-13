package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func TestAuditValidationRetainsDiagnosisAndCountsRejectedCall(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache: c, AllowedPaths: []string{dir}, ParallelOps: 2,
		CompactMode: true, LogDir: filepath.Join(dir, "logs"),
	})
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })

	called := false
	handler := auditWrap(engine, "write_file", func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return mcp.NewToolResultText("ok"), nil
	})
	path := filepath.Join(dir, "untouched.txt")
	res, err := handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "write_file", Arguments: map[string]interface{}{
			"path": path, "content": "payload-not-for-audit", "modde": "append",
		},
	}})
	if err != nil || res == nil || !res.IsError || called {
		t.Fatalf("invalid request reached handler or was not rejected: called=%v result=%v err=%v", called, res, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "logs", "operations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var entry core.AuditEntry
	// Unmarshal the entire log: a second entry would also fail this check.
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Status != "error" || entry.Path != path || entry.RequestID == "" || entry.SessionID == "" {
		t.Fatalf("missing audit context: %+v", entry)
	}
	if !strings.Contains(entry.Error, `unknown parameter "modde"`) || !strings.Contains(entry.Error, `did you mean "mode"`) {
		t.Fatalf("lost validation diagnosis: %s", entry.Error)
	}
	if entry.Args["modde"] != "append" || entry.Args["content"] != "(21 chars)" || strings.Contains(string(data), "payload-not-for-audit") {
		t.Fatalf("incorrect argument summary: %s", data)
	}
	stats := engine.GetPerformanceStats()
	if !strings.Contains(stats, "mcp:1 ") || !strings.Contains(stats, "applied:0 rejected:1") {
		t.Fatalf("rejected request missing from metrics: %s", stats)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected write created a file: %v", err)
	}
}
