package mcpserver

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestPathErrorJSON_Envelope(t *testing.T) {
	raw := pathErrorJSON(errCodeNotAllowed, "nope", `C:\x`, map[string]string{"k": "v"}, "call list_allowed_directories")
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != errCodeNotAllowed || env.Error.Path != `C:\x` {
		t.Fatalf("%+v", env)
	}
	if env.Error.Retryable {
		t.Fatal("NOT_ALLOWED must not be retryable")
	}
}

func TestRetryableForCode_FailureIntelligence(t *testing.T) {
	cases := map[string]bool{
		errCodeOCCMismatch:    true,
		errCodeHashRequired:   true,
		errCodePatchFailed:    false,
		errCodeRewriteBlocked: false,
		errCodeNotAllowed:     false,
		errCodeSecretDenied:   false,
		errCodeReadOnly:       false,
		errCodeInvalidParams:  false,
		errCodeNotFound:       false,
		errCodeUnavailable:    false,
	}
	for code, want := range cases {
		if got := retryableForCode(code); got != want {
			t.Errorf("retryableForCode(%q)=%v want %v", code, got, want)
		}
	}
}

func TestPathErrorJSON_RequiredFailureIntelligenceFields(t *testing.T) {
	raw := pathErrorJSON(errCodeOCCMismatch, "stale edit", `C:\x`, map[string]string{"current_hash": "new"}, "Rebase")
	var payload map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	errBody := payload["error"]
	for _, key := range []string{"code", "retryable", "message"} {
		if _, ok := errBody[key]; !ok {
			t.Errorf("missing %q in %#v", key, errBody)
		}
	}
	if errBody["retryable"] != true {
		t.Fatalf("OCC_MISMATCH must be retryable: %#v", errBody)
	}
}
func TestFormatToolError_AccessDeniedEnvelope(t *testing.T) {
	err := &core.PathError{Op: "read", Path: `C:\secret`, Err: errors.New("access denied — outside allowed directories: C:\\proj")}
	got := formatToolError(err)
	if !strings.Contains(got, `"code":"NOT_ALLOWED"`) {
		t.Fatalf("want envelope, got %s", got)
	}
	if !strings.Contains(got, "list_allowed_directories") {
		t.Fatalf("missing suggestion: %s", got)
	}
}

func TestFormatToolError_NotFoundUnchangedPrefix(t *testing.T) {
	err := errors.New("something else")
	got := formatToolError(err)
	if !strings.HasPrefix(got, "Error: ") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatToolError_NotFoundEnvelope(t *testing.T) {
	err := &core.PathError{Op: "read", Path: `C:\proj\missing.txt`, Err: fs.ErrNotExist}
	got := formatToolError(err)
	var env pathErrorEnvelope
	if json.Unmarshal([]byte(got), &env) != nil {
		t.Fatalf("want JSON envelope, got %s", got)
	}
	if env.Error.Code != errCodeNotFound {
		t.Fatalf("code=%q", env.Error.Code)
	}
	if env.Error.Path != `C:\proj\missing.txt` {
		t.Fatalf("path=%q", env.Error.Path)
	}
	if !strings.Contains(env.Error.Suggestion, "FILESYSTEM MISMATCH?") {
		t.Fatalf("suggestion=%q", env.Error.Suggestion)
	}
}
