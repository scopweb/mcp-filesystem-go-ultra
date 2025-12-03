// Bug #9 Test - Optional search parameters
// Tests that smart_search and advanced_text_search accept and use optional parameters

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	"github.com/mcp/filesystem-ultra/mcp"
)

// Helper function to create engine for tests
func createTestEngineForBug9(t *testing.T) *core.UltraFastEngine {
	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	cfg := &core.Config{
		Cache:            cacheInstance,
		ParallelOps:      2,
		DebugMode:        false,
		CompactMode:      false,
		MaxSearchResults: 100,
	}

	engine, err := core.NewUltraFastEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	return engine
}

// TestSmartSearchWithIncludeContent tests smart_search with include_content parameter
func TestSmartSearchWithIncludeContent(t *testing.T) {
	testDir := t.TempDir()

	// Create test files
	testFile1 := filepath.Join(testDir, "file1.go")
	testFile2 := filepath.Join(testDir, "file2.txt")
	testFile3 := filepath.Join(testDir, "other.md")

	if err := os.WriteFile(testFile1, []byte("package main\nfunc TestFunction() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("This is a test file\nwith TestFunction mentioned\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile3, []byte("# Documentation\nNo matches here\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	engine := createTestEngineForBug9(t)
	ctx := context.Background()

	t.Run("SmartSearch without include_content", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":            testDir,
				"pattern":         "TestFunction",
				"include_content": false,
			},
		}

		resp, err := engine.SmartSearch(ctx, req)
		if err != nil {
			t.Fatalf("SmartSearch failed: %v", err)
		}

		result := resp.Content[0].Text
		// Should only find filename matches, not content
		if strings.Contains(result, "Content matches") {
			t.Errorf("Expected no content matches when include_content=false, got: %s", result)
		}
	})

	t.Run("SmartSearch with include_content", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":            testDir,
				"pattern":         "TestFunction",
				"include_content": true,
			},
		}

		resp, err := engine.SmartSearch(ctx, req)
		if err != nil {
			t.Fatalf("SmartSearch failed: %v", err)
		}

		result := resp.Content[0].Text
		// Should find content matches
		if !strings.Contains(result, "Content matches") && !strings.Contains(result, "content matches") {
			t.Errorf("Expected content matches when include_content=true, got: %s", result)
		}

		// Should find matches in both .go and .txt files
		if !strings.Contains(result, "file1.go") {
			t.Errorf("Expected to find file1.go in results")
		}
		if !strings.Contains(result, "file2.txt") {
			t.Errorf("Expected to find file2.txt in results")
		}
	})
}

// TestSmartSearchWithFileTypes tests smart_search with file_types parameter
func TestSmartSearchWithFileTypes(t *testing.T) {
	testDir := t.TempDir()

	// Create test files
	testFile1 := filepath.Join(testDir, "search.go")
	testFile2 := filepath.Join(testDir, "search.txt")
	testFile3 := filepath.Join(testDir, "search.md")

	if err := os.WriteFile(testFile1, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("text content\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile3, []byte("# Markdown\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	engine := createTestEngineForBug9(t)
	ctx := context.Background()

	t.Run("SmartSearch with file_types filter", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":            testDir,
				"pattern":         "search",
				"include_content": true,
				"file_types":      []interface{}{".go", ".txt"},
			},
		}

		resp, err := engine.SmartSearch(ctx, req)
		if err != nil {
			t.Fatalf("SmartSearch failed: %v", err)
		}

		result := resp.Content[0].Text

		// Should find .go and .txt files
		if !strings.Contains(result, "search.go") {
			t.Errorf("Expected to find search.go in results")
		}
		if !strings.Contains(result, "search.txt") {
			t.Errorf("Expected to find search.txt in results")
		}

		// Should NOT find .md file
		if strings.Contains(result, "search.md") {
			t.Errorf("Should not find search.md when filtering by .go and .txt, got: %s", result)
		}
	})
}

// TestAdvancedTextSearchCaseSensitive tests advanced_text_search with case_sensitive parameter
func TestAdvancedTextSearchCaseSensitive(t *testing.T) {
	testDir := t.TempDir()

	testFile := filepath.Join(testDir, "test.txt")
	content := "Hello World\nhello world\nHELLO WORLD\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	engine := createTestEngineForBug9(t)
	ctx := context.Background()

	t.Run("AdvancedTextSearch case insensitive", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":           testDir,
				"pattern":        "hello",
				"case_sensitive": false,
			},
		}

		resp, err := engine.AdvancedTextSearch(ctx, req)
		if err != nil {
			t.Fatalf("AdvancedTextSearch failed: %v", err)
		}

		result := resp.Content[0].Text
		// Should find all 3 variations
		matchCount := strings.Count(result, "test.txt:")
		if matchCount < 3 {
			t.Errorf("Expected at least 3 matches for case-insensitive search, got %d. Result: %s", matchCount, result)
		}
	})

	t.Run("AdvancedTextSearch case sensitive", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":           testDir,
				"pattern":        "hello",
				"case_sensitive": true,
			},
		}

		resp, err := engine.AdvancedTextSearch(ctx, req)
		if err != nil {
			t.Fatalf("AdvancedTextSearch failed: %v", err)
		}

		result := resp.Content[0].Text
		// Should only find exact "hello" match
		if !strings.Contains(result, "No matches") {
			matchCount := strings.Count(result, "test.txt:")
			if matchCount != 1 {
				t.Errorf("Expected exactly 1 match for case-sensitive 'hello', got %d. Result: %s", matchCount, result)
			}
		}
	})
}

// TestAdvancedTextSearchWithContext tests advanced_text_search with include_context parameter
func TestAdvancedTextSearchWithContext(t *testing.T) {
	testDir := t.TempDir()

	testFile := filepath.Join(testDir, "test.txt")
	content := "line 1\nline 2\nMATCH HERE\nline 4\nline 5\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	engine := createTestEngineForBug9(t)
	ctx := context.Background()

	t.Run("AdvancedTextSearch without context", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":            testDir,
				"pattern":         "MATCH",
				"include_context": false,
			},
		}

		resp, err := engine.AdvancedTextSearch(ctx, req)
		if err != nil {
			t.Fatalf("AdvancedTextSearch failed: %v", err)
		}

		result := resp.Content[0].Text
		// Should not include "Context:" section
		if strings.Contains(result, "Context:") {
			t.Errorf("Should not include context when include_context=false, got: %s", result)
		}
	})

	t.Run("AdvancedTextSearch with context", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":            testDir,
				"pattern":         "MATCH",
				"include_context": true,
				"context_lines":   2,
			},
		}

		resp, err := engine.AdvancedTextSearch(ctx, req)
		if err != nil {
			t.Fatalf("AdvancedTextSearch failed: %v", err)
		}

		result := resp.Content[0].Text
		// Should include Context section with surrounding lines
		if !strings.Contains(result, "Context:") {
			t.Errorf("Expected to include context when include_context=true, got: %s", result)
		}

		// Should include lines 2 and 4 as context (2 lines before and after)
		if !strings.Contains(result, "line 2") || !strings.Contains(result, "line 4") {
			t.Errorf("Expected context lines 2 and 4, got: %s", result)
		}
	})
}
