package main

import (
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestScheduler_NoDependencies(t *testing.T) {
	scheduler := core.NewPipelineScheduler()
	steps := []core.PipelineStep{
		{ID: "a", Action: "search", Params: map[string]interface{}{"pattern": "x"}},
		{ID: "b", Action: "search", Params: map[string]interface{}{"pattern": "y"}},
		{ID: "c", Action: "search", Params: map[string]interface{}{"pattern": "z"}},
	}

	levels, err := scheduler.BuildExecutionPlan(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All read-only steps with no deps → single level
	if len(levels) != 1 {
		t.Fatalf("expected 1 level, got %d: %v", len(levels), levels)
	}
	if len(levels[0]) != 3 {
		t.Fatalf("expected 3 steps in level 0, got %d", len(levels[0]))
	}
}

func TestScheduler_LinearChain(t *testing.T) {
	scheduler := core.NewPipelineScheduler()
	steps := []core.PipelineStep{
		{ID: "a", Action: "search", Params: map[string]interface{}{"pattern": "x"}},
		{ID: "b", Action: "count_occurrences", InputFrom: "a", Params: map[string]interface{}{"pattern": "x"}},
		{ID: "c", Action: "edit", InputFrom: "b", Params: map[string]interface{}{"old_text": "x", "new_text": "y"}},
	}

	levels, err := scheduler.BuildExecutionPlan(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each step depends on previous → 3 levels (with destructive split)
	if len(levels) < 3 {
		t.Fatalf("expected at least 3 levels for linear chain, got %d", len(levels))
	}
}

func TestScheduler_DiamondDAG(t *testing.T) {
	scheduler := core.NewPipelineScheduler()
	// Diamond: a → b, a → c, b+c → d
	steps := []core.PipelineStep{
		{ID: "a", Action: "search", Params: map[string]interface{}{"pattern": "x"}},
		{ID: "b", Action: "count_occurrences", InputFrom: "a", Params: map[string]interface{}{"pattern": "x"}},
		{ID: "c", Action: "count_occurrences", InputFrom: "a", Params: map[string]interface{}{"pattern": "y"}},
		{ID: "d", Action: "merge", InputFrom: "b", Condition: &core.StepCondition{Type: "step_succeeded", StepRef: "c"}},
	}

	levels, err := scheduler.BuildExecutionPlan(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Level 0: a, Level 1: b+c (parallel), Level 2: d
	if len(levels) < 3 {
		t.Fatalf("expected at least 3 levels for diamond DAG, got %d", len(levels))
	}

	// b and c should be in the same level (level 1)
	found := false
	for _, level := range levels {
		if len(level) == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected b and c to be in the same level (parallel)")
	}
}

func TestScheduler_DestructiveSplitting(t *testing.T) {
	scheduler := core.NewPipelineScheduler()
	// Two independent edit steps (no deps between them)
	steps := []core.PipelineStep{
		{ID: "a", Action: "edit", Params: map[string]interface{}{"old_text": "x", "new_text": "y", "files": []interface{}{"a.go"}}},
		{ID: "b", Action: "edit", Params: map[string]interface{}{"old_text": "a", "new_text": "b", "files": []interface{}{"b.go"}}},
	}

	levels, err := scheduler.BuildExecutionPlan(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both are destructive with no deps → should be split into separate sub-levels
	if len(levels) < 2 {
		t.Fatalf("expected destructive steps to be split, got %d levels", len(levels))
	}
}

func TestScheduler_EmptyPlan(t *testing.T) {
	scheduler := core.NewPipelineScheduler()
	levels, err := scheduler.BuildExecutionPlan(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if levels != nil {
		t.Fatal("expected nil for empty steps")
	}
}

func TestScheduler_GetDependencies(t *testing.T) {
	scheduler := core.NewPipelineScheduler()
	steps := []core.PipelineStep{
		{ID: "a", Action: "search", Params: map[string]interface{}{"pattern": "x"}},
		{ID: "b", Action: "edit", InputFrom: "a", Params: map[string]interface{}{"old_text": "x", "new_text": "y"}},
		{ID: "c", Action: "edit", InputFrom: "a",
			Condition: &core.StepCondition{Type: "step_succeeded", StepRef: "b"},
			Params:    map[string]interface{}{"old_text": "z", "new_text": "w"},
		},
	}

	deps := scheduler.GetDependencies(steps)
	if len(deps["a"]) != 0 {
		t.Fatal("step a should have no deps")
	}
	if len(deps["b"]) != 1 || deps["b"][0] != "a" {
		t.Fatalf("step b deps: %v", deps["b"])
	}
	// c depends on a (input_from) and b (condition)
	if len(deps["c"]) != 2 {
		t.Fatalf("step c should have 2 deps, got %v", deps["c"])
	}
}
