package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStatsCacheDemandReadPath(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "one.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]any {
		t.Helper()
		res, err := reg.handlers["server_info"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "server_info", Arguments: map[string]any{"action": "stats"}}})
		if err != nil || res.IsError {
			t.Fatalf("stats: %v %v", res, err)
		}
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m["cache"].(map[string]any)
	}
	initial := snapshot()
	if initial["file_hits"] != float64(0) || initial["file_misses"] != float64(0) {
		t.Fatal(initial)
	}
	for i := 0; i < 2; i++ {
		res := callReadFile(t, reg, map[string]any{"path": path, "encoding": "base64"})
		if res.IsError {
			t.Fatal(res)
		}
	}
	after := snapshot()
	if after["file_hits"] != float64(1) || after["file_misses"] != float64(1) || after["disk_loads"] != float64(1) || after["resident_bytes_tracked"] != float64(5) {
		t.Fatal(after)
	}
	if !reflect.DeepEqual(after, snapshot()) {
		t.Fatal("stats changed cache counters")
	}
}
