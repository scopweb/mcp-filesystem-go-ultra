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

	result, err := engine.IntelligentEdit(ctx, testFile, oldText, newText, true)
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

	result, err := engine.AutoRecoveryEdit(ctx, testFile, oldText, newText, true)
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
	var _ func(context.Context, string, string, string, bool) (*EditResult, error) = engine.IntelligentEdit
	var _ func(context.Context, string, string, string, bool) (*EditResult, error) = engine.AutoRecoveryEdit
	var _ func(context.Context, string) (string, error) = engine.GetOptimizationSuggestion
	var _ func() string = engine.GetOptimizationReport
}

// TestCreateDirectory tests directory creation
func TestCreateDirectory(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	// Test creating a simple directory
	newDir := filepath.Join(tempDir, "test_dir")
	err := engine.CreateDirectory(ctx, newDir)
	if err != nil {
		t.Fatalf("CreateDirectory failed: %v", err)
	}

	// Verify directory was created
	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("Failed to stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Error("Created path is not a directory")
	}

	// Test creating nested directories
	nestedDir := filepath.Join(tempDir, "parent", "child", "grandchild")
	err = engine.CreateDirectory(ctx, nestedDir)
	if err != nil {
		t.Fatalf("CreateDirectory failed for nested path: %v", err)
	}

	// Verify nested directory was created
	info, err = os.Stat(nestedDir)
	if err != nil {
		t.Fatalf("Failed to stat nested directory: %v", err)
	}
	if !info.IsDir() {
		t.Error("Created nested path is not a directory")
	}

	// Test error when directory already exists
	err = engine.CreateDirectory(ctx, newDir)
	if err == nil {
		t.Error("Expected error when creating existing directory, got nil")
	}
}

// TestDeleteFile tests file and directory deletion
func TestDeleteFile(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	// Test deleting a file
	testFile := createTestFile(t, tempDir, "delete_test.txt", "test content")
	err := engine.DeleteFile(ctx, testFile)
	if err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	// Verify file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("File still exists after deletion")
	}

	// Test deleting a directory
	testDir := filepath.Join(tempDir, "delete_dir")
	os.MkdirAll(testDir, 0755)
	createTestFile(t, testDir, "file_in_dir.txt", "content")

	err = engine.DeleteFile(ctx, testDir)
	if err != nil {
		t.Fatalf("DeleteFile failed for directory: %v", err)
	}

	// Verify directory was deleted
	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Error("Directory still exists after deletion")
	}

	// Test error when file doesn't exist
	err = engine.DeleteFile(ctx, filepath.Join(tempDir, "nonexistent.txt"))
	if err == nil {
		t.Error("Expected error when deleting non-existent file, got nil")
	}
}

// TestMoveFile tests moving files and directories
func TestMoveFile(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	// Test moving a file
	testContent := "move test content"
	sourceFile := createTestFile(t, tempDir, "source.txt", testContent)
	destFile := filepath.Join(tempDir, "destination.txt")

	err := engine.MoveFile(ctx, sourceFile, destFile)
	if err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	// Verify source doesn't exist
	if _, err := os.Stat(sourceFile); !os.IsNotExist(err) {
		t.Error("Source file still exists after move")
	}

	// Verify destination exists with correct content
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, string(content))
	}

	// Test moving to a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	destFile2 := filepath.Join(subDir, "moved.txt")

	err = engine.MoveFile(ctx, destFile, destFile2)
	if err != nil {
		t.Fatalf("MoveFile to subdirectory failed: %v", err)
	}

	// Verify subdirectory was created and file exists there
	if _, err := os.Stat(destFile2); os.IsNotExist(err) {
		t.Error("File doesn't exist at destination after move to subdirectory")
	}
}

// TestCopyFile tests copying files and directories
func TestCopyFile(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	// Test copying a file
	testContent := "copy test content"
	sourceFile := createTestFile(t, tempDir, "source_copy.txt", testContent)
	destFile := filepath.Join(tempDir, "dest_copy.txt")

	err := engine.CopyFile(ctx, sourceFile, destFile)
	if err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Verify source still exists
	if _, err := os.Stat(sourceFile); os.IsNotExist(err) {
		t.Error("Source file doesn't exist after copy")
	}

	// Verify destination exists with correct content
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, string(content))
	}

	// Test copying a directory
	sourceDir := filepath.Join(tempDir, "source_dir")
	os.MkdirAll(sourceDir, 0755)
	createTestFile(t, sourceDir, "file1.txt", "content1")
	createTestFile(t, sourceDir, "file2.txt", "content2")

	// Create subdirectory
	subDir := filepath.Join(sourceDir, "subdir")
	os.MkdirAll(subDir, 0755)
	createTestFile(t, subDir, "file3.txt", "content3")

	destDir := filepath.Join(tempDir, "dest_dir")
	err = engine.CopyFile(ctx, sourceDir, destDir)
	if err != nil {
		t.Fatalf("CopyFile failed for directory: %v", err)
	}

	// Verify all files were copied
	files := []string{
		filepath.Join(destDir, "file1.txt"),
		filepath.Join(destDir, "file2.txt"),
		filepath.Join(destDir, "subdir", "file3.txt"),
	}

	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			t.Errorf("File %s doesn't exist after directory copy", file)
		}
	}
}

// TestGetFileInfo tests getting file information
func TestGetFileInfo(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	tempDir := t.TempDir()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, tempDir)

	// Test getting info for a file
	testContent := "file info test"
	testFile := createTestFile(t, tempDir, "info_test.txt", testContent)

	info, err := engine.GetFileInfo(ctx, testFile)
	if err != nil {
		t.Fatalf("GetFileInfo failed: %v", err)
	}

	if info == "" {
		t.Error("GetFileInfo returned empty string")
	}

	// Info should contain file name
	if !contains(info, "info_test.txt") {
		t.Error("File info doesn't contain file name")
	}

	// Test getting info for a directory
	testDir := filepath.Join(tempDir, "info_dir")
	os.MkdirAll(testDir, 0755)
	createTestFile(t, testDir, "file1.txt", "content")
	createTestFile(t, testDir, "file2.txt", "content")

	info, err = engine.GetFileInfo(ctx, testDir)
	if err != nil {
		t.Fatalf("GetFileInfo failed for directory: %v", err)
	}

	if info == "" {
		t.Error("GetFileInfo returned empty string for directory")
	}

	// Test error for non-existent file
	_, err = engine.GetFileInfo(ctx, filepath.Join(tempDir, "nonexistent.txt"))
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}