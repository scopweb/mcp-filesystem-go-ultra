package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// setupBug18Engine creates a test engine for literal escape tests
func setupBug18Engine(t *testing.T) (*core.UltraFastEngine, string) {
	t.Helper()
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}

	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("Failed to create test engine: %v", err)
	}

	return engine, tempDir
}

// TestBug18_LiteralNewlineEscapes tests that mcp_edit handles literal \n
// sent by Claude Desktop instead of real newlines.
// Bug: Claude Desktop sometimes sends "line1\nline2" as a flat string with
// literal backslash-n instead of actual newline characters.
func TestBug18_LiteralNewlineEscapes(t *testing.T) {
	engine, tempDir := setupBug18Engine(t)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "test_literal_escapes.razor")
	// File has real newlines
	content := "    private async Task HandleKeyDown(KeyboardEventArgs e) {\n        if (e.Key == \"Enter\") await Search();\n    }\n\n    private async Task Search()\n    {\n        // search logic\n    }"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Claude Desktop sends old_text with literal \n instead of real newlines
	oldTextWithLiteralEscapes := `    private async Task HandleKeyDown(KeyboardEventArgs e) {\n        if (e.Key == "Enter") await Search();\n    }\n\n    private async Task Search()`
	newText := "    private async Task HandleKeyDown(KeyboardEventArgs e) {\n        if (e.Key == \"Enter\") await Search();\n    }\n\n    private async Task SearchOrders()"

	result, err := engine.EditFile(ctx, testFile, oldTextWithLiteralEscapes, newText, false, false)
	if err != nil {
		t.Fatalf("EditFile failed with literal \\n escapes: %v", err)
	}
	if result.ReplacementCount == 0 {
		t.Fatal("Expected at least 1 replacement, got 0")
	}

	// Verify the file was actually modified
	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read modified file: %v", err)
	}
	if !strings.Contains(string(modified), "SearchOrders") {
		t.Errorf("Expected modified file to contain 'SearchOrders', got:\n%s", string(modified))
	}
	t.Logf("Successfully edited with literal \\n escapes, confidence: %s, replacements: %d",
		result.MatchConfidence, result.ReplacementCount)
}

// TestBug18_LiteralTabEscapes tests that literal \t is also normalized
func TestBug18_LiteralTabEscapes(t *testing.T) {
	engine, tempDir := setupBug18Engine(t)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "test_literal_tabs.txt")
	content := "\tfunction hello() {\n\t\treturn 'world';\n\t}"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// old_text with literal \t and \n
	oldTextLiteral := `\tfunction hello() {\n\t\treturn 'world';\n\t}`
	newText := "\tfunction hello() {\n\t\treturn 'universe';\n\t}"

	result, err := engine.EditFile(ctx, testFile, oldTextLiteral, newText, false, false)
	if err != nil {
		t.Fatalf("EditFile failed with literal \\t escapes: %v", err)
	}
	if result.ReplacementCount == 0 {
		t.Fatal("Expected at least 1 replacement, got 0")
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read modified file: %v", err)
	}
	if !strings.Contains(string(modified), "universe") {
		t.Errorf("Expected 'universe' in modified file, got:\n%s", string(modified))
	}
	t.Logf("Successfully edited with literal \\t escapes, confidence: %s", result.MatchConfidence)
}

// TestBug18_RealNewlinesStillWork ensures that normal (real) newlines are not broken
func TestBug18_RealNewlinesStillWork(t *testing.T) {
	engine, tempDir := setupBug18Engine(t)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "test_real_newlines.txt")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Normal edit with real newlines (should still work as before)
	// force=true because this small file triggers CRITICAL risk (>90% change)
	result, err := engine.EditFile(ctx, testFile, "line1\nline2", "lineA\nlineB", true, false)
	if err != nil {
		t.Fatalf("EditFile failed with real newlines: %v", err)
	}
	if result.ReplacementCount == 0 {
		t.Fatal("Expected at least 1 replacement, got 0")
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read modified file: %v", err)
	}
	if !strings.Contains(string(modified), "lineA\nlineB") {
		t.Errorf("Expected 'lineA\\nlineB' in modified file, got:\n%s", string(modified))
	}
}

// TestBug18_CodeWithBackslashN ensures that code containing literal \n strings
// (like Go/C# code with "\\n") is NOT corrupted by the normalization
func TestBug18_CodeWithBackslashN(t *testing.T) {
	engine, tempDir := setupBug18Engine(t)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "test_code_backslash_n.go")
	// This file contains actual code with \n in strings — should NOT be modified
	content := "package main\n\nfunc main() {\n\tfmt.Printf(\"hello\\nworld\")\n}\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Edit with real newlines — the literal \n inside Printf should be preserved
	// force=true because this small file triggers CRITICAL risk (>90% change)
	result, err := engine.EditFile(ctx, testFile, "fmt.Printf(\"hello\\nworld\")", "fmt.Printf(\"hello\\nuniverse\")", true, false)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if result.ReplacementCount == 0 {
		t.Fatal("Expected at least 1 replacement, got 0")
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read modified file: %v", err)
	}
	modStr := string(modified)
	// The \n inside the Printf string should still be literal \n (not a real newline)
	if !strings.Contains(modStr, `hello\nuniverse`) {
		t.Errorf("Expected literal \\n preserved in code, got:\n%s", modStr)
	}
}
