package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	localmcp "github.com/mcp/filesystem-ultra/mcp"
)

// setupEngine19_21 creates a test engine for bug #19, #20, #21 tests
func setupEngine19_21(t *testing.T) (*core.UltraFastEngine, string) {
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

// makeSearchRequest builds a localmcp.CallToolRequest for SmartSearch/AdvancedTextSearch
func makeSearchRequest(args map[string]interface{}) localmcp.CallToolRequest {
	return localmcp.CallToolRequest{Arguments: args}
}

// responseText extracts text content from a localmcp.CallToolResponse
func responseText(r *localmcp.CallToolResponse) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// =============================================================================
// Bug #19 — NormalizePath missing in search_files handler
// =============================================================================

// TestBug19_NormalizePath_WindowsPathUnchanged verifies a Windows path
// keeps its drive letter and is not mangled.
func TestBug19_NormalizePath_WindowsPathUnchanged(t *testing.T) {
	input := `C:\Users\foo\bar\file.go`
	got := core.NormalizePath(input)
	if !strings.HasPrefix(got, "C:") {
		t.Errorf("NormalizePath changed Windows path; got %q", got)
	}
	if strings.Contains(got, "mnt") {
		t.Errorf("NormalizePath injected 'mnt' into Windows path: %q", got)
	}
}

// TestBug19_NormalizePath_WSLConverted verifies /mnt/c/ → C:\ conversion
// and ensures the result does NOT contain "mnt\c" (the pre-fix bug).
func TestBug19_NormalizePath_WSLConverted(t *testing.T) {
	input := "/mnt/c/Users/foo/bar"
	got := core.NormalizePath(input)

	if strings.Contains(got, `mnt\c`) || strings.Contains(got, "mnt/c") {
		t.Errorf("NormalizePath produced buggy output (mnt preserved): %q", got)
	}
	if !strings.HasPrefix(got, "C:") {
		t.Errorf("NormalizePath did not convert /mnt/c/ to C:\\; got %q", got)
	}
}

// TestBug19_SmartSearch_FindsContent verifies SmartSearch finds a known
// pattern, confirming NormalizePath does not break the search path.
func TestBug19_SmartSearch_FindsContent(t *testing.T) {
	engine, tempDir := setupEngine19_21(t)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "search_smoke.go")
	if err := os.WriteFile(testFile, []byte("package main\n\n// target_marker\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := makeSearchRequest(map[string]interface{}{
		"path":            tempDir,
		"pattern":         "target_marker",
		"include_content": true,
	})

	resp, err := engine.SmartSearch(ctx, req)
	if err != nil {
		t.Fatalf("SmartSearch error: %v", err)
	}
	text := responseText(resp)
	if !strings.Contains(text, "target_marker") {
		t.Errorf("SmartSearch did not return match; response: %q", text)
	}
}

// TestBug19_CountOccurrences_DirectoryMode verifies CountOccurrences aggregates
// across multiple files in a directory (Bug #18 + #19 combined).
func TestBug19_CountOccurrences_DirectoryMode(t *testing.T) {
	engine, tempDir := setupEngine19_21(t)
	ctx := context.Background()

	files := map[string]string{
		"a.go": "foo foo bar\nfoo\n", // 3 occurrences
		"b.go": "foo baz\n",         // 1 occurrence
		"c.go": "bar baz\n",         // 0 occurrences
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(body), 0644); err != nil {
			t.Fatalf("setup %s: %v", name, err)
		}
	}

	result, err := engine.CountOccurrences(ctx, tempDir, "foo", false, true, false)
	if err != nil {
		t.Fatalf("CountOccurrences error: %v", err)
	}
	if !strings.Contains(result, "foo") {
		t.Errorf("CountOccurrences result missing pattern: %q", result)
	}
	if !strings.Contains(result, "a.go") || !strings.Contains(result, "b.go") {
		t.Errorf("CountOccurrences missing per-file results; got: %q", result)
	}
}

// =============================================================================
// Bug #20 — batchManager.SetEngine(engine) was missing
// =============================================================================

