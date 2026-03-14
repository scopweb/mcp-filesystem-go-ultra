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

func setupCompareEngine(t *testing.T) (*core.UltraFastEngine, string) {
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

func TestCompareFiles_Different(t *testing.T) {
	engine, tempDir := setupCompareEngine(t)
	ctx := context.Background()

	fileA := filepath.Join(tempDir, "fileA.txt")
	fileB := filepath.Join(tempDir, "fileB.txt")

	contentA := "line1\nline2\nline3\nline4\n"
	contentB := "line1\nline2_modified\nline3\nline5\n"

	os.WriteFile(fileA, []byte(contentA), 0644)
	os.WriteFile(fileB, []byte(contentB), 0644)

	analysis, err := engine.CompareFiles(ctx, fileA, fileB)
	if err != nil {
		t.Fatalf("CompareFiles failed: %v", err)
	}

	if analysis.OperationType != "compare" {
		t.Errorf("Expected operation type 'compare', got '%s'", analysis.OperationType)
	}
	if analysis.RiskLevel != "low" {
		t.Errorf("Expected risk level 'low', got '%s'", analysis.RiskLevel)
	}
	if analysis.LinesModified == 0 {
		t.Error("Expected lines modified > 0")
	}
	if !strings.Contains(analysis.Preview, "---") {
		t.Error("Expected diff preview with file header")
	}
	if !strings.Contains(analysis.Preview, "line2_modified") {
		t.Errorf("Expected diff to show 'line2_modified', preview:\n%s", analysis.Preview)
	}
	if !strings.Contains(analysis.Impact, "differ") {
		t.Errorf("Expected impact to mention differences, got: %s", analysis.Impact)
	}

	t.Logf("Compare result: %s", analysis.Impact)
	t.Logf("Preview:\n%s", analysis.Preview)
}

func TestCompareFiles_Identical(t *testing.T) {
	engine, tempDir := setupCompareEngine(t)
	ctx := context.Background()

	fileA := filepath.Join(tempDir, "fileA.txt")
	fileB := filepath.Join(tempDir, "fileB.txt")

	content := "same content\nline2\nline3\n"

	os.WriteFile(fileA, []byte(content), 0644)
	os.WriteFile(fileB, []byte(content), 0644)

	analysis, err := engine.CompareFiles(ctx, fileA, fileB)
	if err != nil {
		t.Fatalf("CompareFiles failed: %v", err)
	}

	if analysis.LinesModified != 0 {
		t.Errorf("Expected 0 lines modified for identical files, got %d", analysis.LinesModified)
	}
	if !strings.Contains(analysis.Preview, "identical") {
		t.Errorf("Expected preview to mention 'identical', got: %s", analysis.Preview)
	}
	if !strings.Contains(analysis.Impact, "identical") {
		t.Errorf("Expected impact to mention 'identical', got: %s", analysis.Impact)
	}

	t.Logf("Identical files result: %s", analysis.Impact)
}

func TestCompareFiles_FileNotFound(t *testing.T) {
	engine, tempDir := setupCompareEngine(t)
	ctx := context.Background()

	fileA := filepath.Join(tempDir, "exists.txt")
	fileB := filepath.Join(tempDir, "does_not_exist.txt")

	os.WriteFile(fileA, []byte("content"), 0644)

	_, err := engine.CompareFiles(ctx, fileA, fileB)
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "file B") {
		t.Errorf("Expected error to mention 'file B', got: %v", err)
	}
}

func TestCompareFiles_AccessDenied(t *testing.T) {
	engine, _ := setupCompareEngine(t)
	ctx := context.Background()

	// Paths outside allowed directory
	_, err := engine.CompareFiles(ctx, "/etc/passwd", "/etc/shadow")
	if err == nil {
		t.Fatal("Expected access denied error, got nil")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("Expected 'access denied' error, got: %v", err)
	}
}

func TestCompareFiles_LargerFiles(t *testing.T) {
	engine, tempDir := setupCompareEngine(t)
	ctx := context.Background()

	fileA := filepath.Join(tempDir, "large_a.txt")
	fileB := filepath.Join(tempDir, "large_b.txt")

	// Generate files with 100 lines, differing at specific points
	var linesA, linesB []string
	for i := 0; i < 100; i++ {
		linesA = append(linesA, "    <div class=\"row\">content line "+string(rune('A'+i%26))+"</div>")
		if i%10 == 0 {
			linesB = append(linesB, "    <div class=\"row modified\">changed line "+string(rune('A'+i%26))+"</div>")
		} else {
			linesB = append(linesB, linesA[i])
		}
	}

	os.WriteFile(fileA, []byte(strings.Join(linesA, "\n")), 0644)
	os.WriteFile(fileB, []byte(strings.Join(linesB, "\n")), 0644)

	analysis, err := engine.CompareFiles(ctx, fileA, fileB)
	if err != nil {
		t.Fatalf("CompareFiles failed: %v", err)
	}

	if analysis.LinesModified != 10 {
		t.Errorf("Expected 10 lines modified, got %d", analysis.LinesModified)
	}

	t.Logf("Large file compare: %s", analysis.Impact)
	t.Logf("Metadata: %v", analysis.Metadata)
}
