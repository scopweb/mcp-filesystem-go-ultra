package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestEdit_ReplacementWithoutNewText_Trial3PriceGo(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "price.go")
	original := "package price\n\nimport \"fmt\"\n\n\n\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path":        path,
		"old_text":    original,
		"replacement": "package price\n\nfunc Total() {}\n",
	})
	if !res.IsError {
		t.Fatal("replacement without new_text must be rejected")
	}
	text := resultText(t, res)
	if !strings.Contains(text, `"code":"VALIDATION"`) || !strings.Contains(text, `"recovery":"fix_arguments"`) || strings.Contains(text, "next_call") {
		t.Fatalf("got %s", text)
	}
	mustFile(t, path, original)
}

func TestEdit_ExplicitEmptyNewTextStillDeletes(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "price.go")
	original := "package price\n\nkeep\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_text": "keep\n", "new_text": "",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	mustFile(t, path, "package price\n\n")
}

func TestEdit_NewStrAliasIsNotADelete(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "price.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]interface{}{
		"path": path, "old_str": "alpha", "new_str": "beta",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	mustFile(t, path, "beta\n")
}

func TestBatchRequestDescription_ListsOperationTypes(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	tool, ok := reg.server.ListTools()["batch_operations"]
	if !ok {
		t.Fatal("batch_operations missing")
	}
	req, ok := tool.Tool.InputSchema.Properties["request"].(map[string]any)
	if !ok {
		t.Fatalf("request schema %T", tool.Tool.InputSchema.Properties["request"])
	}
	desc, _ := req["description"].(string)
	for _, op := range []string{"write", "edit", "search_and_replace", "copy", "move", "delete", "create_dir", "extract"} {
		if !strings.Contains(desc, op) {
			t.Errorf("request description missing %q: %s", op, desc)
		}
	}
	legacy, _ := tool.Tool.InputSchema.Properties["request_json"].(map[string]any)
	legacyDesc, _ := legacy["description"].(string)
	if !strings.Contains(legacyDesc, "Legacy") || strings.Contains(legacyDesc, "search_and_replace") {
		t.Fatalf("request_json should stay a legacy adapter, got %q", legacyDesc)
	}
	raw, err := json.Marshal(tool.Tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "oneOf") || strings.Contains(string(raw), "anyOf") {
		t.Fatalf("schema introduced oneOf/anyOf: %s", raw)
	}
}

func TestHarnessDocs_NoOneFilePerCall(t *testing.T) {
	paths := []string{
		filepath.Join("..", "..", "examples", "harness", "README.md"),
		filepath.Join("..", "..", "examples", "harness", "codex", "README.md"),
		filepath.Join("..", "..", "examples", "harness", "claude-code", ".claude", "agents", "fs-editor.md"),
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "one file per call") {
			t.Errorf("%s still says one file per call", p)
		}
	}
}

func TestErrorEnvelope_RecoveryClosedEnum(t *testing.T) {
	allowed := map[string]bool{
		recoveryRetrySame:           true,
		recoveryFixArguments:        true,
		recoveryReadAndRebase:       true,
		recoveryCheckAlreadyApplied: true,
	}
	cases := []struct{ code, want string }{
		{errCodeOCCMismatch, recoveryReadAndRebase},
		{errCodeHashRequired, recoveryReadAndRebase},
		{errCodePatchFailed, recoveryReadAndRebase},
		{errCodeInvalidParams, recoveryFixArguments},
		{errCodeNotFound, recoveryFixArguments},
		{errCodeBeginPatch, recoveryFixArguments},
		{errCodeRollbackPartial, recoveryCheckAlreadyApplied},
		{errCodeRollbackFailed, recoveryCheckAlreadyApplied},
		{errCodeRollbackComplete, recoveryCheckAlreadyApplied},
	}
	for _, c := range cases {
		got := recoveryForCode(c.code)
		if got != c.want || !allowed[got] {
			t.Errorf("%s: got %q want %q", c.code, got, c.want)
		}
	}
	if !allowed[recoveryForCode("NO_SUCH")] {
		t.Fatal("unknown code left the closed enum")
	}
	occ := occMismatchResult("stale", `C:\x`, "aaaa", "bbbb", core.OCCConflictReport{})
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(resultText(t, occ)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Recovery != recoveryReadAndRebase || strings.Contains(resultText(t, occ), "next_call") {
		t.Fatalf("%+v", env.Error)
	}
}
