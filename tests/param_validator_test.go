package main

import (
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// TestSearchFilesParams tests search_files parameter validation
func TestSearchFilesParams(t *testing.T) {
	t.Run("output_format is valid", func(t *testing.T) {
		errs := core.ValidateToolParams("search_files", map[string]interface{}{
			"path": ".", "pattern": "foo", "output_format": "json",
		})
		if len(errs) > 0 {
			t.Errorf("output_format should be valid: %v", errs)
		}
	})

	t.Run("output alias is valid", func(t *testing.T) {
		errs := core.ValidateToolParams("search_files", map[string]interface{}{
			"path": ".", "pattern": "foo", "output": "content",
		})
		if len(errs) > 0 {
			t.Errorf("output should be valid: %v", errs)
		}
	})

	t.Run("search alias has same params", func(t *testing.T) {
		errs := core.ValidateToolParams("search", map[string]interface{}{
			"path": ".", "pattern": "foo", "output_format": "json",
		})
		if len(errs) > 0 {
			t.Errorf("output_format should be valid in search alias: %v", errs)
		}
	})

	t.Run("unknown param is rejected", func(t *testing.T) {
		errs := core.ValidateToolParams("search_files", map[string]interface{}{
			"path": ".", "pattern": "foo", "unknown_param": "bar",
		})
		if len(errs) == 0 {
			t.Error("unknown_param should be rejected")
		}
	})

	t.Run("return_lines string is valid", func(t *testing.T) {
		errs := core.ValidateToolParams("search_files", map[string]interface{}{
			"path": ".", "pattern": "foo", "return_lines": "true",
		})
		if len(errs) > 0 {
			t.Errorf("return_lines string should be valid: %v", errs)
		}
	})

	t.Run("return_lines bool true is valid", func(t *testing.T) {
		errs := core.ValidateToolParams("search_files", map[string]interface{}{
			"path": ".", "pattern": "foo", "return_lines": true,
		})
		if len(errs) > 0 {
			t.Errorf("return_lines bool should be valid: %v", errs)
		}
	})

	t.Run("include alias works", func(t *testing.T) {
		errs := core.ValidateToolParams("search_files", map[string]interface{}{
			"path": ".", "pattern": "foo", "include": "*.go",
		})
		if len(errs) > 0 {
			t.Errorf("include should be valid: %v", errs)
		}
	})
}

// TestAllToolsParams tests that all tool schemas include all their defined params
func TestAllToolsParams(t *testing.T) {
	// Map of tool name → params that appear in the MCP tool definition
	// but might be missing from the param_validator schema
	knownGaps := map[string]map[string]bool{
		"search_files": {
			"output_format": true,
			"output":        true,
		},
		"search": {
			"output_format": true,
			"output":        true,
		},
	}

	for toolName, expectedParams := range knownGaps {
		for param := range expectedParams {
			errs := core.ValidateToolParams(toolName, map[string]interface{}{
				"path": ".", "pattern": "foo", param: "test",
			})
			if len(errs) > 0 {
				t.Errorf("%s.%s: should be valid but got: %v", toolName, param, errs)
			}
		}
	}
}

// TestReadFileParams tests read_file parameter validation
func TestReadFileParams(t *testing.T) {
	errs := core.ValidateToolParams("read_file", map[string]interface{}{
		"path": "test.txt",
	})
	if len(errs) > 0 {
		t.Errorf("basic read_file should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("read_file", map[string]interface{}{
		"paths": `["a.txt","b.txt"]`,
	})
	if len(errs) > 0 {
		t.Errorf("batch read_file should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("read_file", map[string]interface{}{
		"path": "test.txt", "start_line": 1.0, "end_line": 10.0,
	})
	if len(errs) > 0 {
		t.Errorf("range read should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("read_file", map[string]interface{}{
		"path": "test.txt", "encoding": "base64",
	})
	if len(errs) > 0 {
		t.Errorf("base64 read should be valid: %v", errs)
	}
}

// TestWriteFileParams tests write_file parameter validation
func TestWriteFileParams(t *testing.T) {
	errs := core.ValidateToolParams("write_file", map[string]interface{}{
		"path": "test.txt", "content": "hello",
	})
	if len(errs) > 0 {
		t.Errorf("write_file should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("write_file", map[string]interface{}{
		"path": "test.txt", "content_base64": "aGVsbG8=",
	})
	if len(errs) > 0 {
		t.Errorf("base64 write should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("write_file", map[string]interface{}{
		"path": "test.txt", "content": "hello", "encoding": "base64",
	})
	if len(errs) > 0 {
		t.Errorf("base64 write via encoding should be valid: %v", errs)
	}
}

// TestEditFileParams tests edit_file parameter validation
func TestEditFileParams(t *testing.T) {
	errs := core.ValidateToolParams("edit_file", map[string]interface{}{
		"path": "test.txt", "old_text": "a", "new_text": "b",
	})
	if len(errs) > 0 {
		t.Errorf("basic edit_file should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("edit_file", map[string]interface{}{
		"path": "test.txt", "old_str": "a", "new_str": "b",
	})
	if len(errs) > 0 {
		t.Errorf("old_str/new_str alias should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("edit_file", map[string]interface{}{
		"path": "test.txt", "mode": "search_replace", "pattern": "foo", "replacement": "bar",
	})
	if len(errs) > 0 {
		t.Errorf("search_replace mode should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("edit_file", map[string]interface{}{
		"path": "test.txt", "mode": "regex", "patterns_json": `[{"pattern":"\\d+","replacement":"X"}]`,
	})
	if len(errs) > 0 {
		t.Errorf("regex mode should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("edit_file", map[string]interface{}{
		"path": "test.txt", "old_text": "a", "new_text": "b", "occurrence": 1.0,
	})
	if len(errs) > 0 {
		t.Errorf("occurrence param should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("edit_file", map[string]interface{}{
		"path": "test.txt", "old_text": "a", "new_text": "b", "dry_run": true,
	})
	if len(errs) > 0 {
		t.Errorf("dry_run param should be valid: %v", errs)
	}
}

// TestMultiEditParams tests multi_edit parameter validation
func TestMultiEditParams(t *testing.T) {
	errs := core.ValidateToolParams("multi_edit", map[string]interface{}{
		"path": "test.txt", "edits_json": `[{"old_text":"a","new_text":"b"}]`,
	})
	if len(errs) > 0 {
		t.Errorf("multi_edit should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("multi_edit", map[string]interface{}{
		"path": "test.txt", "edits_json": `[{"old_text":"a","new_text":"b"}]`, "force": true,
	})
	if len(errs) > 0 {
		t.Errorf("force param should be valid: %v", errs)
	}
}

// TestBatchOperationsParams tests batch_operations parameter validation
func TestBatchOperationsParams(t *testing.T) {
	errs := core.ValidateToolParams("batch_operations", map[string]interface{}{
		"request_json": `{"operations":[]}`,
	})
	if len(errs) > 0 {
		t.Errorf("request_json should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("batch_operations", map[string]interface{}{
		"pipeline_json": `{"name":"test","steps":[]}`,
	})
	if len(errs) > 0 {
		t.Errorf("pipeline_json should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("batch_operations", map[string]interface{}{
		"rename_json": `{"path":".","find":"a","replace":"b"}`,
	})
	if len(errs) > 0 {
		t.Errorf("rename_json should be valid: %v", errs)
	}
}

// TestProjectReplaceParams tests project_replace parameter validation
func TestProjectReplaceParams(t *testing.T) {
	params := map[string]interface{}{
		"path": ".", "find": "foo", "replace": "bar",
		"literal": true, "case_sensitive": true,
		"file_types": ".go", "preview": true,
		"create_backup": true, "parallel": true,
		"max_files": 100.0, "force": true,
	}
	errs := core.ValidateToolParams("project_replace", params)
	if len(errs) > 0 {
		t.Errorf("project_replace all params should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("project_replace", map[string]interface{}{
		"path": ".", "find": "foo", "replace": "bar",
		"include_paths": `["*.go","*.ts"]`,
		"exclude_paths": `["node_modules/**"]`,
	})
	if len(errs) > 0 {
		t.Errorf("include_paths/exclude_paths should be valid: %v", errs)
	}
}

// TestBackupParams tests backup parameter validation
func TestBackupParams(t *testing.T) {
	errs := core.ValidateToolParams("backup", map[string]interface{}{
		"action": "list",
	})
	if len(errs) > 0 {
		t.Errorf("backup list should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("backup", map[string]interface{}{
		"action": "restore", "backup_id": "test-123",
	})
	if len(errs) > 0 {
		t.Errorf("restore action should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("backup", map[string]interface{}{
		"action": "undo_last", "file_path": "test.txt", "preview": true,
	})
	if len(errs) > 0 {
		t.Errorf("undo_last with preview should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("backup", map[string]interface{}{
		"action": "cleanup", "older_than_days": 7.0, "dry_run": true,
	})
	if len(errs) > 0 {
		t.Errorf("cleanup with older_than_days should be valid: %v", errs)
	}
}

// TestWSLParams tests wsl parameter validation
func TestWSLParams(t *testing.T) {
	errs := core.ValidateToolParams("wsl", map[string]interface{}{
		"action": "status",
	})
	if len(errs) > 0 {
		t.Errorf("wsl status should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("wsl", map[string]interface{}{
		"wsl_path": "/mnt/c/test.txt", "windows_path": "C:\\test.txt",
	})
	if len(errs) > 0 {
		t.Errorf("wsl sync should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("wsl", map[string]interface{}{
		"action": "autosync_config", "enabled": true,
		"sync_on_write": true, "silent": false,
	})
	if len(errs) > 0 {
		t.Errorf("autosync_config should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("wsl", map[string]interface{}{
		"direction": "wsl_to_windows", "dry_run": true,
	})
	if len(errs) > 0 {
		t.Errorf("dry_run should be valid: %v", errs)
	}
}

// TestAnalyzeOperationParams tests analyze_operation parameter validation
func TestAnalyzeOperationParams(t *testing.T) {
	errs := core.ValidateToolParams("analyze_operation", map[string]interface{}{
		"operation": "file", "path": "test.txt",
	})
	if len(errs) > 0 {
		t.Errorf("analyze file should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("analyze_operation", map[string]interface{}{
		"operation": "edit", "path": "test.txt",
		"old_text": "a", "new_text": "b",
	})
	if len(errs) > 0 {
		t.Errorf("analyze edit should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("analyze_operation", map[string]interface{}{
		"operation": "write", "path": "test.txt", "content": "hello",
	})
	if len(errs) > 0 {
		t.Errorf("analyze write should be valid: %v", errs)
	}
}

// TestDeleteFileParams tests delete_file parameter validation
func TestDeleteFileParams(t *testing.T) {
	errs := core.ValidateToolParams("delete_file", map[string]interface{}{
		"path": "test.txt",
	})
	if len(errs) > 0 {
		t.Errorf("delete_file should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("delete_file", map[string]interface{}{
		"paths": `["a.txt","b.txt"]`,
	})
	if len(errs) > 0 {
		t.Errorf("batch delete should be valid: %v", errs)
	}

	errs = core.ValidateToolParams("delete_file", map[string]interface{}{
		"path": "test.txt", "permanent": true,
	})
	if len(errs) > 0 {
		t.Errorf("permanent delete should be valid: %v", errs)
	}
}
