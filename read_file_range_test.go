package main

// Integration tests for read_file with start_line/end_line (range mode).
// Exercises the MCP handler path in tools_core.go → ReadFileRange.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func callReadFile(t *testing.T, reg *toolRegistry, params map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "read_file",
			Arguments: params,
		},
	}
	result, err := reg.readFileHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("read_file handler error: %v", err)
	}
	return result
}

func TestReadFileHandler_StartEndLine_FooterAndContent(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)

	const totalLines = 200
	path := filepath.Join(dir, "range_handler.go")
	var b strings.Builder
	for i := 1; i <= totalLines; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	original := b.String()
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	result := callReadFile(t, reg, map[string]interface{}{
		"path":       path,
		"start_line": float64(15),
		"end_line":   float64(50),
	})
	if result.IsError {
		t.Fatalf("read_file returned error: %v", result.Content)
	}
	text := resultText(t, result)

	body := text
	if idx := strings.Index(text, "\n\n[Lines "); idx >= 0 {
		body = text[:idx]
	}
	hasLine := func(n int) bool {
		return regexp.MustCompile(fmt.Sprintf(`(?m)^line %d$`, n)).MatchString(body)
	}
	for _, n := range []int{15, 50} {
		if !hasLine(n) {
			t.Fatalf("response must include line %d, got body tail:\n%s", n, body[max(0, len(body)-300):])
		}
	}
	for _, n := range []int{14, 51} {
		if hasLine(n) {
			t.Fatalf("response must not include line %d outside requested range", n)
		}
	}

	wantTotal := fmt.Sprintf("of %d total lines", totalLines)
	if !strings.Contains(text, wantTotal) {
		t.Fatalf("footer must report real total %q\n--- tail ---\n%s", wantTotal, text[len(text)-200:])
	}
	wantHint := "start_line=51 end_line=86"
	if !strings.Contains(text, wantHint) {
		t.Fatalf("footer must include continuation hint %q\n%s", wantHint, text[len(text)-200:])
	}

	// Range mode must not append content_hash (only full reads do).
	if strings.Contains(text, "content_hash:") {
		t.Fatal("range read must not append content_hash footer")
	}
}

func TestReadFileHandler_StartEndLine_DoesNotTruncateFile(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)

	path := filepath.Join(dir, "no_truncate.go")
	var b strings.Builder
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&b, "content line %d with payload\n", i)
	}
	original := b.String()
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	result := callReadFile(t, reg, map[string]interface{}{
		"path":       path,
		"start_line": float64(40),
		"end_line":   float64(50),
	})
	if result.IsError {
		t.Fatalf("read_file returned error: %v", result.Content)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatal("read_file range mode must not modify the file on disk")
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Size() != beforeInfo.Size() {
		t.Fatalf("file size changed: before=%d after=%d", beforeInfo.Size(), afterInfo.Size())
	}
}