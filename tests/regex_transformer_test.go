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

// TestRegexTransformer_SimpleReplacement tests basic regex replacement
func TestRegexTransformer_SimpleReplacement(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "simple_test.txt")
	content := "Hello world\nHello universe\nGoodbye world"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform: Replace "Hello" with "Hi"
	ctx := context.Background()
	result, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     "Hello",
				Replacement: "Hi",
				Limit:       -1,
			},
		},
		Mode:         core.ModeSequential,
		CreateBackup: true,
		DryRun:       false,
	})

	// Verify
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.PatternsApplied != 1 {
		t.Errorf("Expected 1 pattern applied, got %d", result.PatternsApplied)
	}
	if result.TotalReplacements != 2 {
		t.Errorf("Expected 2 replacements, got %d", result.TotalReplacements)
	}

	// Verify file content
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	expected := "Hi world\nHi universe\nGoodbye world"
	if string(processedContent) != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, string(processedContent))
	}
}

// TestRegexTransformer_CaptureGroups tests capture group replacement
func TestRegexTransformer_CaptureGroups(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "capture_test.txt")
	content := "Name: John\nName: Alice\nName: Bob"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform: Extract name and add prefix
	ctx := context.Background()
	result, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     "Name: (\\w+)",
				Replacement: "Person: $1 Doe",
				Limit:       -1,
			},
		},
		Mode:         core.ModeSequential,
		CreateBackup: false,
		DryRun:       false,
	})

	// Verify
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.TotalReplacements != 3 {
		t.Errorf("Expected 3 replacements, got %d", result.TotalReplacements)
	}

	// Verify file content
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !strings.Contains(string(processedContent), "Person: John Doe") {
		t.Error("Expected capture group replacement 'Person: John Doe'")
	}
	if !strings.Contains(string(processedContent), "Person: Alice Doe") {
		t.Error("Expected capture group replacement 'Person: Alice Doe'")
	}
}

// TestRegexTransformer_MultiplePatterns tests applying multiple patterns sequentially
func TestRegexTransformer_MultiplePatterns(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "multi_test.txt")
	content := "foo bar baz"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform: Apply multiple patterns in sequence
	ctx := context.Background()
	result, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     "foo",
				Replacement: "FOO",
				Limit:       -1,
			},
			{
				Pattern:     "bar",
				Replacement: "BAR",
				Limit:       -1,
			},
			{
				Pattern:     "baz",
				Replacement: "BAZ",
				Limit:       -1,
			},
		},
		Mode:         core.ModeSequential,
		CreateBackup: false,
		DryRun:       false,
	})

	// Verify
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.PatternsApplied != 3 {
		t.Errorf("Expected 3 patterns applied, got %d", result.PatternsApplied)
	}

	// Verify file content
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	expected := "FOO BAR BAZ"
	if string(processedContent) != expected {
		t.Errorf("Expected: %s, Got: %s", expected, string(processedContent))
	}
}

// TestRegexTransformer_CaseInsensitive tests case-insensitive matching
func TestRegexTransformer_CaseInsensitive(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "case_test.txt")
	content := "Hello HELLO hello HeLLo"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform with case-insensitive matching
	ctx := context.Background()
	result, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     "hello",
				Replacement: "Hi",
				Limit:       -1,
			},
		},
		Mode:          core.ModeSequential,
		CaseSensitive: false, // Case-insensitive
		CreateBackup:  false,
		DryRun:        false,
	})

	// Verify
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.TotalReplacements != 4 {
		t.Errorf("Expected 4 replacements (all variations), got %d", result.TotalReplacements)
	}

	// Verify file content
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	expected := "Hi Hi Hi Hi"
	if string(processedContent) != expected {
		t.Errorf("Expected: %s, Got: %s", expected, string(processedContent))
	}
}

