package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// read_handler_perf_test.go — the read_file handler in-process, so the cost
// between the engine and the wire is visible: the MCP-level numbers include
// JSON-RPC framing and the stdio hop.

func benchReadHandler(b *testing.B, size int) (string, *toolRegistry) {
	b.Helper()
	dir := b.TempDir()
	p := filepath.Join(dir, "data.bin")
	line := []byte("BENCH_READ_LINE_0123456789 padding\n")
	content := make([]byte, size)
	for i := 0; i < size; i += len(line) {
		copy(content[i:], line)
	}
	if err := os.WriteFile(p, content, 0o600); err != nil {
		b.Fatal(err)
	}

	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		b.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{dir},
		ParallelOps:  2,
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { engine.Close() })
	s := server.NewMCPServer("bench", "0.0.0")
	if err := registerToolsOpts(s, engine, registerOpts{Profile: profileUltra}); err != nil {
		b.Fatal(err)
	}
	reg := &toolRegistry{server: s, engine: engine, handlers: map[string]toolHandler{}, profile: profileUltra}
	return p, reg
}

func BenchmarkReadHandler_2KB(b *testing.B) {
	p, reg := benchReadHandler(b, 2*1024)
	req := benchReadRequest(p)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := reg.server.HandleMessage(context.Background(), req)
		if res == nil {
			b.Fatal("nil result")
		}
		if _, err := json.Marshal(res); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadHandler_5MB(b *testing.B) {
	p, reg := benchReadHandler(b, 5*1024*1024)
	req := benchReadRequest(p)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := reg.server.HandleMessage(context.Background(), req)
		if res == nil {
			b.Fatal("nil result")
		}
		if _, err := json.Marshal(res); err != nil {
			b.Fatal(err)
		}
	}
}

// benchReadRequest builds the JSON-RPC request the transport would deliver.
func benchReadRequest(path string) []byte {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "read_file",
			"arguments": map[string]any{"path": path},
		},
	})
	return body
}