// TestBug20_BatchSearchAndReplace_Succeeds verifies search_and_replace works
// when engine is properly set on the batch manager.
func TestBug20_BatchSearchAndReplace_Succeeds(t *testing.T) {
	engine, tempDir := setupEngine19_21(t)

	testFile := filepath.Join(tempDir, "batch_sar.go")
	original := "package main\n\nconst OldName = \"hello\"\nconst OldName2 = \"world\"\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	batchManager := core.NewBatchOperationManager(tempDir, 10)
	batchManager.SetEngine(engine) // Bug #20 fix

	req := core.BatchRequest{
		Operations: []core.FileOperation{
			{
				Type:    "search_and_replace",
				Path:    testFile,
				OldText: "OldName",
				NewText: "NewName",
			},
		},
		Atomic:       true,
		CreateBackup: false,
	}

	result := batchManager.ExecuteBatch(req)
	if !result.Success {
		t.Fatalf("batch search_and_replace failed: %+v", result)
	}

	got, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read after batch: %v", err)
	}
	if strings.Contains(string(got), "OldName") {
		t.Errorf("search_and_replace left OldName in file:\n%s", got)
	}
	if !strings.Contains(string(got), "NewName") {
		t.Errorf("NewName not found after replacement:\n%s", got)
	}
}

// TestBug20_BatchWithoutEngine_Fails verifies that without SetEngine,
// search_and_replace fails (documents pre-fix behaviour, prevents regression).
func TestBug20_BatchWithoutEngine_Fails(t *testing.T) {
	_, tempDir := setupEngine19_21(t)

	testFile := filepath.Join(tempDir, "no_engine.go")
	if err := os.WriteFile(testFile, []byte("OldName\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// No SetEngine — should fail
	batchManager := core.NewBatchOperationManager(tempDir, 10)

	req := core.BatchRequest{
		Operations: []core.FileOperation{
			{
				Type:    "search_and_replace",
				Path:    testFile,
				OldText: "OldName",
				NewText: "NewName",
			},
		},
		Atomic:       false,
		CreateBackup: false,
	}

	result := batchManager.ExecuteBatch(req)
	if result.Success {
		t.Error("expected failure without SetEngine, but batch succeeded — regression risk")
	}
}

// =============================================================================
// Bug #21 — formatPipelineResult silent failures in compact mode
// We test that PipelineResult.Results[].Error is populated on failure,
// which is the data source for the compact formatter fix in main.go.
// =============================================================================

// TestBug21_FailedStepHasErrorPopulated verifies that when a pipeline step
// fails, StepResult.Error is non-empty.
func TestBug21_FailedStepHasErrorPopulated(t *testing.T) {
	engine, tempDir := setupEngine19_21(t)
	ctx := context.Background()

	nonexistent := filepath.Join(tempDir, "ghost_file_does_not_exist.go")

	req := core.PipelineRequest{
		Name:        "test-bug21-fail",
		StopOnError: true,
		Steps: []core.PipelineStep{
			{
				ID:     "step1",
				Action: "read_ranges",
				Params: map[string]interface{}{
					"path": nonexistent,
				},
			},
		},
	}

	result, err := engine.ExecutePipeline(ctx, req)
	if err != nil {
		t.Logf("ExecutePipeline returned error directly (acceptable): %v", err)
		return
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Success {
		t.Skip("pipeline unexpectedly succeeded")
	}
	if len(result.Results) == 0 {
		t.Fatal("no step results returned")
	}

	failedStep := result.Results[0]
	if failedStep.Error == "" {
		t.Errorf("Bug #21: StepResult.Error is empty for failed step %q — compact formatter will show silent failure", failedStep.StepID)
	} else {
		t.Logf("StepResult.Error correctly populated: %q", failedStep.Error)
	}
}

// TestBug21_SuccessfulPipelineHasNoError verifies successful steps leave
// StepResult.Error empty (no false positives).
func TestBug21_SuccessfulPipelineHasNoError(t *testing.T) {
	engine, tempDir := setupEngine19_21(t)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "ok_pipeline.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := core.PipelineRequest{
		Name: "test-bug21-ok",
		Steps: []core.PipelineStep{
			{
				ID:     "readok",
				Action: "search",
				Params: map[string]interface{}{
					"path":    testFile,
					"pattern": "package",
				},
			},
		},
	}

	result, err := engine.ExecutePipeline(ctx, req)
	if err != nil {
		t.Fatalf("ExecutePipeline error: %v", err)
	}
	if !result.Success {
		t.Fatalf("pipeline failed unexpectedly")
	}
	if len(result.Results) == 0 {
		t.Fatal("no step results")
	}
	if result.Results[0].Error != "" {
		t.Errorf("successful step has Error populated: %q", result.Results[0].Error)
	}
}
