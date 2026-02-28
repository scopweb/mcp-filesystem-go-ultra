package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestLargeFileProcessor_ProcessInMemory tests in-memory processing for small files
func TestLargeFileProcessor_ProcessInMemory(t *testing.T) {
	// Setup
	engine := setupLargeFileTestEngine(t)
	processor := core.NewLargeFileProcessor(engine)

	// Create test file (1KB)
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "small_test.txt")
	content := strings.Repeat("Line test content\n", 50) // ~1KB
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test processing function - uppercase transformation
	processFunc := func(content string, meta core.ProcessMetadata) (string, error) {
		return strings.ToUpper(content), nil
	}

	// Process file
	ctx := context.Background()
	result, err := processor.ProcessFile(ctx, core.ProcessingConfig{
		InputPath:    testFile,
		OutputPath:   testFile,
		Mode:         core.ModeFullFile,
		ProcessFunc:  processFunc,
		CreateBackup: true,
		DryRun:       false,
	})

	// Verify
	if err != nil {
		t.Fatalf("ProcessFile failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.Mode != "in-memory" {
		t.Errorf("Expected mode=in-memory, got %s", result.Mode)
	}
	if result.BytesProcessed <= 0 {
		t.Error("Expected BytesProcessed > 0")
	}
	if result.BackupID == "" {
		t.Error("Expected backup ID")
	}

	// Verify file was transformed
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read processed file: %v", err)
	}
	if !strings.Contains(string(processedContent), "LINE TEST CONTENT") {
		t.Error("Content was not transformed to uppercase")
	}
}

// TestLargeFileProcessor_ProcessLineByLine tests streaming line-by-line processing
func TestLargeFileProcessor_ProcessLineByLine(t *testing.T) {
	// Setup
	engine := setupLargeFileTestEngine(t)
	processor := core.NewLargeFileProcessor(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "medium_test.txt")
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, "Test line number "+string(rune(i)))
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test processing function - add prefix to each line
	lineCount := 0
	processFunc := func(content string, meta core.ProcessMetadata) (string, error) {
		lineCount++
		return "PROCESSED: " + content, nil
	}

	// Process file in line-by-line mode
	ctx := context.Background()
	result, err := processor.ProcessFile(ctx, core.ProcessingConfig{
		InputPath:    testFile,
		OutputPath:   testFile,
		Mode:         core.ModeLineByLine,
		ProcessFunc:  processFunc,
		CreateBackup: false,
		DryRun:       false,
	})

	// Verify
	if err != nil {
		t.Fatalf("ProcessFile failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.Mode != "streaming-line" {
		t.Errorf("Expected mode=streaming-line, got %s", result.Mode)
	}
	// Allow 1000 or 1001 lines (depending on trailing newline)
	if result.LinesProcessed < 1000 || result.LinesProcessed > 1001 {
		t.Errorf("Expected ~1000 lines processed, got %d", result.LinesProcessed)
	}

	// Verify file was transformed
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read processed file: %v", err)
	}
	if !strings.Contains(string(processedContent), "PROCESSED: Test line") {
		t.Error("Lines were not prefixed with PROCESSED:")
	}
}

