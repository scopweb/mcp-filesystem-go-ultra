package core

import (
	"testing"
)

func TestValidateToolParams_UnknownParam(t *testing.T) {
	args := map[string]interface{}{
		"path":          "/tmp/test.txt",
		"bogus_param":   "should fail",
		"another_bogus": 42.0,
	}
	errs := ValidateToolParams("read_file", args)
	if len(errs) == 0 {
		t.Fatal("expected errors for unknown params, got none")
	}
	foundBogus := false
	foundAnother := false
	for _, e := range errs {
		if strContains(e, "bogus_param") {
			foundBogus = true
		}
		if strContains(e, "another_bogus") {
			foundAnother = true
		}
	}
	if !foundBogus || !foundAnother {
		t.Errorf("expected both unknown params flagged, got: %v", errs)
	}
}

func TestValidateToolParams_MissingRequired(t *testing.T) {
	// edit_file requires "path"
	args := map[string]interface{}{
		"old_text": "foo",
		"new_text": "bar",
	}
	errs := ValidateToolParams("edit_file", args)
	foundMissing := false
	for _, e := range errs {
		if strContains(e, "path") && strContains(e, "required") {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("expected missing required 'path' error, got: %v", errs)
	}
}

func TestValidateToolParams_TypeMismatch(t *testing.T) {
	args := map[string]interface{}{
		"path":       "/tmp/test.txt",
		"start_line": "not_a_number", // should be float64
	}
	errs := ValidateToolParams("read_file", args)
	foundType := false
	for _, e := range errs {
		if strContains(e, "start_line") && strContains(e, "number") {
			foundType = true
		}
	}
	if !foundType {
		t.Errorf("expected type mismatch for start_line, got: %v", errs)
	}
}

func TestValidateToolParams_ValidParams(t *testing.T) {
	args := map[string]interface{}{
		"path":       "/tmp/test.txt",
		"max_lines":  100.0,
		"mode":       "head",
		"start_line": 10.0,
		"end_line":   20.0,
		"encoding":   "base64",
	}
	errs := ValidateToolParams("read_file", args)
	if len(errs) > 0 {
		t.Errorf("expected no errors for valid params, got: %v", errs)
	}
}

func TestValidateToolParams_PathOrPaths(t *testing.T) {
	// read_file: "path" is required, but "paths" should satisfy the requirement
	args := map[string]interface{}{
		"paths": `["file1.txt", "file2.txt"]`,
	}
	errs := ValidateToolParams("read_file", args)
	for _, e := range errs {
		if strContains(e, "missing required") && strContains(e, "path") {
			t.Errorf("should not require 'path' when 'paths' is provided, got: %v", errs)
		}
	}
}

func TestValidateToolParams_UnknownTool(t *testing.T) {
	// Unknown tools should pass validation (no schema = no checks)
	args := map[string]interface{}{
		"anything": "goes",
	}
	errs := ValidateToolParams("nonexistent_tool", args)
	if len(errs) > 0 {
		t.Errorf("unknown tool should not produce errors, got: %v", errs)
	}
}

func TestValidateToolParams_AllToolsRegistered(t *testing.T) {
	expected := []string{
		"read_file", "write_file", "edit_file", "list_directory", "search_files",
		"multi_edit", "move_file", "copy_file", "delete_file", "create_directory",
		"batch_operations", "backup", "analyze_operation", "wsl", "server_info",
		"get_file_info", "search", "edit", "write", "help",
	}
	for _, tool := range expected {
		if _, ok := toolSchemas[tool]; !ok {
			t.Errorf("tool %q missing from schema registry", tool)
		}
	}
}

func TestValidateToolParams_Aliases(t *testing.T) {
	// edit_file accepts old_str/new_str as aliases
	args := map[string]interface{}{
		"path":    "/tmp/test.txt",
		"old_str": "foo",
		"new_str": "bar",
	}
	errs := ValidateToolParams("edit_file", args)
	if len(errs) > 0 {
		t.Errorf("aliases should be accepted, got: %v", errs)
	}
}

func strContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
