package core

import (
	"strings"
	"testing"
)

func TestDecodePaths_NativeAndJSONEquivalent(t *testing.T) {
	native, err := DecodePaths([]interface{}{"a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := DecodePaths(`["a.go","b.go"]`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(native, ",") != strings.Join(fromJSON, ",") {
		t.Fatalf("native=%v json=%v", native, fromJSON)
	}
}

func TestDecodePaths_Invalid(t *testing.T) {
	if _, err := DecodePaths(42.0); err == nil {
		t.Fatal("number accepted")
	}
	if _, err := DecodePaths(`{"not":"array"}`); err == nil {
		t.Fatal("object JSON accepted")
	}
	if _, err := DecodePaths([]interface{}{1}); err == nil {
		t.Fatal("non-string item accepted")
	}
}

func TestDecodeDual_ConflictAndEquivalence(t *testing.T) {
	type pair struct {
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	native := []interface{}{map[string]interface{}{"old_text": "a", "new_text": "b"}}
	same, err := DecodeDual[[]pair](native, `[{"old_text":"a","new_text":"b"}]`, "edits", "edits_json")
	if err != nil || len(same) != 1 || same[0].OldText != "a" {
		t.Fatalf("%v %v", same, err)
	}
	if _, err := DecodeDual[[]pair](native, `[{"old_text":"a","new_text":"OTHER"}]`, "edits", "edits_json"); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("want conflict, got %v", err)
	}
	if _, err := DecodeDual[[]pair](nil, "", "edits", "edits_json"); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("want required, got %v", err)
	}
}

func TestDecodeDualOptional_Neither(t *testing.T) {
	_, ok, err := DecodeDualOptional[BatchRequest](nil, "", "request", "request_json")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
