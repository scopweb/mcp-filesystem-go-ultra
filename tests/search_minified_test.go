package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	localmcp "github.com/mcp/filesystem-ultra/mcp"
)

// createMinifiedSearchTestEngine creates an engine restricted to a temp directory for
// search-related tests.
func createMinifiedSearchTestEngine(t *testing.T) (*core.UltraFastEngine, string) {
	t.Helper()
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(50 * 1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
		CompactMode:  false,
	})
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	return engine, tempDir
}

// writeSearchFixture writes a small source file and a minified bundle under dir.
func writeSearchFixture(t *testing.T, dir string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("function useSwal() { swal('hello'); }\n"), 0o600); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	// sweetalert2.min.js is a single huge line with many occurrences of "swal".
	// In the original bug this produced >50 KB of output per search.
	var minified strings.Builder
	minified.WriteString("/*! sweetalert2.min.js */")
	for i := 0; i < 2000; i++ {
		minified.WriteString("swal();")
	}
	if err := os.WriteFile(filepath.Join(dir, "sweetalert2.min.js"), []byte(minified.String()), 0o600); err != nil {
		t.Fatalf("write sweetalert2.min.js: %v", err)
	}

	// Source map is also a generated single-line JSON blob.
	if err := os.WriteFile(filepath.Join(dir, "app.js.map"), []byte("{\"version\":3,\"sources\":[\"app.js\"],\"swal\":\"x\"}"+strings.Repeat("x", 5000)), 0o600); err != nil {
		t.Fatalf("write app.js.map: %v", err)
	}
}

func TestSmartSearch_SkipsMinifiedContent(t *testing.T) {
	engine, dir := createMinifiedSearchTestEngine(t)
	writeSearchFixture(t, dir)

	req := localmcp.CallToolRequest{Arguments: map[string]interface{}{
		"path":            dir,
		"pattern":         "swal",
		"include_content": true,
		"output_format":   "text",
	}}
	resp, err := engine.SmartSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("SmartSearch failed: %v", err)
	}
	text := resp.Content[0].Text

	if !strings.Contains(text, "app.js") {
		t.Errorf("expected result to mention app.js, got:\n%s", text)
	}
	if strings.Contains(text, "sweetalert2.min.js") {
		t.Errorf("minified content should be excluded, but sweetalert2.min.js appeared:\n%s", text)
	}
	if strings.Contains(text, "app.js.map") {
		t.Errorf("source map content should be excluded, but app.js.map appeared:\n%s", text)
	}
}

func TestAdvancedTextSearch_SkipsMinifiedContent(t *testing.T) {
	engine, dir := createMinifiedSearchTestEngine(t)
	writeSearchFixture(t, dir)

	req := localmcp.CallToolRequest{Arguments: map[string]interface{}{
		"path":          dir,
		"pattern":       "swal",
		"output_format": "text",
	}}
	resp, err := engine.AdvancedTextSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("AdvancedTextSearch failed: %v", err)
	}
	text := resp.Content[0].Text

	if !strings.Contains(text, "app.js") {
		t.Errorf("expected result to mention app.js, got:\n%s", text)
	}
	if strings.Contains(text, "sweetalert2.min.js") {
		t.Errorf("minified content should be excluded, but sweetalert2.min.js appeared:\n%s", text)
	}
}

func TestAdvancedTextSearch_TruncatesLongLines(t *testing.T) {
	engine, dir := createMinifiedSearchTestEngine(t)

	var longLine strings.Builder
	longLine.WriteString("// start\n")
	longLine.WriteString("const hugeToken = '")
	for i := 0; i < 1000; i++ {
		longLine.WriteString("x")
	}
	longLine.WriteString("';\n")
	if err := os.WriteFile(filepath.Join(dir, "long.js"), []byte(longLine.String()), 0o600); err != nil {
		t.Fatalf("write long.js: %v", err)
	}

	req := localmcp.CallToolRequest{Arguments: map[string]interface{}{
		"path":          dir,
		"pattern":       "hugeToken",
		"output_format": "text",
	}}
	resp, err := engine.AdvancedTextSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("AdvancedTextSearch failed: %v", err)
	}
	text := resp.Content[0].Text

	if !strings.Contains(text, "long.js") {
		t.Errorf("expected result to mention long.js, got:\n%s", text)
	}
	if !strings.Contains(text, " …") {
		t.Errorf("expected long line to be truncated with ellipsis, got:\n%s", text)
	}

	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "hugeToken") {
			if len([]rune(line)) > core.MaxSearchLineLength+10 {
				t.Errorf("truncated line is too long: %d runes\n%s", len([]rune(line)), line)
			}
		}
	}
}

func TestCountOccurrences_SkipsMinified(t *testing.T) {
	engine, dir := createMinifiedSearchTestEngine(t)
	writeSearchFixture(t, dir)

	count, err := engine.CountOccurrences(context.Background(), dir, "swal", false, true, false)
	if err != nil {
		t.Fatalf("CountOccurrences failed: %v", err)
	}

	if !strings.Contains(count, "app.js") {
		t.Errorf("expected result to mention app.js, got:\n%s", count)
	}
	if strings.Contains(count, "sweetalert2.min.js") {
		t.Errorf("minified file should not be counted, but sweetalert2.min.js appeared:\n%s", count)
	}
}
