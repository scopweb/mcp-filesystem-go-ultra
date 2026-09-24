package mcpserver

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const (
	errCodeNotAllowed       = "NOT_ALLOWED"
	errCodeNotFound         = "NOT_FOUND"
	errCodeOCCMismatch      = "OCC_MISMATCH"
	errCodeRewriteBlocked   = "REWRITE_BLOCKED"
	errCodeRootsEmpty       = "ROOTS_EMPTY"
	errCodeSecretDenied     = "SECRET_DENIED"
	errCodeReadOnly         = "READONLY"
	errCodePatchFailed      = "PATCH_FAILED"
	errCodeInvalidParams    = "VALIDATION"
	errCodeHashRequired     = "HASH_REQUIRED"
	errCodeUnavailable      = "TOOL_UNAVAILABLE"
	errCodeBudgetExceeded   = "BUDGET_EXCEEDED"
	errCodeRollbackPartial  = "ROLLBACK_PARTIAL"
	errCodeRollbackFailed   = "ROLLBACK_FAILED"
	errCodeRollbackComplete = "ROLLBACK_COMPLETE"
	errCodeBeginPatch       = "BEGIN_PATCH"
)

const (
	recoveryRetrySame           = "retry_same"
	recoveryFixArguments        = "fix_arguments"
	recoveryReadAndRebase       = "read_and_rebase"
	recoveryCheckAlreadyApplied = "check_already_applied"
)

const (
	suggestionPatchFailed    = "re-read the file and regenerate the patch"
	suggestionBaseMissing    = "create the directory or pass an existing directory; the patch is fine"
	suggestionRewriteBlocked = "use write_file or allow_rewrite:true"
	suggestionNotAllowed     = "call list_allowed_directories"
	suggestionHashRequired   = "Read the file and resend the mutation with expected_hash."
	suggestionValidation     = "Correct the listed parameters; do not retry with the same arguments"
	suggestionOCCMismatch    = "Rebase against current content; retry with current_hash."
	suggestionBudgetExceeded = "Raise --mutation-budget or start a new server process."
	suggestionBeginPatch     = "send a unified diff; do not retry this patch"
	beginPatchExample        = "--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n"
)

type pathErrorBody struct {
	Code         string                  `json:"code"`
	Message      string                  `json:"message"`
	Path         string                  `json:"path,omitempty"`
	Details      map[string]any          `json:"details,omitempty"`
	ExpectedHash string                  `json:"expected_hash,omitempty"`
	CurrentHash  string                  `json:"current_hash,omitempty"`
	Conflict     *core.OCCConflictReport `json:"conflict,omitempty"`
	Suggestion   string                  `json:"suggestion,omitempty"`
	Retryable    bool                    `json:"retryable"`
	Recovery     string                  `json:"recovery,omitempty"`
}

type pathErrorEnvelope struct {
	Error pathErrorBody `json:"error"`
}

func toAnyDetails(details map[string]string) map[string]any {
	if details == nil {
		return nil
	}
	out := make(map[string]any, len(details))
	for k, v := range details {
		out[k] = v
	}
	return out
}

func marshalPathError(code, message, path string, details map[string]any, suggestion string) string {
	env := pathErrorEnvelope{Error: pathErrorBody{
		Code:       code,
		Message:    message,
		Path:       path,
		Details:    details,
		Suggestion: suggestion,
		Retryable:  retryableForCode(code),
		Recovery:   recoveryForCode(code),
	}}
	b, err := json.Marshal(env)
	if err != nil {
		return message
	}
	return string(b)
}

func pathErrorJSON(code, message, path string, details map[string]string, suggestion string) string {
	return marshalPathError(code, message, path, toAnyDetails(details), suggestion)
}

func pathErrorResult(code, message, path string, details map[string]string, suggestion string) *mcp.CallToolResult {
	return mcp.NewToolResultError(pathErrorJSON(code, message, path, details, suggestion))
}

func pathErrorResultDetails(code, message, path string, details map[string]any, suggestion string) *mcp.CallToolResult {
	return mcp.NewToolResultError(marshalPathError(code, message, path, details, suggestion))
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
		Suggestion:   suggestionOCCMismatch,
		Retryable:    true,
		Recovery:     recoveryForCode(errCodeOCCMismatch),
	}}
	b, err := json.Marshal(env)
	if err != nil {
		return message
	}
	return string(b)
}

func recoveryForCode(code string) string {
	switch code {
	case errCodeOCCMismatch, errCodeHashRequired, errCodePatchFailed:
		return recoveryReadAndRebase
	case errCodeRollbackPartial, errCodeRollbackFailed, errCodeRollbackComplete:
		return recoveryCheckAlreadyApplied
	default:
		return recoveryFixArguments
	}
}

