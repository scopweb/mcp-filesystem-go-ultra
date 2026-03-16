package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestNormalizer_ParamAlias(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	args := map[string]interface{}{
		"path":    "/test.txt",
		"old_str": "hello",
		"new_str": "world",
	}

	result := n.Normalize("edit_file", args)
	if !result.WasModified {
		t.Fatal("Expected normalization to be applied")
	}

	if _, ok := result.Args["old_text"]; !ok {
		t.Error("old_str was not renamed to old_text")
	}
	if _, ok := result.Args["new_text"]; !ok {
		t.Error("new_str was not renamed to new_text")
	}
	if _, ok := result.Args["old_str"]; ok {
		t.Error("old_str should have been removed")
	}
	if _, ok := result.Args["new_str"]; ok {
		t.Error("new_str should have been removed")
	}

	// Verify applied records
	if len(result.Applied) != 2 {
		t.Fatalf("Expected 2 applied rules, got %d", len(result.Applied))
	}
}

func TestNormalizer_ParamAliasNoOverwrite(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// If old_text already exists, old_str should be ignored
	args := map[string]interface{}{
		"path":     "/test.txt",
		"old_text": "original",
		"old_str":  "should_be_ignored",
	}

	result := n.Normalize("edit_file", args)

	if result.Args["old_text"] != "original" {
		t.Errorf("old_text was overwritten: got %v", result.Args["old_text"])
	}
}

func TestNormalizer_TypeCoerceBool(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"false", false},
		{"False", false},
		{"0", false},
		{"no", false},
	}

	for _, tt := range tests {
		args := map[string]interface{}{
			"path":  "/test.txt",
			"force": tt.input,
		}
		result := n.Normalize("edit_file", args)
		if !result.WasModified {
			t.Errorf("Expected force=%q to be coerced", tt.input)
			continue
		}
		got, ok := result.Args["force"].(bool)
		if !ok {
			t.Errorf("force=%q was not coerced to bool, got %T", tt.input, result.Args["force"])
			continue
		}
		if got != tt.expected {
			t.Errorf("force=%q: expected %v, got %v", tt.input, tt.expected, got)
		}
	}
}

func TestNormalizer_TypeCoerceBoolAlreadyBool(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// If force is already a bool, no normalization should happen
	args := map[string]interface{}{
		"force": true,
	}
	result := n.Normalize("edit_file", args)

	// force-bool-coerce should NOT fire since value is already bool
	for _, a := range result.Applied {
		if a.RuleID == "force-bool-coerce" {
			t.Error("force-bool-coerce should not fire on existing bool")
		}
	}
}

func TestNormalizer_JSONAcceptBoth(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// edits_json sent as raw JSON array (not string)
	rawEdits := []interface{}{
		map[string]interface{}{"old_text": "a", "new_text": "b"},
		map[string]interface{}{"old_text": "c", "new_text": "d"},
	}
	args := map[string]interface{}{
		"path":       "/test.txt",
		"edits_json": rawEdits,
	}

	result := n.Normalize("multi_edit", args)
	if !result.WasModified {
		t.Fatal("Expected normalization to be applied")
	}

	// edits_json should now be a string
	jsonStr, ok := result.Args["edits_json"].(string)
	if !ok {
		t.Fatalf("edits_json was not converted to string, got %T", result.Args["edits_json"])
	}

	// Verify it's valid JSON
	var parsed []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Converted JSON is invalid: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("Expected 2 edits, got %d", len(parsed))
	}
}

func TestNormalizer_JSONAcceptBothAlreadyString(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// edits_json already a string — no normalization
	args := map[string]interface{}{
		"path":       "/test.txt",
		"edits_json": `[{"old_text":"a","new_text":"b"}]`,
	}

	result := n.Normalize("multi_edit", args)
	for _, a := range result.Applied {
		if a.RuleID == "multi_edit-edits-coerce" {
			t.Error("json_accept_both should not fire on existing string")
		}
	}
}

