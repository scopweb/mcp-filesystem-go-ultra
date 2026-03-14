package main

import (
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestTemplates_SimpleStringReplacement(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("find", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"a.go", "b.go", "c.go"},
		Counts:       map[string]int{"a.go": 5, "b.go": 3, "c.go": 2},
		RiskLevel:    "MEDIUM",
		EditsApplied: 10,
	})

	params := map[string]interface{}{
		"message": "Found {{find.count}} occurrences in {{find.files_count}} files",
	}

	resolved := core.ResolveTemplates(params, pCtx)
	msg := resolved["message"].(string)
	if msg != "Found 10 occurrences in 3 files" {
		t.Fatalf("unexpected resolved message: %s", msg)
	}
}

func TestTemplates_AllFields(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("step1", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"x.go", "y.go"},
		Counts:       map[string]int{"x.go": 7},
		RiskLevel:    "HIGH",
		EditsApplied: 42,
	})

	tests := []struct {
		template string
		expected string
	}{
		{"{{step1.count}}", "7"},
		{"{{step1.files_count}}", "2"},
		{"{{step1.files}}", "x.go,y.go"},
		{"{{step1.risk}}", "HIGH"},
		{"{{step1.edits}}", "42"},
	}

	for _, tc := range tests {
		params := map[string]interface{}{"val": tc.template}
		resolved := core.ResolveTemplates(params, pCtx)
		got := resolved["val"].(string)
		if got != tc.expected {
			t.Errorf("template %q: expected %q, got %q", tc.template, tc.expected, got)
		}
	}
}

func TestTemplates_NestedMaps(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("s1", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"test.go"},
	})

	params := map[string]interface{}{
		"outer": map[string]interface{}{
			"inner": "files: {{s1.files_count}}",
		},
	}

	resolved := core.ResolveTemplates(params, pCtx)
	outer := resolved["outer"].(map[string]interface{})
	inner := outer["inner"].(string)
	if inner != "files: 1" {
		t.Fatalf("nested template not resolved: %s", inner)
	}
}

func TestTemplates_SliceValues(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("s1", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"a.go"},
		EditsApplied: 5,
	})

	params := map[string]interface{}{
		"items": []interface{}{
			"edits: {{s1.edits}}",
			"plain string",
			42, // non-string should pass through
		},
	}

	resolved := core.ResolveTemplates(params, pCtx)
	items := resolved["items"].([]interface{})
	if items[0].(string) != "edits: 5" {
		t.Fatalf("slice template not resolved: %v", items[0])
	}
	if items[1].(string) != "plain string" {
		t.Fatalf("plain string changed: %v", items[1])
	}
	if items[2].(int) != 42 {
		t.Fatalf("non-string changed: %v", items[2])
	}
}

func TestTemplates_UnresolvedReference(t *testing.T) {
	pCtx := core.NewPipelineContext()
	// No step results stored

	params := map[string]interface{}{
		"val": "{{nonexistent.count}}",
	}

	resolved := core.ResolveTemplates(params, pCtx)
	got := resolved["val"].(string)
	if got != "{{nonexistent.count}}" {
		t.Fatalf("unresolved template should remain as-is, got: %s", got)
	}
}

func TestTemplates_UnknownField(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("s1", &core.StepResult{Success: true})

	params := map[string]interface{}{
		"val": "{{s1.unknown_field}}",
	}

	resolved := core.ResolveTemplates(params, pCtx)
	got := resolved["val"].(string)
	// Unknown field returns the original template as fallback
	if got != "{{s1.unknown_field}}" {
		t.Fatalf("unknown field should return fallback, got: %s", got)
	}
}

func TestTemplates_NilParams(t *testing.T) {
	pCtx := core.NewPipelineContext()
	resolved := core.ResolveTemplates(nil, pCtx)
	if resolved != nil {
		t.Fatal("nil params should return nil")
	}
}

func TestTemplates_NoTemplates(t *testing.T) {
	pCtx := core.NewPipelineContext()
	params := map[string]interface{}{
		"plain": "no templates here",
		"num":   123,
	}

	resolved := core.ResolveTemplates(params, pCtx)
	if resolved["plain"].(string) != "no templates here" {
		t.Fatal("plain string should pass through unchanged")
	}
	if resolved["num"].(int) != 123 {
		t.Fatal("non-string should pass through unchanged")
	}
}

func TestTemplates_CountFallbackToFilesMatched(t *testing.T) {
	pCtx := core.NewPipelineContext()
	// Step with no Counts but with FilesMatched
	pCtx.SetStepResult("s1", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"a.go", "b.go", "c.go"},
		Counts:       nil,
	})

	params := map[string]interface{}{
		"val": "{{s1.count}}",
	}

	resolved := core.ResolveTemplates(params, pCtx)
	got := resolved["val"].(string)
	if got != "3" {
		t.Fatalf("count should fall back to files_count when no Counts map, got: %s", got)
	}
}

func TestTemplates_MultipleInOneString(t *testing.T) {
	pCtx := core.NewPipelineContext()
	pCtx.SetStepResult("a", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"x.go"},
		EditsApplied: 10,
	})
	pCtx.SetStepResult("b", &core.StepResult{
		Success:      true,
		FilesMatched: []string{"y.go", "z.go"},
		RiskLevel:    "LOW",
	})

	params := map[string]interface{}{
		"msg": "a={{a.edits}}, b_files={{b.files_count}}, b_risk={{b.risk}}",
	}

	resolved := core.ResolveTemplates(params, pCtx)
	got := resolved["msg"].(string)
	expected := "a=10, b_files=2, b_risk=LOW"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}
