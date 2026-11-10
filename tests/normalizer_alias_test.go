package tests

import (
	"encoding/json"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// A model that says old_string/new_string or source/destination must not lose
// a round trip to a validation error: the aliases are renamed to the canonical
// parameter names before validation runs.
func TestNormalizer_EditStringAliases(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}
	args := map[string]interface{}{
		"path":       "/test.txt",
		"old_string": "hello",
		"new_string": "world",
	}
	result := n.Normalize("edit_file", args)
	if !result.WasModified {
		t.Fatal("expected normalization to be applied")
	}
	if result.Args["old_text"] != "hello" || result.Args["new_text"] != "world" {
		t.Fatalf("aliases not renamed: %v", result.Args)
	}
	if _, ok := result.Args["old_string"]; ok {
		t.Fatal("old_string should have been removed")
	}
	if errs := core.ValidateToolParams("edit_file", result.Args); len(errs) > 0 {
		t.Fatalf("normalized args must validate: %v", errs)
	}
}

// The canonical name wins when a call carries both spellings.
func TestNormalizer_CanonicalWinsOverAlias(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}
	args := map[string]interface{}{
		"path":       "/test.txt",
		"old_text":   "canonical",
		"old_string": "alias",
	}
	result := n.Normalize("edit_file", args)
	if result.Args["old_text"] != "canonical" {
		t.Fatalf("canonical value must win: %v", result.Args)
	}
}

// The alias names are also part of the tool contract, so a call that reaches
// the validator without the normalizer is not rejected either.
func TestValidateToolParams_AcceptsAliasNames(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]interface{}
	}{
		{"edit_file", map[string]interface{}{"path": "/t.txt", "old_string": "a", "new_string": "b"}},

		{"copy_file", map[string]interface{}{"source_path": "/a", "dest_path": "/b", "source": "/a", "destination": "/b"}},
		{"move_file", map[string]interface{}{"source_path": "/a", "dest_path": "/b", "source": "/a", "destination": "/b"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			if errs := core.ValidateToolParams(tc.tool, tc.args); len(errs) > 0 {
				t.Fatalf("alias names must validate: %v", errs)
			}
		})
	}
}

// multi_edit accepts the aliases inside each item of the native edits array
// and inside the legacy edits_json payload.
func TestNormalizer_MultiEditItemAliases(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}

	t.Run("native_edits", func(t *testing.T) {
		args := map[string]interface{}{
			"path": "/test.txt",
			"edits": []interface{}{
				map[string]interface{}{"old_string": "a", "new_string": "b"},
			},
		}
		result := n.Normalize("multi_edit", args)
		items, ok := result.Args["edits"].([]interface{})
		if !ok || len(items) != 1 {
			t.Fatalf("edits must stay a native array: %#v", result.Args["edits"])
		}
		item, ok := items[0].(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected item type: %#v", items[0])
		}
		if item["old_text"] != "a" || item["new_text"] != "b" {
			t.Fatalf("item aliases not renamed: %#v", item)
		}
		if _, ok := item["old_string"]; ok {
			t.Fatal("old_string should have been removed")
		}
	})

	t.Run("edits_json", func(t *testing.T) {
		args := map[string]interface{}{
			"path":       "/test.txt",
			"edits_json": `[{"old_string":"a","new_string":"b"}]`,
		}
		result := n.Normalize("multi_edit", args)
		raw, ok := result.Args["edits_json"].(string)
		if !ok {
			t.Fatalf("edits_json must stay a string: %#v", result.Args["edits_json"])
		}
		var items []map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			t.Fatalf("edits_json is not valid JSON: %v", err)
		}
		if len(items) != 1 || items[0]["old_text"] != "a" || items[0]["new_text"] != "b" {
			t.Fatalf("json item aliases not renamed: %#v", items)
		}
	})
}

// copy_file and move_file: source/destination are the names models reach for.
func TestNormalizer_CopyMoveAliases(t *testing.T) {
	n, err := core.NewNormalizer("", "")
	if err != nil {
		t.Fatalf("NewNormalizer: %v", err)
	}
	for _, tool := range []string{"copy_file", "move_file"} {
		t.Run(tool, func(t *testing.T) {
			args := map[string]interface{}{"source": "/a.txt", "destination": "/b.txt"}
			result := n.Normalize(tool, args)
			if result.Args["source_path"] != "/a.txt" || result.Args["dest_path"] != "/b.txt" {
				t.Fatalf("aliases not renamed: %v", result.Args)
			}
			if _, ok := result.Args["source"]; ok {
				t.Fatal("source should have been removed")
			}
			if errs := core.ValidateToolParams(tool, result.Args); len(errs) > 0 {
				t.Fatalf("normalized args must validate: %v", errs)
			}
		})
	}
}
