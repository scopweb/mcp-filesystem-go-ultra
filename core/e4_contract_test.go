package core

import (
	"strings"
	"testing"
)

func TestE4_InvalidEnumRejected(t *testing.T) {
	errs := ValidateToolParams("edit_file", map[string]interface{}{
		"path": "a.txt", "mode": "nope",
	})
	if len(errs) == 0 || !strings.Contains(errs[0], "invalid value") {
		t.Fatalf("%v", errs)
	}
}

func TestE4_NativePathsAccepted(t *testing.T) {
	if errs := ValidateToolParams("read_file", map[string]interface{}{
		"paths": []interface{}{"a.txt", "b.txt"},
	}); len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
	if errs := ValidateToolParams("read_file", map[string]interface{}{
		"paths": `["a.txt"]`,
	}); len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
}

func TestE4_EditsOrEditsJSON(t *testing.T) {
	if errs := ValidateToolParams("multi_edit", map[string]interface{}{
		"path":  "a.txt",
		"edits": []interface{}{map[string]interface{}{"old_text": "a", "new_text": "b"}},
	}); len(errs) != 0 {
		t.Fatalf("native edits: %v", errs)
	}
	if errs := ValidateToolParams("multi_edit", map[string]interface{}{"path": "a.txt"}); len(errs) == 0 {
		t.Fatal("missing edits must fail")
	}
}

func TestE4_BatchFamilyConflict(t *testing.T) {
	errs := ValidateToolParams("batch_operations", map[string]interface{}{
		"request_json":  `{}`,
		"pipeline_json": `{}`,
	})
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, " "), "incompatible") {
		t.Fatalf("%v", errs)
	}
}

func TestE4_EndLineAndMaxLines(t *testing.T) {
	errs := ValidateToolParams("read_file", map[string]interface{}{
		"path": "a.txt", "start_line": 1.0, "end_line": 2.0, "max_lines": 3.0,
	})
	if len(errs) == 0 {
		t.Fatal("expected incompatible end_line/max_lines")
	}
}

func TestE4_NonIntegerLine(t *testing.T) {
	errs := ValidateToolParams("edit_file", map[string]interface{}{
		"path": "a.txt", "start_line": 1.5,
	})
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, " "), "integer") {
		t.Fatalf("%v", errs)
	}
}
