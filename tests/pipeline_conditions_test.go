package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestCondition_HasMatches(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("find", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"a.go", "b.go"},
	})

	cond := &core.StepCondition{Type: "has_matches", StepRef: "find"}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected has_matches to be true when files matched")
	}

	// Empty matches
	pCtx.SetStepResult("empty", &core.StepResult{Success: true, FilesMatched: nil})
	cond2 := &core.StepCondition{Type: "has_matches", StepRef: "empty"}
	shouldRun2, reason := core.EvaluateCondition(cond2, pCtx)
	if shouldRun2 {
		t.Fatal("expected has_matches to be false when no files matched")
	}
	if reason == "" {
		t.Fatal("expected a reason when condition is false")
	}
}

func TestCondition_NoMatches(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("find", &core.StepResult{Success: true, FilesMatched: nil})

	cond := &core.StepCondition{Type: "no_matches", StepRef: "find"}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected no_matches to be true when no files matched")
	}

	pCtx.SetStepResult("find2", &core.StepResult{Success: true, FilesMatched: []string{"a.go"}})
	cond2 := &core.StepCondition{Type: "no_matches", StepRef: "find2"}
	shouldRun2, _ := core.EvaluateCondition(cond2, pCtx)
	if shouldRun2 {
		t.Fatal("expected no_matches to be false when files matched")
	}
}

func TestCondition_CountGT(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("count", &core.StepResult{
		Success: true,
		Counts:  map[string]int{"a.go": 5, "b.go": 3},
	})

	// 5+3=8 > 5 → true
	cond := &core.StepCondition{Type: "count_gt", StepRef: "count", Value: "5"}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected count_gt to be true (8 > 5)")
	}

	// 8 > 10 → false
	cond2 := &core.StepCondition{Type: "count_gt", StepRef: "count", Value: "10"}
	shouldRun2, _ := core.EvaluateCondition(cond2, pCtx)
	if shouldRun2 {
		t.Fatal("expected count_gt to be false (8 > 10)")
	}
}

func TestCondition_CountLT(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("count", &core.StepResult{
		Success: true,
		Counts:  map[string]int{"a.go": 2},
	})

	cond := &core.StepCondition{Type: "count_lt", StepRef: "count", Value: "5"}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected count_lt to be true (2 < 5)")
	}
}

func TestCondition_CountEQ(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("count", &core.StepResult{
		Success: true,
		Counts:  map[string]int{"a.go": 3, "b.go": 2},
	})

	cond := &core.StepCondition{Type: "count_eq", StepRef: "count", Value: "5"}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected count_eq to be true (3+2 == 5)")
	}
}

func TestCondition_FileExists(t *testing.T) {
	// Create a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(tmpFile, []byte("hello"), 0644)

	pCtx := core.NewPipelineContext()

	cond := &core.StepCondition{Type: "file_exists", Path: tmpFile}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected file_exists to be true for existing file")
	}

	cond2 := &core.StepCondition{Type: "file_exists", Path: filepath.Join(tmpDir, "nope.txt")}
	shouldRun2, _ := core.EvaluateCondition(cond2, pCtx)
	if shouldRun2 {
		t.Fatal("expected file_exists to be false for non-existent file")
	}
}

func TestCondition_FileNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	pCtx := core.NewPipelineContext()

	cond := &core.StepCondition{Type: "file_not_exists", Path: filepath.Join(tmpDir, "gone.txt")}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected file_not_exists to be true for non-existent file")
	}
}

func TestCondition_StepSucceeded(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("prev", &core.StepResult{Success: true})

	cond := &core.StepCondition{Type: "step_succeeded", StepRef: "prev"}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected step_succeeded to be true")
	}

	pCtx.SetStepResult("fail", &core.StepResult{Success: false})
	cond2 := &core.StepCondition{Type: "step_succeeded", StepRef: "fail"}
	shouldRun2, _ := core.EvaluateCondition(cond2, pCtx)
	if shouldRun2 {
		t.Fatal("expected step_succeeded to be false for failed step")
	}
}

