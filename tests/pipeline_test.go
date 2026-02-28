package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestPipeline_Validation tests pipeline request validation
func TestPipeline_Validation(t *testing.T) {
	testDir := t.TempDir()
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	tests := []struct {
		name        string
		request     core.PipelineRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "empty name",
			request: core.PipelineRequest{
				Name:  "",
				Steps: []core.PipelineStep{{ID: "test", Action: "search", Params: map[string]interface{}{"pattern": "test"}}},
			},
			expectError: true,
			errorMsg:    "name is required",
		},
		{
			name: "no steps",
			request: core.PipelineRequest{
				Name:  "test",
				Steps: []core.PipelineStep{},
			},
			expectError: true,
			errorMsg:    "at least one step is required",
		},
		{
			name: "duplicate step IDs",
			request: core.PipelineRequest{
				Name: "test",
				Steps: []core.PipelineStep{
					{ID: "step1", Action: "search", Params: map[string]interface{}{"pattern": "test"}},
					{ID: "step1", Action: "search", Params: map[string]interface{}{"pattern": "test"}},
				},
			},
			expectError: true,
			errorMsg:    "duplicate step ID",
		},
		{
			name: "invalid step ID format",
			request: core.PipelineRequest{
				Name: "test",
				Steps: []core.PipelineStep{
					{ID: "step$1", Action: "search", Params: map[string]interface{}{"pattern": "test"}},
				},
			},
			expectError: true,
			errorMsg:    "invalid step ID",
		},
		{
			name: "input_from references non-existent step",
			request: core.PipelineRequest{
				Name: "test",
				Steps: []core.PipelineStep{
					{ID: "step1", Action: "search", Params: map[string]interface{}{"pattern": "test"}},
					{ID: "step2", Action: "edit", InputFrom: "nonexistent", Params: map[string]interface{}{"old_text": "a", "new_text": "b"}},
				},
			},
			expectError: true,
			errorMsg:    "non-existent step",
		},
		{
			name: "forward reference in input_from",
			request: core.PipelineRequest{
				Name: "test",
				Steps: []core.PipelineStep{
					{ID: "step1", Action: "edit", InputFrom: "step2", Params: map[string]interface{}{"old_text": "a", "new_text": "b"}},
					{ID: "step2", Action: "search", Params: map[string]interface{}{"pattern": "test"}},
				},
			},
			expectError: true,
			errorMsg:    "non-existent step", // Will catch either "non-existent" or "forward reference"
		},
		{
			name: "search missing pattern",
			request: core.PipelineRequest{
				Name: "test",
				Steps: []core.PipelineStep{
					{ID: "step1", Action: "search", Params: map[string]interface{}{}},
				},
			},
			expectError: true,
			errorMsg:    "pattern",
		},
		{
			name: "edit missing old_text",
			request: core.PipelineRequest{
				Name: "test",
				Steps: []core.PipelineStep{
					{ID: "step1", Action: "edit", Params: map[string]interface{}{"new_text": "b", "files": []string{"test.txt"}}},
				},
			},
			expectError: true,
			errorMsg:    "old_text",
		},
		{
			name: "valid pipeline",
			request: core.PipelineRequest{
				Name: "test-pipeline",
				Steps: []core.PipelineStep{
					{ID: "search", Action: "search", Params: map[string]interface{}{"pattern": "test"}},
					{ID: "edit", Action: "edit", InputFrom: "search", Params: map[string]interface{}{"old_text": "test", "new_text": "best"}},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Execute(ctx, tt.request)

			if tt.expectError {
				if err == nil && result.Success {
					t.Errorf("expected error containing '%s', got nil", tt.errorMsg)
				}
				if err != nil && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%v'", tt.errorMsg, err)
				}
				if result != nil && result.Success && !strings.Contains(strings.Join(extractErrors(result), " "), tt.errorMsg) {
					t.Errorf("expected result error containing '%s'", tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestPipeline_SearchAndCount tests a linear pipeline: search -> count
func TestPipeline_SearchAndCount(t *testing.T) {
	// Create test files first
	testDir := t.TempDir()
	file1 := filepath.Join(testDir, "file1.txt")
	file2 := filepath.Join(testDir, "file2.txt")
	os.WriteFile(file1, []byte("hello world hello"), 0644)
	os.WriteFile(file2, []byte("hello universe"), 0644)

	// Now create engine with this test dir allowed
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	request := core.PipelineRequest{
		Name: "search-and-count",
		Steps: []core.PipelineStep{
			{
				ID:     "find",
				Action: "search",
				Params: map[string]interface{}{
					"path":    testDir,
					"pattern": "hello",
				},
			},
			{
				ID:        "count",
				Action:    "count_occurrences",
				InputFrom: "find",
				Params: map[string]interface{}{
					"pattern": "hello",
				},
			},
		},
	}

	result, err := executor.Execute(ctx, request)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure")
	}

	if result.CompletedSteps != 2 {
		t.Errorf("expected 2 completed steps, got %d", result.CompletedSteps)
	}

	// Verify search found files
	searchResult := result.Results[0]
	if len(searchResult.FilesMatched) != 2 {
		t.Errorf("expected 2 files matched, got %d", len(searchResult.FilesMatched))
	}

	// Verify count results
	countResult := result.Results[1]
	totalCount := 0
	for _, count := range countResult.Counts {
		totalCount += count
	}
	if totalCount != 3 {
		t.Errorf("expected total count of 3, got %d", totalCount)
	}
}

// TestPipeline_DryRun tests dry-run mode
func TestPipeline_DryRun(t *testing.T) {
	testDir := t.TempDir()
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	// Create test file
	testFile := filepath.Join(testDir, "test.txt")
	originalContent := "hello world"
	os.WriteFile(testFile, []byte(originalContent), 0644)

	request := core.PipelineRequest{
		Name:   "dry-run-test",
		DryRun: true,
		Steps: []core.PipelineStep{
			{
				ID:     "edit",
				Action: "edit",
				Params: map[string]interface{}{
					"files":    []string{testFile},
					"old_text": "hello",
					"new_text": "goodbye",
				},
			},
		},
	}

	result, err := executor.Execute(ctx, request)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure")
	}

	if !result.DryRun {
		t.Errorf("expected DryRun=true in result")
	}

	// Verify file was NOT modified
	content, _ := os.ReadFile(testFile)
	if string(content) != originalContent {
		t.Errorf("file was modified in dry-run mode: expected '%s', got '%s'", originalContent, string(content))
	}

	// Verify edit count was reported
	editResult := result.Results[0]
	if editResult.Counts[testFile] != 1 {
		t.Errorf("expected count of 1, got %d", editResult.Counts[testFile])
	}
}

// TestPipeline_StopOnError tests stop-on-error behavior
func TestPipeline_StopOnError(t *testing.T) {
	// Create test directory with a file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	request := core.PipelineRequest{
		Name:        "stop-on-error",
		StopOnError: true,
		Steps: []core.PipelineStep{
			{
				ID:     "step1",
				Action: "search",
				Params: map[string]interface{}{
					"path":    testDir,
					"pattern": "test",
				},
			},
			{
				ID:     "step2",
				Action: "edit",
				Params: map[string]interface{}{
					"files":    []string{"/nonexistent/file.txt"},
					"old_text": "test",
					"new_text": "best",
				},
			},
			{
				ID:     "step3",
				Action: "search",
				Params: map[string]interface{}{
					"path":    testDir,
					"pattern": "final",
				},
			},
		},
	}

	result, _ := executor.Execute(ctx, request)

	// Should fail due to step2 error
	if result.Success {
		t.Errorf("expected pipeline to fail, but it succeeded")
	}

	// Should stop at step 2 (not execute step 3)
	if result.CompletedSteps >= 3 {
		t.Errorf("expected pipeline to stop before step 3, but completed %d steps", result.CompletedSteps)
	}

	// Verify step1 succeeded
	if len(result.Results) > 0 && !result.Results[0].Success {
		t.Errorf("expected step1 to succeed")
	}

	// Verify step2 failed (if it was executed)
	if len(result.Results) > 1 && result.Results[1].Success {
		t.Errorf("expected step2 to fail")
	}
}

// TestPipeline_DependencyChain tests a multi-step dependency chain
func TestPipeline_DependencyChain(t *testing.T) {
	testDir := t.TempDir()
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	// Create test file
	testFile := filepath.Join(testDir, "test.txt")
	os.WriteFile(testFile, []byte("old old old"), 0644)

	request := core.PipelineRequest{
		Name: "dependency-chain",
		Steps: []core.PipelineStep{
			{
				ID:     "search",
				Action: "search",
				Params: map[string]interface{}{
					"path":    testDir,
					"pattern": "old",
				},
			},
			{
				ID:        "read",
				Action:    "read_ranges",
				InputFrom: "search",
			},
			{
				ID:        "edit",
				Action:    "edit",
				InputFrom: "search",
				Params: map[string]interface{}{
					"old_text": "old",
					"new_text": "new",
				},
			},
			{
				ID:        "verify",
				Action:    "count_occurrences",
				InputFrom: "search",
				Params: map[string]interface{}{
					"pattern": "new",
				},
			},
		},
	}

	result, err := executor.Execute(ctx, request)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", extractErrors(result))
	}

	if result.CompletedSteps != 4 {
		t.Errorf("expected 4 completed steps, got %d", result.CompletedSteps)
	}

	// Verify search found the file
	if len(result.Results[0].FilesMatched) != 1 {
		t.Fatalf("search: expected 1 file, got %d", len(result.Results[0].FilesMatched))
	}

	// Use the actual key returned by the pipeline (may differ from testFile due to NormalizePath)
	actualKey := result.Results[0].FilesMatched[0]

	// Verify read got content
	if result.Results[1].Content[actualKey] == "" {
		t.Errorf("read: expected content for %s, got empty (available keys: %v)", actualKey, mapKeys(result.Results[1].Content))
	}

	// Verify edit made 3 replacements
	if result.Results[2].EditsApplied != 3 {
		t.Errorf("edit: expected 3 edits, got %d", result.Results[2].EditsApplied)
	}

	// Verify final count is 3
	verifyResult := result.Results[3]
	if verifyResult.Counts[actualKey] != 3 {
		t.Errorf("verify: expected count of 3, got %d (available keys: %v)", verifyResult.Counts[actualKey], mapKeys2(verifyResult.Counts))
	}
}

// TestPipeline_RiskAssessment tests risk assessment and force flag
func TestPipeline_RiskAssessment(t *testing.T) {
	testDir := t.TempDir()
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	// Create many test files to trigger HIGH risk (60 unique files)
	for i := 0; i < 60; i++ {
		testFile := filepath.Join(testDir, fmt.Sprintf("file%03d.txt", i))
		os.WriteFile(testFile, []byte("test content"), 0644)
	}

	request := core.PipelineRequest{
		Name:        "risky-operation",
		Force:       false, // Do NOT force
		StopOnError: true,  // Stop pipeline when edit is blocked by risk
		Steps: []core.PipelineStep{
			{
				ID:     "search",
				Action: "search",
				Params: map[string]interface{}{
					"path":    testDir,
					"pattern": "test",
				},
			},
			{
				ID:        "edit",
				Action:    "edit",
				InputFrom: "search",
				Params: map[string]interface{}{
					"old_text": "test",
					"new_text": "best",
				},
			},
		},
	}

	result, _ := executor.Execute(ctx, request)

	// Should fail due to HIGH risk without force
	if result.Success {
		t.Errorf("expected failure due to high risk, got success")
	}

	// Now try with force=true
	request.Force = true
	result2, _ := executor.Execute(ctx, request)
	if result2 == nil {
		t.Fatal("pipeline with force returned nil result")
	}

	if !result2.Success {
		t.Errorf("expected success with force=true, got failure: %v", extractErrors(result2))
	}

	// Verify risk level is reported
	if result2.OverallRiskLevel != "HIGH" && result2.OverallRiskLevel != "CRITICAL" {
		t.Errorf("expected HIGH or CRITICAL risk, got %s", result2.OverallRiskLevel)
	}
}

// TestPipeline_BackupAndRollback tests backup creation and rollback
func TestPipeline_BackupAndRollback(t *testing.T) {
	testDir := t.TempDir()
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	// Create test file
	testFile := filepath.Join(testDir, "test.txt")
	originalContent := "original content"
	os.WriteFile(testFile, []byte(originalContent), 0644)

	request := core.PipelineRequest{
		Name:         "backup-test",
		CreateBackup: true,
		StopOnError:  true,
		Steps: []core.PipelineStep{
			{
				ID:     "edit1",
				Action: "edit",
				Params: map[string]interface{}{
					"files":    []string{testFile},
					"old_text": "original",
					"new_text": "modified",
				},
			},
			{
				ID:     "edit2-fail",
				Action: "edit",
				Params: map[string]interface{}{
					"files":    []string{"/nonexistent.txt"},
					"old_text": "test",
					"new_text": "best",
				},
			},
		},
	}

	result, _ := executor.Execute(ctx, request)

	// Pipeline should fail at step 2
	if result.Success {
		t.Errorf("expected pipeline to fail")
	}

	// Verify backup was created
	if result.BackupID == "" {
		t.Errorf("expected backup ID, got empty")
	}

	// If rollback was performed, file should be restored
	if result.RollbackPerformed {
		content, _ := os.ReadFile(testFile)
		if string(content) != originalContent {
			t.Errorf("rollback failed: expected '%s', got '%s'", originalContent, string(content))
		}
	}
}

// TestPipeline_MultiEdit tests multi-edit operation
func TestPipeline_MultiEdit(t *testing.T) {
	testDir := t.TempDir()
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	// Create test file
	testFile := filepath.Join(testDir, "test.txt")
	os.WriteFile(testFile, []byte("apple banana cherry"), 0644)

	request := core.PipelineRequest{
		Name: "multi-edit-test",
		Steps: []core.PipelineStep{
			{
				ID:     "multi-edit",
				Action: "multi_edit",
				Params: map[string]interface{}{
					"files": []string{testFile},
					"edits": []interface{}{
						map[string]interface{}{"old_text": "apple", "new_text": "orange"},
						map[string]interface{}{"old_text": "banana", "new_text": "grape"},
					},
				},
			},
		},
	}

	result, err := executor.Execute(ctx, request)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure")
	}

	// Verify edits were applied
	content, _ := os.ReadFile(testFile)
	if !strings.Contains(string(content), "orange") || !strings.Contains(string(content), "grape") {
		t.Errorf("multi-edit failed: got '%s'", string(content))
	}

	if result.Results[0].EditsApplied != 2 {
		t.Errorf("expected 2 edits, got %d", result.Results[0].EditsApplied)
	}
}

// TestPipeline_Copy tests file copy operation
func TestPipeline_Copy(t *testing.T) {
	testDir := t.TempDir()
	engine := createTestEngineWithPath(t, testDir)
	executor := core.NewPipelineExecutor(engine)
	ctx := context.Background()

	// Create test files
	srcFile := filepath.Join(testDir, "source.txt")
	destDir := filepath.Join(testDir, "dest")
	os.WriteFile(srcFile, []byte("test content"), 0644)
	os.MkdirAll(destDir, 0755)

	request := core.PipelineRequest{
		Name: "copy-test",
		Steps: []core.PipelineStep{
			{
				ID:     "copy",
				Action: "copy",
				Params: map[string]interface{}{
					"files":       []string{srcFile},
					"destination": destDir,
				},
			},
		},
	}

	result, err := executor.Execute(ctx, request)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure")
	}

	// Verify file was copied
	destFile := filepath.Join(destDir, "source.txt")
	if _, err := os.Stat(destFile); os.IsNotExist(err) {
		t.Errorf("destination file was not created")
	}
}

// Helper functions

func extractErrors(result *core.PipelineResult) []string {
	var errors []string
	for _, r := range result.Results {
		if r.Error != "" {
			errors = append(errors, r.Error)
		}
	}
	return errors
}

func createTestEngine(t *testing.T) *core.UltraFastEngine {
	tempDir := t.TempDir()
	return createTestEngineWithPath(t, tempDir)
}

func createTestEngineWithPath(t *testing.T, allowedPath string) *core.UltraFastEngine {
	cacheInstance, err := cache.NewIntelligentCache(50 * 1024 * 1024) // 50MB
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedPath}, // Allow specified path for testing
		ParallelOps:  2,
		CompactMode:  false,
	}

	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return engine
}

// mapKeys returns all keys from a map[string]string for debug output
func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// mapKeys2 returns all keys from a map[string]int for debug output
func mapKeys2(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