// TestLargeFileProcessor_DryRun tests dry-run mode (no file modification)
func TestLargeFileProcessor_DryRun(t *testing.T) {
	// Setup
	engine := setupLargeFileTestEngine(t)
	processor := core.NewLargeFileProcessor(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "dryrun_test.txt")
	originalContent := "Original content"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Process with dry run
	processFunc := func(content string, meta core.ProcessMetadata) (string, error) {
		return "MODIFIED CONTENT", nil
	}

	ctx := context.Background()
	result, err := processor.ProcessFile(ctx, core.ProcessingConfig{
		InputPath:    testFile,
		OutputPath:   testFile,
		Mode:         core.ModeFullFile,
		ProcessFunc:  processFunc,
		CreateBackup: false,
		DryRun:       true,
	})

	// Verify
	if err != nil {
		t.Fatalf("ProcessFile failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}

	// Verify file was NOT modified
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(content) != originalContent {
		t.Errorf("File was modified in dry-run mode. Expected %q, got %q", originalContent, string(content))
	}
}

// TestLargeFileProcessor_ContextCancellation tests context cancellation
func TestLargeFileProcessor_ContextCancellation(t *testing.T) {
	// Setup
	engine := setupLargeFileTestEngine(t)
	processor := core.NewLargeFileProcessor(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "cancel_test.txt")
	content := strings.Repeat("Line\n", 10000) // Many lines
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Process function that cancels after first call
	callCount := 0
	processFunc := func(content string, meta core.ProcessMetadata) (string, error) {
		callCount++
		if callCount == 1 {
			cancel() // Cancel context
		}
		time.Sleep(10 * time.Millisecond) // Simulate slow processing
		return content, nil
	}

	// Process file
	_, err := processor.ProcessFile(ctx, core.ProcessingConfig{
		InputPath:    testFile,
		OutputPath:   testFile,
		Mode:         core.ModeLineByLine,
		ProcessFunc:  processFunc,
		CreateBackup: false,
		DryRun:       false,
	})

	// Verify cancellation was detected
	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
	if !strings.Contains(err.Error(), "cancel") {
		t.Errorf("Expected cancellation error, got: %v", err)
	}
}

// TestLargeFileProcessor_ModeAutoSelection tests automatic mode selection
func TestLargeFileProcessor_ModeAutoSelection(t *testing.T) {
	// Setup
	engine := setupLargeFileTestEngine(t)
	processor := core.NewLargeFileProcessor(engine)
	testDir := t.TempDir()

	tests := []struct {
		name         string
		fileSize     int
		expectedMode string
	}{
		{"Small file (1KB)", 1024, "in-memory"},
		{"Medium file (100KB)", 100 * 1024, "in-memory"},
		{"Large file (5MB)", 5 * 1024 * 1024, "in-memory"},
		{"Very large file (15MB)", 15 * 1024 * 1024, "streaming-line"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(testDir, "test_"+tt.name+".txt")
			content := strings.Repeat("x", tt.fileSize)
			if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			processFunc := func(content string, meta core.ProcessMetadata) (string, error) {
				return content, nil
			}

			ctx := context.Background()
			result, err := processor.ProcessFile(ctx, core.ProcessingConfig{
				InputPath:    testFile,
				OutputPath:   testFile,
				Mode:         core.ModeAuto, // Auto-select
				ProcessFunc:  processFunc,
				CreateBackup: false,
				DryRun:       true,
			})

			if err != nil {
				t.Fatalf("ProcessFile failed: %v", err)
			}
			if result.Mode != tt.expectedMode {
				t.Errorf("Expected mode=%s, got %s", tt.expectedMode, result.Mode)
			}
		})
	}
}

// TestLargeFileProcessor_ErrorHandling tests error handling
func TestLargeFileProcessor_ErrorHandling(t *testing.T) {
	// Setup
	engine := setupLargeFileTestEngine(t)
	processor := core.NewLargeFileProcessor(engine)

	tests := []struct {
		name        string
		setupFunc   func() string
		expectError bool
	}{
		{
			name: "Nonexistent file",
			setupFunc: func() string {
				return "/nonexistent/file.txt"
			},
			expectError: true,
		},
		{
			name: "Directory instead of file",
			setupFunc: func() string {
				dir := t.TempDir()
				return dir
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := tt.setupFunc()

			processFunc := func(content string, meta core.ProcessMetadata) (string, error) {
				return content, nil
			}

			ctx := context.Background()
			_, err := processor.ProcessFile(ctx, core.ProcessingConfig{
				InputPath:   filePath,
				OutputPath:  filePath,
				Mode:        core.ModeAuto,
				ProcessFunc: processFunc,
			})

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// setupLargeFileTestEngine creates a test engine instance
func setupLargeFileTestEngine(t *testing.T) *core.UltraFastEngine {
	cacheSystem, err := cache.NewIntelligentCache(10 * 1024 * 1024) // 10MB cache
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:       cacheSystem,
		ParallelOps: 4,
		BackupDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	return engine
}