func beginPatchResult(path string) *mcp.CallToolResult {
	return pathErrorResult(errCodeBeginPatch,
		"patch looks like Codex *** Begin Patch, not a unified diff. Do not retry this patch. Example:\n"+beginPatchExample,
		path, map[string]string{"field": "patch", "expected": "unified diff"}, suggestionBeginPatch)
}

func patchFailedResult(path string, err error, extra map[string]string) *mcp.CallToolResult {
	pe := core.AsPatchError(err)
	details := map[string]any{"path": path, "reason": pe.Reason}
	if pe.HunkIndex > 0 {
		details["hunk_index"] = strconv.Itoa(pe.HunkIndex)
	}
	if pe.LineHint > 0 {
		details["line_hint"] = strconv.Itoa(pe.LineHint)
	}
	for k, v := range extra {
		details[k] = v
	}
	msg := pe.Msg
	if msg == "" && err != nil {
		msg = err.Error()
	}
	suggestion := suggestionPatchFailed
	if msg == "base directory does not exist" || strings.HasPrefix(msg, "cannot stat base directory:") {
		suggestion = suggestionBaseMissing
	}
	return pathErrorResultDetails(errCodePatchFailed, msg, path, details, suggestion)
}

func rewriteBlockedResult(path, message string, oldLen, newLen int) *mcp.CallToolResult {
	return pathErrorResult(errCodeRewriteBlocked, message, path, map[string]string{
		"path":    path,
		"old_len": strconv.Itoa(oldLen),
		"new_len": strconv.Itoa(newLen),
		"pattern": string(core.PatternAccidentalRewrite),
	}, suggestionRewriteBlocked)
}

func notAllowedResult(engine *core.UltraFastEngine, path string) *mcp.CallToolResult {
	return pathErrorResultDetails(errCodeNotAllowed, "access denied", path, notAllowedDetails(engine, path), suggestionNotAllowed)
}

func notAllowedDetails(engine *core.UltraFastEngine, path string) map[string]any {
	roots := []string{}
	source := ""
	if engine != nil {
		if listed := engine.ListedAllowedPaths(); listed != nil {
			roots = listed
		}
		source = engine.AllowedSource()
	}
	return map[string]any{
		"path":   path,
		"roots":  roots,
		"source": source,
	}
}

func notAllowedDetailsFromPathError(pe *core.PathError) map[string]any {
	roots := pe.Roots
	if roots == nil {
		roots = []string{}
	}
	return map[string]any{
		"path":   pe.Path,
		"roots":  roots,
		"source": pe.Source,
	}
}

func hashRequiredResult(path, currentHash string) *mcp.CallToolResult {
	details := map[string]string{"path": path}
	if currentHash != "" {
		details["current_hash"] = currentHash
	}
	return pathErrorResult(errCodeHashRequired,
		"expected_hash is required after an external file change", path, details, suggestionHashRequired)
}

func validationResult(message, field, expected string) *mcp.CallToolResult {
	details := map[string]string{}
	if field != "" {
		details["field"] = field
	}
	if expected != "" {
		details["expected"] = expected
	}
	return pathErrorResult(errCodeInvalidParams, message, "", details, suggestionValidation)
}

func quotedField(s string) string {
	i := strings.Index(s, `"`)
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func budgetExceededResult(used, limit int) *mcp.CallToolResult {
	return pathErrorResult(errCodeBudgetExceeded, "mutation budget exceeded", "", map[string]string{
		"used":  strconv.Itoa(used),
		"limit": strconv.Itoa(limit),
	}, suggestionBudgetExceeded)
}

func validationFieldExpected(msg string) (field, expected string) {
	switch {
	case strings.Contains(msg, "unknown parameter"):
		return quotedField(msg), "known parameter"
	case strings.Contains(msg, "missing required parameter"):
		return quotedField(msg), "required"
	case strings.HasPrefix(msg, "missing one of "):
		group := strings.TrimPrefix(msg, "missing one of ")
		return group, group
	case strings.Contains(msg, "expected "):
		field = quotedField(msg)
		_, rest, ok := strings.Cut(msg, "expected ")
		if ok {
			expected, _, _ = strings.Cut(rest, ",")
			expected = strings.TrimSpace(expected)
		}
		return field, expected
	case strings.Contains(msg, "invalid value"):
		field = quotedField(msg)
		if i := strings.Index(msg, "(valid: "); i >= 0 {
			expected = strings.TrimSuffix(msg[i+len("(valid: "):], ")")
		}
		return field, expected
	default:
		return quotedField(msg), ""
	}
}
