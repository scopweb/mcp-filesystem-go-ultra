package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The verbose edit_file response carries both the unified diff and a
// "Change Analysis" block. The analysis used to be computed from the file
// AFTER the write, so the exact-occurrence count was 0 and the response
// claimed "No matches found - operation will have no effect" for an edit
// that had just been applied — the message contradicted itself.
func TestEditFile_Response_DoesNotContradictItself(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Program.cs")
	original := "using System;\r\nusing System.Text;\r\nvar x = 1;\r\n"
	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	reg := buildEditRegistry(t, dir, false)

	result := callEdit(t, reg, map[string]interface{}{
		"path":        file,
		"old_text":    "using System;\nusing System.Text;\nvar x = 1;",
		"new_text":    "using System;\nusing System.Text;\nvar x = 2;",
		"diff_format": "full",
	})

	text := resultText(t, result)
	t.Logf("edit_file verbose response:\n%s", text)
	if result.IsError {
		t.Fatalf("edit failed: %s", text)
	}
	if !strings.Contains(text, "--- a/") {
		t.Fatalf("expected a diff in the verbose response:\n%s", text)
	}
	if !strings.Contains(text, "Change Analysis") {
		t.Fatalf("expected a change analysis block:\n%s", text)
	}
	for _, bad := range []string{
		"No matches found",
		"No exact matches found",
		"No changes will be made",
	} {
		if strings.Contains(text, bad) {
			t.Errorf("response contradicts the applied edit (%q):\n%s", bad, text)
		}
	}

	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "var x = 2;") {
		t.Errorf("edit was not applied to the file: %q", string(after))
	}
}
