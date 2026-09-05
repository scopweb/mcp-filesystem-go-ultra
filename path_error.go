package main

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	errCodeNotAllowed     = "NOT_ALLOWED"
	errCodeNotFound       = "NOT_FOUND"
	errCodeOCCMismatch    = "OCC_MISMATCH"
	errCodeRewriteBlocked = "REWRITE_BLOCKED"
	errCodeRootsEmpty     = "ROOTS_EMPTY"
	errCodeSecretDenied   = "SECRET_DENIED"
	errCodeReadOnly       = "READ_ONLY"
	errCodePatchFailed    = "PATCH_APPLY_FAILED"
)

type pathErrorBody struct {
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Path       string            `json:"path,omitempty"`
	Details    map[string]string `json:"details,omitempty"`
	Suggestion string            `json:"suggestion,omitempty"`
}

type pathErrorEnvelope struct {
	Error pathErrorBody `json:"error"`
}

func pathErrorJSON(code, message, path string, details map[string]string, suggestion string) string {
	env := pathErrorEnvelope{Error: pathErrorBody{
		Code: code, Message: message, Path: path, Details: details, Suggestion: suggestion,
	}}
	b, err := json.Marshal(env)
	if err != nil {
		return message
	}
	return string(b)
}

func pathErrorResult(code, message, path string, details map[string]string, suggestion string) *mcp.CallToolResult {
	return mcp.NewToolResultError(pathErrorJSON(code, message, path, details, suggestion))
}
