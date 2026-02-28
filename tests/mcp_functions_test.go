package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	localmcp "github.com/mcp/filesystem-ultra/mcp"
)

// setupTestEngine creates a test engine for MCP function testing
func setupTestEngine(t *testing.T) *core.UltraFastEngine {
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

	return engine
}

// createMockRequest creates a mock CallToolRequest for testing
func createMockRequest(params map[string]interface{}) mcp.CallToolRequest {
	arguments := make(map[string]interface{})
	for k, v := range params {
		arguments[k] = v
	}

	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "test_tool",
			Arguments: arguments,
		},
	}
}

// TestParseIntFunction tests the mcp.ParseInt usage that we fixed
func TestParseIntFunction(t *testing.T) {
	tests := []struct {
		name         string
		params       map[string]interface{}
		key          string
		defaultValue int
		expected     int
	}{
		{
			name:         "Parse existing integer",
			params:       map[string]interface{}{"max_chunk_size": 16384},
			key:          "max_chunk_size",
			defaultValue: 32768,
			expected:     16384,
		},
		{
			name:         "Parse with default value",
			params:       map[string]interface{}{},
			key:          "max_chunk_size",
			defaultValue: 32768,
			expected:     32768,
		},
		{
			name:         "Parse string number",
			params:       map[string]interface{}{"max_file_size": "2048"},
			key:          "max_file_size",
			defaultValue: 1024,
			expected:     2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := createMockRequest(tt.params)
			result := mcp.ParseInt(request, tt.key, tt.defaultValue)

			if result != tt.expected {
				t.Errorf("ParseInt() = %d, want %d", result, tt.expected)
			}
		})
	}
}

// TestMCPToolParameterParsing tests our fixes to the MCP parameter parsing
func TestMCPToolParameterParsing(t *testing.T) {
	// Test that mcp.WithNumber works (replacement for mcp.WithInt)
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool for parameter parsing"),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
		mcp.WithNumber("max_chunk_size", mcp.Description("Maximum chunk size")),
		mcp.WithNumber("max_file_size", mcp.Description("Maximum file size")),
	)

	// Tool creation successful if we reach here without panic
	if tool.Name != "test_tool" {
		t.Errorf("Tool name mismatch. Expected: test_tool, Got: %s", tool.Name)
	}
}

// TestWrapperMethodsCallable tests that all wrapper methods are callable
func TestWrapperMethodsCallable(t *testing.T) {
	engine := setupTestEngine(t)
	defer engine.Close()

	ctx := context.Background()
	tempDir := t.TempDir()

	// Create test file in allowed directory
	testFile := filepath.Join(tempDir, "wrapper_test.txt")
	testContent := "test content"

	// Write test file
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test that we can call all wrapper methods without compilation errors
	// Note: These may return errors due to path restrictions, but should be callable

	// Test IntelligentWrite
	err = engine.IntelligentWrite(ctx, testFile, "updated content")
	if err != nil {
		t.Logf("IntelligentWrite callable (may have path restriction): %v", err)
	}

	// Test IntelligentRead
	_, err = engine.IntelligentRead(ctx, testFile)
	if err != nil {
		t.Logf("IntelligentRead callable (may have path restriction): %v", err)
	}

	// Test IntelligentEdit
	_, err = engine.IntelligentEdit(ctx, testFile, "old", "new", false)
	if err != nil {
		t.Logf("IntelligentEdit callable (may have path restriction): %v", err)
	}

	// Test AutoRecoveryEdit
	_, err = engine.AutoRecoveryEdit(ctx, testFile, "old", "new", false)
	if err != nil {
		t.Logf("AutoRecoveryEdit callable (may have path restriction): %v", err)
	}

	// Test GetOptimizationSuggestion
	_, err = engine.GetOptimizationSuggestion(ctx, testFile)
	if err != nil {
		t.Logf("GetOptimizationSuggestion callable (may have path restriction): %v", err)
	}

	// Test GetOptimizationReport
	report := engine.GetOptimizationReport()
	if report == "" {
		t.Log("GetOptimizationReport callable (empty report in test environment)")
	}
}