func TestCondition_StepFailed(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("fail", &core.StepResult{Success: false, Error: "some error"})

	cond := &core.StepCondition{Type: "step_failed", StepRef: "fail"}
	shouldRun, _ := core.EvaluateCondition(cond, pCtx)
	if !shouldRun {
		t.Fatal("expected step_failed to be true for failed step")
	}
}

func TestCondition_NilCondition(t *testing.T) {
	pCtx := core.NewPipelineContext()
	shouldRun, _ := core.EvaluateCondition(nil, pCtx)
	if !shouldRun {
		t.Fatal("nil condition should always return true")
	}
}

func TestCondition_ValidationErrors(t *testing.T) {
	priorSteps := map[string]int{"find": 0}

	tests := []struct {
		name string
		cond *core.StepCondition
	}{
		{"invalid type", &core.StepCondition{Type: "bogus"}},
		{"missing step_ref", &core.StepCondition{Type: "has_matches"}},
		{"bad step_ref", &core.StepCondition{Type: "has_matches", StepRef: "nonexistent"}},
		{"missing value", &core.StepCondition{Type: "count_gt", StepRef: "find"}},
		{"non-integer value", &core.StepCondition{Type: "count_gt", StepRef: "find", Value: "abc"}},
		{"missing path", &core.StepCondition{Type: "file_exists"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := core.ValidateCondition(tc.cond, "test-step", priorSteps)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestCondition_PipelineIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "cond-test.txt")
	os.WriteFile(testFile, []byte("hello world hello"), 0644)

	executor := core.NewPipelineExecutor(engine)

	// Pipeline: search → conditional edit (only if matches found)
	request := core.PipelineRequest{
		Name:        "condition-test",
		StopOnError: true,
		Steps: []core.PipelineStep{
			{
				ID:     "find",
				Action: "search",
				Params: map[string]interface{}{
					"path":    tmpDir,
					"pattern": "hello",
				},
			},
			{
				ID:        "edit",
				Action:    "edit",
				InputFrom: "find",
				Condition: &core.StepCondition{
					Type:    "has_matches",
					StepRef: "find",
				},
				Params: map[string]interface{}{
					"old_text": "hello",
					"new_text": "goodbye",
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	if !result.Success {
		t.Fatal("expected pipeline to succeed")
	}
	if result.Results[1].Skipped {
		t.Fatal("edit step should NOT be skipped (has_matches is true)")
	}

	// Verify edit happened
	content, _ := os.ReadFile(testFile)
	if string(content) != "goodbye world goodbye" {
		t.Fatalf("unexpected content: %s", string(content))
	}

	// Now test with no_matches condition (should skip)
	os.WriteFile(testFile, []byte("no match here"), 0644)

	request2 := core.PipelineRequest{
		Name:        "condition-skip-test",
		StopOnError: true,
		Steps: []core.PipelineStep{
			{
				ID:     "find",
				Action: "search",
				Params: map[string]interface{}{
					"path":    tmpDir,
					"pattern": "ZZZZZ_NOT_FOUND",
				},
			},
			{
				ID:        "edit",
				Action:    "edit",
				InputFrom: "find",
				Condition: &core.StepCondition{
					Type:    "has_matches",
					StepRef: "find",
				},
				Params: map[string]interface{}{
					"old_text": "no match",
					"new_text": "replaced",
				},
			},
		},
	}

	result2, err := executor.Execute(context.Background(), request2)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	if !result2.Success {
		t.Fatal("expected pipeline to succeed")
	}
	if !result2.Results[1].Skipped {
		t.Fatal("edit step SHOULD be skipped (no matches found)")
	}

	// Verify file unchanged
	content2, _ := os.ReadFile(testFile)
	if string(content2) != "no match here" {
		t.Fatalf("file should not have changed: %s", string(content2))
	}
}
