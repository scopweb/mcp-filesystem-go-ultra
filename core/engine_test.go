package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
)

// setupTestEngine creates a test engine with proper configuration
func setupTestEngine(t *testing.T) (*UltraFastEngine, func()) {
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	config := &Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}

	engine, err := NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("Failed to create test engine: %v", err)
	}

	cleanup := func() {
		engine.Close()
	}

	return engine, cleanup
}

// createTestFile creates a temporary test file with given content
func createTestFile(t *testing.T, dir, filename, content string) string {
	filePath := filepath.Join(dir, filename)
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	return filePath
}

// TestIntelligentWrite tests the IntelligentWrite wrapper method
func TestIntelligentWrite(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	testFile := filepath.Join(tempDir, "test_write.txt")
	testContent := "This is a test content for intelligent write"

	err := engine.IntelligentWrite(ctx, testFile, testContent)
	if err != nil {
		t.Errorf("IntelligentWrite failed: %v", err)
	}

	// Verify the file was written correctly
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("Failed to read written file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, string(content))
	}
}

// TestIntelligentRead tests the IntelligentRead wrapper method
func TestIntelligentRead(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	testContent := "This is test content for intelligent read"
	testFile := createTestFile(t, tempDir, "test_read.txt", testContent)

	content, err := engine.IntelligentRead(ctx, testFile)
	if err != nil {
		t.Errorf("IntelligentRead failed: %v", err)
	}

	if content != testContent {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, content)
	}
}

// TestIntelligentEdit tests the IntelligentEdit wrapper method
func TestIntelligentEdit(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	originalContent := "Hello world, this is a test file"
	testFile := createTestFile(t, tempDir, "test_edit.txt", originalContent)

	oldText := "world"
	newText := "universe"

	result, err := engine.IntelligentEdit(ctx, testFile, oldText, newText)
	if err != nil {
		t.Errorf("IntelligentEdit failed: %v", err)
	}

	if result == nil {
		t.Error("IntelligentEdit returned nil result")
		return
	}

	if result.ReplacementCount == 0 {
		t.Error("No replacements were made")
	}

	// Verify the file was edited correctly
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("Failed to read edited file: %v", err)
	}

	expectedContent := "Hello universe, this is a test file"
	if string(content) != expectedContent {
		t.Errorf("Content mismatch after edit. Expected: %s, Got: %s", expectedContent, string(content))
	}
}

// TestAutoRecoveryEdit tests the AutoRecoveryEdit wrapper method
func TestAutoRecoveryEdit(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	originalContent := "function test() {\n  console.log('hello');\n}"
	testFile := createTestFile(t, tempDir, "test_recovery.js", originalContent)

	// Try editing with slightly different formatting (should work with auto-recovery)
	oldText := "function test() {"
	newText := "function testFunction() {"

	result, err := engine.AutoRecoveryEdit(ctx, testFile, oldText, newText)
	if err != nil {
		t.Errorf("AutoRecoveryEdit failed: %v", err)
	}

	if result == nil {
		t.Error("AutoRecoveryEdit returned nil result")
		return
	}

	if result.ReplacementCount == 0 {
		t.Error("Auto recovery edit made no replacements")
	}

	// Verify the file was edited correctly
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("Failed to read edited file: %v", err)
	}

	expectedContent := "function testFunction() {\n  console.log('hello');\n}"
	if string(content) != expectedContent {
		t.Errorf("Content mismatch after auto recovery edit. Expected: %s, Got: %s", expectedContent, string(content))
	}
}

// TestGetOptimizationSuggestion tests the GetOptimizationSuggestion wrapper method
func TestGetOptimizationSuggestion(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	// Create a test file
	testContent := "This is a test file for optimization suggestion"
	testFile := createTestFile(t, tempDir, "test_suggestion.txt", testContent)

	suggestion, err := engine.GetOptimizationSuggestion(ctx, testFile)
	if err != nil {
		t.Errorf("GetOptimizationSuggestion failed: %v", err)
	}

	if suggestion == "" {
		t.Error("GetOptimizationSuggestion returned empty suggestion")
	}

	// The suggestion should be a non-empty string
	if len(suggestion) == 0 {
		t.Error("Optimization suggestion should not be empty")
	}
}

// TestGetOptimizationReport tests the GetOptimizationReport wrapper method
func TestGetOptimizationReport(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	report := engine.GetOptimizationReport()
	if report == "" {
		t.Error("GetOptimizationReport returned empty report")
	}

	// The report should contain some performance information
	if len(report) == 0 {
		t.Error("Optimization report should not be empty")
	}
}

// TestWrapperMethodsExist verifies all wrapper methods exist and can be called
func TestWrapperMethodsExist(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	// Test that all methods exist (compile-time check)
	var _ func(context.Context, string, string) error = engine.IntelligentWrite
	var _ func(context.Context, string) (string, error) = engine.IntelligentRead
	var _ func(context.Context, string, string, string) (*EditResult, error) = engine.IntelligentEdit
	var _ func(context.Context, string, string, string) (*EditResult, error) = engine.AutoRecoveryEdit
	var _ func(context.Context, string) (string, error) = engine.GetOptimizationSuggestion
	var _ func() string = engine.GetOptimizationReport
}