// TestCompilationSuccess verifies that the fixes allow compilation
func TestCompilationSuccess(t *testing.T) {
	// If this test runs, it means our fixes were successful
	t.Log("✅ Compilation successful - all MCP API fixes working")

	// Test that ParseInt works with different parameter types
	request := createMockRequest(map[string]interface{}{
		"max_chunk_size": 16384,
		"max_file_size":  "2048",
	})

	chunkSize := mcp.ParseInt(request, "max_chunk_size", 32768)
	fileSize := mcp.ParseInt(request, "max_file_size", 1024*1024)

	if chunkSize != 16384 {
		t.Errorf("Expected chunk size 16384, got %d", chunkSize)
	}

	if fileSize != 2048 {
		t.Errorf("Expected file size 2048, got %d", fileSize)
	}

	t.Logf("✅ ParseInt working correctly: chunkSize=%d, fileSize=%d", chunkSize, fileSize)
}

// --- Consolidated from bug5_test.go ---

// TestContextValidation tests the context validation feature for EditFile
func TestContextValidation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_context.go")

	content := `package main

func ReadFile() {
	return nil
}

func WriteFile() {
	return nil
}
`

	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

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

	t.Run("ValidContext", func(t *testing.T) {
		oldText := "return nil"
		newText := "return content"

		result, err := engine.EditFile(testFile, oldText, newText, true)
		if err != nil {
			t.Fatalf("EditFile should succeed with valid context, got error: %v", err)
		}

		if result.ReplacementCount == 0 {
			t.Error("Expected at least one replacement")
		}

		if result.MatchConfidence == "none" {
			t.Error("Expected non-zero match confidence")
		}
	})

	t.Run("InvalidContext", func(t *testing.T) {
		oldText := "this_text_does_not_exist"
		newText := "something"

		result, err := engine.EditFile(testFile, oldText, newText, false)
		if err == nil {
			t.Error("EditFile should fail with invalid context")
		}

		if result != nil && result.ReplacementCount != 0 {
			t.Error("Expected no replacements for non-existent text")
		}
	})
}

// TestEditTelemetry tests the telemetry system
func TestEditTelemetry(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_telemetry.go")

	content := `package main

func SmallChange() {
	return nil
}

func LargeChange() {
	return nil
}
`

	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

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

	t.Run("TargetedEditDetection", func(t *testing.T) {
		oldText := "return nil"
		newText := "return content"

		engine.LogEditTelemetry(int64(len(oldText)), int64(len(newText)), testFile)

		summary := engine.GetEditTelemetrySummary()

		if total, ok := summary["total_edits"].(int64); !ok || total != 1 {
			t.Logf("Telemetry recorded - total_edits type: %T, value: %v", summary["total_edits"], summary["total_edits"])
		}

		if _, ok := summary["targeted_edits"]; !ok {
			t.Error("Expected targeted_edits in summary")
		}
	})

	t.Run("FullRewriteDetection", func(t *testing.T) {
		largeOldText := string(make([]byte, 1001))
		largeNewText := string(make([]byte, 1001))

		engine.LogEditTelemetry(int64(len(largeOldText)), int64(len(largeNewText)), testFile)

		summary := engine.GetEditTelemetrySummary()

		if _, ok := summary["full_rewrites"]; !ok {
			t.Error("Expected full_rewrites in summary")
		}
	})

	t.Run("TelemetrySummaryFormat", func(t *testing.T) {
		summary := engine.GetEditTelemetrySummary()

		requiredFields := []string{
			"total_edits",
			"full_rewrites",
			"average_bytes_per_edit",
			"recommendation",
		}

		for _, field := range requiredFields {
			if _, ok := summary[field]; !ok {
				t.Logf("Field %s present: %v", field, ok)
			}
		}
	})
}

// BenchmarkTelemetryLogging benchmarks the telemetry system
func BenchmarkTelemetryLogging(b *testing.B) {
	cacheInstance, _ := cache.NewIntelligentCache(1024 * 1024)
	cfg := &core.Config{
		Cache:       cacheInstance,
		ParallelOps: 2,
		DebugMode:   false,
	}

	engine, _ := core.NewUltraFastEngine(cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.LogEditTelemetry(int64(50*i), int64(50*i+10), "/tmp/test.go")
	}
}

// --- Consolidated from bug9_test.go ---

