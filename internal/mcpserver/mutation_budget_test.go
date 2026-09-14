package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMutationBudget_ThirdWriteBlocked(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	reg.engine.SetMutationBudget(2)
	for i, name := range []string{"a.txt", "b.txt"} {
		res := callPatchTool(t, reg, "write_file", map[string]any{
			"path": filepath.Join(dir, name), "content": "x",
		})
		if res.IsError {
			t.Fatalf("write %d: %s", i+1, resultText(t, res))
		}
	}
	res := callPatchTool(t, reg, "write_file", map[string]any{
		"path": filepath.Join(dir, "c.txt"), "content": "x",
	})
	if !res.IsError {
		t.Fatal("third write should be blocked")
	}
	raw := resultText(t, res)
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != errCodeBudgetExceeded || env.Error.Retryable {
		t.Fatalf("%+v", env.Error)
	}
	if env.Error.Suggestion != "Raise --mutation-budget or start a new server process." {
		t.Fatalf("suggestion=%q", env.Error.Suggestion)
	}
	if env.Error.Details["used"] != "2" || env.Error.Details["limit"] != "2" {
		t.Fatalf("details=%v", env.Error.Details)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); !os.IsNotExist(err) {
		t.Fatal("blocked write must not create the file")
	}
}

func TestMutationBudget_DryRunDoesNotCount(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "e.txt")
	_ = os.WriteFile(p, []byte("old\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	reg.engine.SetMutationBudget(1)
	dry := callPatchTool(t, reg, "edit_file", map[string]any{
		"path": p, "old_text": "old", "new_text": "new", "dry_run": true,
	})
	if dry.IsError {
		t.Fatal(resultText(t, dry))
	}
	used, limit := reg.engine.MutationBudgetSnapshot()
	if used != 0 || limit != 1 {
		t.Fatalf("dry_run counted: used=%d limit=%d", used, limit)
	}
	ok := callPatchTool(t, reg, "edit_file", map[string]any{
		"path": p, "old_text": "old", "new_text": "new",
	})
	if ok.IsError {
		t.Fatal(resultText(t, ok))
	}
	blocked := callPatchTool(t, reg, "write_file", map[string]any{
		"path": filepath.Join(dir, "n.txt"), "content": "x",
	})
	if !blocked.IsError || !strings.Contains(resultText(t, blocked), `"code":"BUDGET_EXCEEDED"`) {
		t.Fatalf("want BUDGET_EXCEEDED after one real edit, got %s", resultText(t, blocked))
	}
}

func TestMutationBudget_OffByDefault(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for i := 0; i < 3; i++ {
		res := callPatchTool(t, reg, "write_file", map[string]any{
			"path": filepath.Join(dir, "f.txt"), "content": "x",
		})
		if res.IsError {
			t.Fatalf("write %d: %s", i, resultText(t, res))
		}
	}
}
