package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestBug8LineEndingNormalization tests that EditFile handles line ending differences correctly
func TestBug8LineEndingNormalization(t *testing.T) {
	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_bug8.cs")

	// Content with Windows line endings (CRLF)
	content := "line1\r\nline2\r\nline3\r\nline4\r\nline5"

	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create engine with cache
	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	cfg := &core.Config{
		Cache:       cacheInstance,
		ParallelOps: 2,
		DebugMode:   false,
	}

	engine, err := core.NewUltraFastEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	t.Run("EditWithMixedLineEndings", func(t *testing.T) {
		// oldText with Unix line endings (LF)
		oldText := "line2\nline3\nline4"
		newText := "line2_modified\nline3_modified\nline4_modified"

		// This should succeed now that we normalize line endings in validateEditContext
		result, err := engine.EditFile(testFile, oldText, newText)
		if err != nil {
			t.Fatalf("EditFile failed with mixed line endings: %v", err)
		}

		if result.ReplacementCount != 1 {
			t.Errorf("Expected 1 replacement, got %d", result.ReplacementCount)
		}

		// Verify content
		newContentBytes, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		newContent := string(newContentBytes)

		// The engine normalizes to LF internally during edit, so we expect LF in the modified part
		if !strings.Contains(newContent, "line2_modified") {
			t.Errorf("New content not found in file: %s", newContent)
		}
	})
}