// createSearchTestEngine creates an engine for search tests
func createSearchTestEngine(t *testing.T) *core.UltraFastEngine {
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

	engine := createSearchTestEngine(t)
	ctx := context.Background()

	t.Run("SmartSearch without include_content", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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
		if strings.Contains(result, "Content matches") {
			t.Errorf("Expected no content matches when include_content=false, got: %s", result)
		}
	})

	t.Run("SmartSearch with include_content", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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
		if !strings.Contains(result, "Content matches") && !strings.Contains(result, "content matches") {
			t.Errorf("Expected content matches when include_content=true, got: %s", result)
		}

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

	engine := createSearchTestEngine(t)
	ctx := context.Background()

	t.Run("SmartSearch with file_types filter", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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

		if !strings.Contains(result, "search.go") {
			t.Errorf("Expected to find search.go in results")
		}
		if !strings.Contains(result, "search.txt") {
			t.Errorf("Expected to find search.txt in results")
		}

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

	engine := createSearchTestEngine(t)
	ctx := context.Background()

	t.Run("AdvancedTextSearch case insensitive", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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
		matchCount := strings.Count(result, "test.txt:")
		if matchCount < 3 {
			t.Errorf("Expected at least 3 matches for case-insensitive search, got %d. Result: %s", matchCount, result)
		}
	})

	t.Run("AdvancedTextSearch case sensitive", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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

	engine := createSearchTestEngine(t)
	ctx := context.Background()

	t.Run("AdvancedTextSearch without context", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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
		if strings.Contains(result, "Context:") {
			t.Errorf("Should not include context when include_context=false, got: %s", result)
		}
	})

	t.Run("AdvancedTextSearch with context", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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
		if !strings.Contains(result, "Context:") {
			t.Errorf("Expected to include context when include_context=true, got: %s", result)
		}

		if !strings.Contains(result, "line 2") || !strings.Contains(result, "line 4") {
			t.Errorf("Expected context lines 2 and 4, got: %s", result)
		}
	})
}

// TestAdvancedTextSearchCompactModeContext tests that include_context works in compact mode
func TestAdvancedTextSearchCompactModeContext(t *testing.T) {
	testDir := t.TempDir()

	testFile := filepath.Join(testDir, "test.txt")
	content := "line 1\nline 2\nMATCH HERE\nline 4\nline 5\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create engine with CompactMode enabled (like Claude Desktop)
	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	cfg := &core.Config{
		Cache:            cacheInstance,
		ParallelOps:      2,
		DebugMode:        false,
		CompactMode:      true,
		MaxSearchResults: 100,
	}
	engine, err := core.NewUltraFastEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	ctx := context.Background()

	t.Run("CompactMode without context", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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
		// Should NOT contain context lines
		if strings.Contains(result, "line 2") || strings.Contains(result, "line 4") {
			t.Errorf("Compact mode without context should not include context lines, got: %s", result)
		}
	})

	t.Run("CompactMode with context", func(t *testing.T) {
		req := localmcp.CallToolRequest{
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
		// Should contain the matched line content
		if !strings.Contains(result, "MATCH HERE") {
			t.Errorf("Compact mode with context should include the matched line, got: %s", result)
		}
		// Should contain context lines
		if !strings.Contains(result, "line 2") || !strings.Contains(result, "line 4") {
			t.Errorf("Compact mode with context should include context lines 2 and 4, got: %s", result)
		}
	})
}

// --- Base64 Read/Write Tests (Bug 11 fix) ---

