package main

import (
	"encoding/json"
	"errors"
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
