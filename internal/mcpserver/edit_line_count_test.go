package mcpserver

// Covers the line_count alternative to end_line for edit_file's delete_range
// and replace_range modes — the offset+count convention added alongside
// read_file's start_line+max_lines (see read_file_range_test.go), for agents
// that treat end_line as a line count instead of an absolute line number.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func callEditFileMode(t *testing.T, reg *toolRegistry, params map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "edit_file",
		Arguments: params,
	}}
	res, err := reg.editFileHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("edit_file handler error: %v", err)
	}
	return res
}

func makeNumberedFile(t *testing.T, dir, name string, totalLines int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var b strings.Builder
	for i := 1; i <= totalLines; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEditFile_DeleteRange_LineCountMatchesEndLine(t *testing.T) {
	dir := t.TempDir()

	pathCount := makeNumberedFile(t, dir, "count.go", 60)
	regCount := buildEditRegistry(t, dir, false)
	resCount := callEditFileMode(t, regCount, map[string]interface{}{
		"path": pathCount, "mode": "delete_range",
		"start_line": float64(10), "line_count": float64(6), // → lines 10..15
	})
	if resCount.IsError {
		t.Fatalf("delete_range with line_count errored: %v", resCount.Content)
	}
	afterCount, err := os.ReadFile(pathCount)
	if err != nil {
		t.Fatal(err)
	}

	pathAbs := makeNumberedFile(t, dir, "abs.go", 60)
	resAbs := callEditFileMode(t, regCount, map[string]interface{}{
		"path": pathAbs, "mode": "delete_range",
		"start_line": float64(10), "end_line": float64(15),
	})
	if resAbs.IsError {
		t.Fatalf("delete_range with end_line errored: %v", resAbs.Content)
	}
	afterAbs, err := os.ReadFile(pathAbs)
	if err != nil {
		t.Fatal(err)
	}

	if string(afterCount) != string(afterAbs) {
		t.Fatalf("start_line+line_count must delete the same lines as the equivalent start_line+end_line\ncount result:\n%s\nabs result:\n%s",
			afterCount, afterAbs)
	}
	if strings.Contains(string(afterCount), "line 12") {
		t.Fatal("line 12 should have been removed")
	}
	if !strings.Contains(string(afterCount), "line 9") || !strings.Contains(string(afterCount), "line 16") {
		t.Fatal("lines outside the range must survive")
	}
}

func TestEditFile_ReplaceRange_LineCountMatchesEndLine(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)

	path := makeNumberedFile(t, dir, "replace.go", 40)
	res := callEditFileMode(t, reg, map[string]interface{}{
		"path": path, "mode": "replace_range",
		"start_line": float64(5), "line_count": float64(5),
		"new_text": "REPLACED",
	})
	if res.IsError {
		t.Fatalf("replace_range with line_count errored: %v", res.Content)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// start_line:5, line_count:5 → lines 5..9 replaced (5 lines).
	for _, n := range []int{5, 6, 7, 8, 9} {
		if strings.Contains(string(after), fmt.Sprintf("line %d\n", n)) {
			t.Fatalf("line %d should have been replaced, got:\n%s", n, after)
		}
	}
	if !strings.Contains(string(after), "line 4\n") || !strings.Contains(string(after), "line 10\n") {
		t.Fatalf("lines outside the range must survive, got:\n%s", after)
	}
	if !strings.Contains(string(after), "REPLACED") {
		t.Fatalf("new_text must appear, got:\n%s", after)
	}
}

func TestEditFile_DeleteRange_RequiresEndLineOrLineCount(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := makeNumberedFile(t, dir, "bare.go", 10)

	res := callEditFileMode(t, reg, map[string]interface{}{
		"path": path, "mode": "delete_range", "start_line": float64(3),
	})
	if !res.IsError {
		t.Fatal("delete_range with neither end_line nor line_count must error")
	}
}