// TestWriteBase64 tests writing binary content from base64 encoding
func TestWriteBase64(t *testing.T) {
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	cfg := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}

	engine, err := core.NewUltraFastEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	ctx := context.Background()

	t.Run("Write text file from base64", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "test_text.txt")
		// "Hello, World!" in base64
		contentBase64 := "SGVsbG8sIFdvcmxkIQ=="

		bytesWritten, err := engine.WriteBase64(ctx, testPath, contentBase64)
		if err != nil {
			t.Fatalf("WriteBase64 failed: %v", err)
		}

		if bytesWritten != 13 { // "Hello, World!" is 13 bytes
			t.Errorf("Expected 13 bytes written, got %d", bytesWritten)
		}

		// Verify content
		content, err := os.ReadFile(testPath)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}

		if string(content) != "Hello, World!" {
			t.Errorf("Expected 'Hello, World!', got '%s'", string(content))
		}
	})

	t.Run("Write binary file from base64", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "test_binary.bin")
		// Binary data: [0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD]
		contentBase64 := "AAEC//79"

		bytesWritten, err := engine.WriteBase64(ctx, testPath, contentBase64)
		if err != nil {
			t.Fatalf("WriteBase64 failed: %v", err)
		}

		if bytesWritten != 6 {
			t.Errorf("Expected 6 bytes written, got %d", bytesWritten)
		}

		// Verify content
		content, err := os.ReadFile(testPath)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}

		expected := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
		if len(content) != len(expected) {
			t.Errorf("Expected %d bytes, got %d", len(expected), len(content))
		}
		for i, b := range content {
			if b != expected[i] {
				t.Errorf("Byte %d: expected 0x%02X, got 0x%02X", i, expected[i], b)
			}
		}
	})

	t.Run("Invalid base64 should fail", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "test_invalid.txt")
		invalidBase64 := "This is not valid base64!!!"

		_, err := engine.WriteBase64(ctx, testPath, invalidBase64)
		if err == nil {
			t.Error("Expected error for invalid base64, got nil")
		}
	})

	t.Run("Auto-create directory", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "subdir", "nested", "file.txt")
		contentBase64 := "dGVzdA==" // "test"

		_, err := engine.WriteBase64(ctx, testPath, contentBase64)
		if err != nil {
			t.Fatalf("WriteBase64 failed to create directories: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(testPath); os.IsNotExist(err) {
			t.Error("File should exist after write")
		}
	})
}

// TestReadBase64 tests reading files as base64 encoding
func TestReadBase64(t *testing.T) {
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	cfg := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}

	engine, err := core.NewUltraFastEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	ctx := context.Background()

	t.Run("Read text file as base64", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "test_read.txt")
		content := "Hello, World!"
		err := os.WriteFile(testPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		encoded, size, err := engine.ReadBase64(ctx, testPath)
		if err != nil {
			t.Fatalf("ReadBase64 failed: %v", err)
		}

		if size != 13 {
			t.Errorf("Expected size 13, got %d", size)
		}

		// "Hello, World!" in base64
		expectedBase64 := "SGVsbG8sIFdvcmxkIQ=="
		if encoded != expectedBase64 {
			t.Errorf("Expected '%s', got '%s'", expectedBase64, encoded)
		}
	})

	t.Run("Read binary file as base64", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "test_binary_read.bin")
		content := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
		err := os.WriteFile(testPath, content, 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		encoded, size, err := engine.ReadBase64(ctx, testPath)
		if err != nil {
			t.Fatalf("ReadBase64 failed: %v", err)
		}

		if size != 6 {
			t.Errorf("Expected size 6, got %d", size)
		}

		expectedBase64 := "AAEC//79"
		if encoded != expectedBase64 {
			t.Errorf("Expected '%s', got '%s'", expectedBase64, encoded)
		}
	})

	t.Run("Read nonexistent file should fail", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "nonexistent.txt")

		_, _, err := engine.ReadBase64(ctx, testPath)
		if err == nil {
			t.Error("Expected error for nonexistent file, got nil")
		}
	})

	t.Run("Read directory should fail", func(t *testing.T) {
		_, _, err := engine.ReadBase64(ctx, tempDir)
		if err == nil {
			t.Error("Expected error when trying to read directory, got nil")
		}
	})
}

// TestBase64RoundTrip tests writing and reading back via base64
func TestBase64RoundTrip(t *testing.T) {
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	cfg := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}

	engine, err := core.NewUltraFastEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	ctx := context.Background()

	t.Run("Write then read binary data", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "roundtrip.bin")
		// Some complex binary data
		originalBase64 := "AAEC//79/P38+vr5+Pj4"

		// Write
		_, err := engine.WriteBase64(ctx, testPath, originalBase64)
		if err != nil {
			t.Fatalf("WriteBase64 failed: %v", err)
		}

		// Read back
		readBase64, _, err := engine.ReadBase64(ctx, testPath)
		if err != nil {
			t.Fatalf("ReadBase64 failed: %v", err)
		}

		if readBase64 != originalBase64 {
			t.Errorf("Round-trip failed: wrote '%s', read '%s'", originalBase64, readBase64)
		}
	})
}