// TestRegexTransformer_LimitedReplacements tests limiting number of replacements
func TestRegexTransformer_LimitedReplacements(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "limit_test.txt")
	content := "test test test test test"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform with limit of 2 replacements
	ctx := context.Background()
	result, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     "test",
				Replacement: "TEST",
				Limit:       2, // Only replace first 2
			},
		},
		Mode:         core.ModeSequential,
		CreateBackup: false,
		DryRun:       false,
	})

	// Verify
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.TotalReplacements != 2 {
		t.Errorf("Expected 2 replacements, got %d", result.TotalReplacements)
	}

	// Verify file content
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	expected := "TEST TEST test test test"
	if string(processedContent) != expected {
		t.Errorf("Expected: %s, Got: %s", expected, string(processedContent))
	}
}

// TestRegexTransformer_DryRun tests dry-run mode
func TestRegexTransformer_DryRun(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "dryrun_test.txt")
	originalContent := "Original content"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform with dry-run
	ctx := context.Background()
	result, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     "Original",
				Replacement: "Modified",
				Limit:       -1,
			},
		},
		Mode:         core.ModeSequential,
		CreateBackup: false,
		DryRun:       true, // Dry run
	})

	// Verify
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
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
		t.Errorf("File was modified in dry-run mode. Expected: %s, Got: %s", originalContent, string(content))
	}
}

// TestRegexTransformer_ComplexPattern tests complex regex patterns (Divi-like)
func TestRegexTransformer_ComplexPattern(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file with JSON-like structure (similar to Divi)
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "complex_test.json")
	content := `{
  "admin_label": "Hero Section",
  "content": "]Welcome to our site[",
  "button_text": "]Click here[",
  "footer": "]Copyright 2024["
}`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform: Extract text between ][ and add prefix
	ctx := context.Background()
	result, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     `\]([^\[]+)\[`,
				Replacement: "]TRANSLATED: $1[",
				Limit:       -1,
			},
		},
		Mode:         core.ModeSequential,
		CreateBackup: false,
		DryRun:       false,
	})

	// Verify
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}
	if result.TotalReplacements != 3 {
		t.Errorf("Expected 3 replacements, got %d", result.TotalReplacements)
	}

	// Verify file content
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !strings.Contains(string(processedContent), "]TRANSLATED: Welcome to our site[") {
		t.Error("Expected translated text with capture group")
	}
	if !strings.Contains(string(processedContent), "]TRANSLATED: Click here[") {
		t.Error("Expected translated button text")
	}
}

// TestRegexTransformer_InvalidPattern tests error handling for invalid regex
func TestRegexTransformer_InvalidPattern(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "invalid_test.txt")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Transform with invalid regex pattern
	ctx := context.Background()
	_, err := transformer.Transform(ctx, core.RegexTransformConfig{
		FilePath: testFile,
		Patterns: []core.TransformPattern{
			{
				Pattern:     "[invalid(regex",
				Replacement: "replacement",
				Limit:       -1,
			},
		},
		Mode:         core.ModeSequential,
		CreateBackup: false,
		DryRun:       false,
	})

	// Verify error
	if err == nil {
		t.Error("Expected error for invalid regex pattern")
	}
	if !strings.Contains(err.Error(), "invalid pattern") {
		t.Errorf("Expected 'invalid pattern' error, got: %v", err)
	}
}

// TestRegexTransformer_QuickTransform tests convenience method
func TestRegexTransformer_QuickTransform(t *testing.T) {
	// Setup
	engine := setupRegexTestEngine(t)
	transformer := core.NewRegexTransformer(engine)

	// Create test file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "quick_test.txt")
	content := "quick test"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Quick transform
	ctx := context.Background()
	result, err := transformer.QuickTransform(ctx, testFile, "quick", "QUICK", true)

	// Verify
	if err != nil {
		t.Fatalf("QuickTransform failed: %v", err)
	}
	if !result.Success {
		t.Error("Expected success=true")
	}

	// Verify file content
	processedContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	expected := "QUICK test"
	if string(processedContent) != expected {
		t.Errorf("Expected: %s, Got: %s", expected, string(processedContent))
	}
}

// setupRegexTestEngine creates a test engine instance for regex tests
func setupRegexTestEngine(t *testing.T) *core.UltraFastEngine {
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