func TestNormalizer_NestedAlias(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// pipeline_json with "type" instead of "action"
	pipeline := map[string]interface{}{
		"name": "test-pipeline",
		"steps": []interface{}{
			map[string]interface{}{"id": "s1", "type": "search", "params": map[string]interface{}{}},
			map[string]interface{}{"id": "s2", "type": "edit", "params": map[string]interface{}{}},
		},
	}
	pipelineJSON, _ := json.Marshal(pipeline)

	args := map[string]interface{}{
		"pipeline_json": string(pipelineJSON),
	}

	result := n.Normalize("batch_operations", args)
	if !result.WasModified {
		t.Fatal("Expected nested alias normalization")
	}

	// Verify the pipeline_json was modified
	var modified map[string]interface{}
	json.Unmarshal([]byte(result.Args["pipeline_json"].(string)), &modified)

	steps := modified["steps"].([]interface{})
	for _, step := range steps {
		m := step.(map[string]interface{})
		if _, hasType := m["type"]; hasType {
			t.Error("'type' field should have been renamed to 'action'")
		}
		if _, hasAction := m["action"]; !hasAction {
			t.Error("'action' field should exist after alias")
		}
	}
}

func TestNormalizer_NestedDefault(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// pipeline_json steps without IDs
	pipeline := map[string]interface{}{
		"name": "test-pipeline",
		"steps": []interface{}{
			map[string]interface{}{"action": "search", "params": map[string]interface{}{}},
			map[string]interface{}{"action": "edit", "params": map[string]interface{}{}},
		},
	}
	pipelineJSON, _ := json.Marshal(pipeline)

	args := map[string]interface{}{
		"pipeline_json": string(pipelineJSON),
	}

	result := n.Normalize("batch_operations", args)
	if !result.WasModified {
		t.Fatal("Expected nested default normalization")
	}

	var modified map[string]interface{}
	json.Unmarshal([]byte(result.Args["pipeline_json"].(string)), &modified)

	steps := modified["steps"].([]interface{})
	for i, step := range steps {
		m := step.(map[string]interface{})
		id, ok := m["id"].(string)
		if !ok || id == "" {
			t.Errorf("Step %d: expected auto-generated ID", i)
		}
	}
}

func TestNormalizer_WildcardRule(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// force is a wildcard rule (*), should work on any tool
	tools := []string{"edit_file", "multi_edit", "write_file", "delete_file", "backup"}
	for _, tool := range tools {
		args := map[string]interface{}{
			"force": "true",
		}
		result := n.Normalize(tool, args)
		got, ok := result.Args["force"].(bool)
		if !ok || !got {
			t.Errorf("Tool %s: force was not coerced to bool", tool)
		}
	}
}

func TestNormalizer_ToolSpecific(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// edit_file rules should NOT fire for read_file
	args := map[string]interface{}{
		"old_str": "hello",
	}
	result := n.Normalize("read_file", args)

	// old_str should NOT be renamed (rule is for edit_file only)
	if _, ok := result.Args["old_text"]; ok {
		t.Error("edit_file param_alias should not fire for read_file")
	}
	if _, ok := result.Args["old_str"]; !ok {
		t.Error("old_str should remain untouched for read_file")
	}
}

func TestNormalizer_Stats(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// Process a few normalizations
	n.Normalize("edit_file", map[string]interface{}{"old_str": "a"})
	n.Normalize("edit_file", map[string]interface{}{"old_str": "b"})
	n.Normalize("read_file", map[string]interface{}{"path": "/x"})

	stats := n.GetStats()
	if stats.TotalProcessed != 3 {
		t.Errorf("TotalProcessed: expected 3, got %d", stats.TotalProcessed)
	}
	if stats.TotalNormalized != 2 {
		t.Errorf("TotalNormalized: expected 2, got %d", stats.TotalNormalized)
	}
}

func TestNormalizer_ExternalRules(t *testing.T) {
	tmpDir := t.TempDir()
	rulesPath := filepath.Join(tmpDir, "rules.json")

	// Create an external rules file
	rules := []core.NormalizationRule{
		{
			ID:    "custom-alias",
			Tools: []string{"read_file"},
			Type:  core.RuleParamAlias,
			From:  "filename",
			To:    "path",
		},
	}
	data, _ := json.Marshal(rules)
	os.WriteFile(rulesPath, data, 0644)

	n, err := core.NewNormalizer(rulesPath, "")
	if err != nil {
		t.Fatalf("NewNormalizer with external rules: %v", err)
	}

	args := map[string]interface{}{
		"filename": "/test.txt",
	}
	result := n.Normalize("read_file", args)
	if !result.WasModified {
		t.Fatal("Expected external rule to apply")
	}
	if _, ok := result.Args["path"]; !ok {
		t.Error("filename was not renamed to path by external rule")
	}
}

