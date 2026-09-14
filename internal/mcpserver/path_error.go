package mcpserver

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const (
	errCodeNotAllowed     = "NOT_ALLOWED"
	errCodeNotFound       = "NOT_FOUND"
	errCodeOCCMismatch    = "OCC_MISMATCH"
	errCodeRewriteBlocked = "REWRITE_BLOCKED"
	errCodeRootsEmpty     = "ROOTS_EMPTY"
	errCodeSecretDenied   = "SECRET_DENIED"
	errCodeReadOnly       = "READONLY"
	errCodePatchFailed    = "PATCH_FAILED"
	errCodeInvalidParams  = "VALIDATION"
	errCodeHashRequired   = "HASH_REQUIRED"
	errCodeUnavailable    = "TOOL_UNAVAILABLE"
)

type pathErrorBody struct {
	Code         string                  `json:"code"`
	Message      string                  `json:"message"`
	Path         string                  `json:"path,omitempty"`
	Details      map[string]string       `json:"details,omitempty"`
	ExpectedHash string                  `json:"expected_hash,omitempty"`
	CurrentHash  string                  `json:"current_hash,omitempty"`
	Conflict     *core.OCCConflictReport `json:"conflict,omitempty"`
	Suggestion   string                  `json:"suggestion,omitempty"`
	Retryable    bool                    `json:"retryable"`
}

type pathErrorEnvelope struct {
	Error pathErrorBody `json:"error"`
}

func pathErrorJSON(code, message, path string, details map[string]string, suggestion string) string {
	env := pathErrorEnvelope{Error: pathErrorBody{
		Code:       code,
		Message:    message,
		Path:       path,
		Details:    details,
		Suggestion: suggestion,
		Retryable:  retryableForCode(code),
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

func occMismatchResult(message, path, expectedHash, currentHash string, conflict core.OCCConflictReport) *mcp.CallToolResult {
	if conflict.Path == "" {
		conflict.Path = path
	}
	return mcp.NewToolResultError(pathErrorJSONWithOCC(message, path, expectedHash, currentHash, conflict))
}

func pathErrorJSONWithOCC(message, path, expectedHash, currentHash string, conflict core.OCCConflictReport) string {
	env := pathErrorEnvelope{Error: pathErrorBody{
		Code:         errCodeOCCMismatch,
		Message:      message,
		Path:         path,
		ExpectedHash: expectedHash,
		CurrentHash:  currentHash,
		Conflict:     &conflict,
		Suggestion:   "Rebase against current content; retry with current_hash.",
		Retryable:    true,
	}}
	b, err := json.Marshal(env)
	if err != nil {
		return message
	}
	return string(b)
}
