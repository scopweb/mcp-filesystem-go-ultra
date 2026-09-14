package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestPatchFailedResult_ContextNotFound(t *testing.T) {
	res := patchFailedResult(`C:\x.go`, &core.PatchError{
		HunkIndex: 1, Reason: core.PatchReasonContextNotFound, LineHint: 3, Msg: "hunk 1: context mismatch at line 3",
	}, nil)
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != errCodePatchFailed || env.Error.Retryable {
		t.Fatalf("%+v", env.Error)
	}
	if env.Error.Suggestion != "re-read the file and regenerate the patch" {
		t.Fatalf("suggestion=%q", env.Error.Suggestion)
	}
	if env.Error.Details["reason"] != "context_not_found" || env.Error.Details["hunk_index"] != "1" || env.Error.Details["line_hint"] != "3" {
		t.Fatalf("details=%v", env.Error.Details)
	}
}

func TestRewriteBlockedResult_Details(t *testing.T) {
	res := rewriteBlockedResult(`C:\x.go`, "blocked", 10, 900)
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != errCodeRewriteBlocked || env.Error.Retryable {
		t.Fatalf("%+v", env.Error)
	}
	if env.Error.Suggestion != "use write_file or allow_rewrite:true" {
		t.Fatalf("suggestion=%q", env.Error.Suggestion)
	}
	if env.Error.Details["pattern"] != "accidental_rewrite" || env.Error.Details["old_len"] != "10" || env.Error.Details["new_len"] != "900" {
		t.Fatalf("details=%v", env.Error.Details)
	}
}

func TestHashRequiredResult_Details(t *testing.T) {
	res := hashRequiredResult(`C:\x.go`, "abcd1234")
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != errCodeHashRequired || !env.Error.Retryable {
		t.Fatalf("%+v", env.Error)
	}
	if env.Error.Suggestion != "Read the file and resend the mutation with expected_hash." {
		t.Fatalf("suggestion=%q", env.Error.Suggestion)
	}
	if env.Error.Details["path"] != `C:\x.go` {
		t.Fatalf("details=%v", env.Error.Details)
	}
}

func TestValidationResult_Details(t *testing.T) {
	res := validationResult("bad", "mode", "string")
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != errCodeInvalidParams || env.Error.Retryable {
		t.Fatalf("%+v", env.Error)
	}
	if env.Error.Suggestion != "Correct the listed parameters; do not retry with the same arguments" {
		t.Fatalf("suggestion=%q", env.Error.Suggestion)
	}
	if env.Error.Details["field"] != "mode" || env.Error.Details["expected"] != "string" {
		t.Fatalf("details=%v", env.Error.Details)
	}
}

func TestNotAllowedDetails_RootsArray(t *testing.T) {
	pe := &core.PathError{Path: `C:\x`, Roots: []string{`C:\proj`}, Source: "cli"}
	d := notAllowedDetailsFromPathError(pe)
	roots, ok := d["roots"].([]string)
	if !ok || len(roots) != 1 || roots[0] != `C:\proj` || d["source"] != "cli" {
		t.Fatalf("details=%v", d)
	}
}

func TestValidationFieldExpected(t *testing.T) {
	field, expected := validationFieldExpected(`unknown parameter "foo" (did you mean "path"?)`)
	if field != "foo" || expected != "known parameter" {
		t.Fatalf("field=%q expected=%q", field, expected)
	}
	field, expected = validationFieldExpected(`parameter "mode": expected string, got float64`)
	if field != "mode" || expected != "string" {
		t.Fatalf("field=%q expected=%q", field, expected)
	}
}