func TestNormalizer_EmptyArgs(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// nil args should not panic
	result := n.Normalize("edit_file", nil)
	if result.WasModified {
		t.Error("nil args should not produce modifications")
	}

	// empty args should not panic
	result = n.Normalize("edit_file", map[string]interface{}{})
	if result.WasModified {
		t.Error("empty args should not produce modifications")
	}
}

func TestNormalizer_MultipleRules(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// Send args that trigger multiple rules at once
	args := map[string]interface{}{
		"path":    "/test.txt",
		"old_str": "hello",
		"new_str": "world",
		"force":   "true",
	}

	result := n.Normalize("edit_file", args)
	if len(result.Applied) < 3 {
		t.Errorf("Expected at least 3 rules to fire, got %d", len(result.Applied))
	}

	// Verify all three normalizations
	if result.Args["old_text"] != "hello" {
		t.Error("old_str → old_text failed")
	}
	if result.Args["new_text"] != "world" {
		t.Error("new_str → new_text failed")
	}
	if result.Args["force"] != true {
		t.Error("force string → bool failed")
	}
}

func TestNormalizer_Bug22Coverage(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	// Bug #22 Pattern 1: old_str/new_str aliases
	t.Run("old_str_new_str", func(t *testing.T) {
		args := map[string]interface{}{
			"path":    "/test.txt",
			"old_str": "before",
			"new_str": "after",
		}
		result := n.Normalize("edit_file", args)
		if result.Args["old_text"] != "before" || result.Args["new_text"] != "after" {
			t.Error("old_str/new_str aliasing failed")
		}
	})

	// Bug #22 Pattern 2: edits_json as raw array
	t.Run("edits_json_raw_array", func(t *testing.T) {
		args := map[string]interface{}{
			"path": "/test.txt",
			"edits_json": []interface{}{
				map[string]interface{}{"old_text": "a", "new_text": "b"},
			},
		}
		result := n.Normalize("multi_edit", args)
		if _, ok := result.Args["edits_json"].(string); !ok {
			t.Error("edits_json raw array → string conversion failed")
		}
	})

	// Bug #22 Pattern 3: pipeline type → action
	t.Run("pipeline_type_alias", func(t *testing.T) {
		pipeline := map[string]interface{}{
			"steps": []interface{}{
				map[string]interface{}{"id": "s1", "type": "search"},
			},
		}
		pJSON, _ := json.Marshal(pipeline)
		args := map[string]interface{}{
			"pipeline_json": string(pJSON),
		}
		result := n.Normalize("batch_operations", args)
		if !result.WasModified {
			t.Error("pipeline type→action alias failed")
		}
	})

	// Bug #22 Pattern 4: force as string "true"
	t.Run("force_string_coerce", func(t *testing.T) {
		args := map[string]interface{}{"force": "true"}
		result := n.Normalize("edit_file", args)
		if result.Args["force"] != true {
			t.Error("force string→bool coercion failed")
		}
	})

	// Bug #22 Pattern 5: count_only as string
	t.Run("count_only_string_coerce", func(t *testing.T) {
		args := map[string]interface{}{
			"path":       "/test",
			"pattern":    "TODO",
			"count_only": "true",
		}
		result := n.Normalize("search_files", args)
		if result.Args["count_only"] != true {
			t.Error("count_only string→bool coercion failed")
		}
	})

	// Bug #22 Pattern 6: dry_run as string
	t.Run("dry_run_string_coerce", func(t *testing.T) {
		args := map[string]interface{}{"dry_run": "true"}
		result := n.Normalize("batch_operations", args)
		if result.Args["dry_run"] != true {
			t.Error("dry_run string→bool coercion failed")
		}
	})

	// Bug #22 Pattern 7: permanent as string
	t.Run("permanent_string_coerce", func(t *testing.T) {
		args := map[string]interface{}{
			"path":      "/test.txt",
			"permanent": "true",
		}
		result := n.Normalize("delete_file", args)
		if result.Args["permanent"] != true {
			t.Error("permanent string→bool coercion failed")
		}
	})
}
