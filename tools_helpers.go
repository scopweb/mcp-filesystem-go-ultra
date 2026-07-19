package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// usageError formats an error response with a usage example, per docs/git-tool-spec.md §4.
//
// Always returns (result, nil) — never a non-nil error — so the caller goes through the
// normal mcp.NewToolResultError path and is logged with status "error" by auditWrap.
func usageError(msg, example string) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("ERROR: %s\nusage: %s", msg, example))
}

// filesystemMismatchHint is appended to read/edit/info responses when the host
// reports a missing path. The recovery guidance is the same for every
// missing-path error so consumers can match on the prefix.
const filesystemMismatchHint = "FILESYSTEM MISMATCH? This path is missing from the filesystem visible to filesystem-ultra. If another tool previously read or wrote this known path, stop and verify it with filesystem-ultra:read_file/get_file_info, audit recent mutations made through the failing tool family, and understand the mismatch before retrying or switching tools."

// filesystemMismatchSuffix adds the recovery guidance only for genuine
// missing-path errors. PathError and fmt.Errorf("...: %w") preserve
// fs.ErrNotExist so callers do not need to inspect brittle error strings.
func filesystemMismatchSuffix(err error) string {
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	return "\n→ " + filesystemMismatchHint
}

// formatToolError preserves the existing "Error:" prefix while making a known
// missing path actionable. Non-not-found errors remain byte-identical.
func formatToolError(err error) string {
	return fmt.Sprintf("Error: %v", err) + filesystemMismatchSuffix(err)
}

// pathsFromArgs extracts the pathspec from args["paths"] as []string.
//
// Returns:
//   - (nil, nil)             — key absent or empty; caller proceeds without pathspec
//   - (nil, usageError)      — value is a string (old API) or wrong shape; caller returns the error
//   - ([]string, nil)        — native []interface{} of strings, normalized via core.NormalizePath
//
// The string-JSON fallback intentionally does NOT exist: the spec mandates native arrays and
// agents re-read the schema each session. See D2 in the upgrade plan.
func pathsFromArgs(args map[string]interface{}) ([]string, *mcp.CallToolResult) {
	raw, present := args["paths"]
	if !present || raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, usageError(
					fmt.Sprintf("'paths[%d]' must be a string, got %T", i, item),
					`git(action:"diff", paths:["src/file.php"])`)
			}
			out = append(out, s)
		}
		return out, nil
	case []string:
		return append([]string(nil), v...), nil
	case string:
		// Old API: caller passed a JSON string. Refuse explicitly so the agent self-corrects.
		return nil, usageError(
			"'paths' must be a native array, not a JSON string. Pass paths:[\"a.go\",\"b.go\"] directly.",
			`git(action:"diff", paths:["src/file.php", "lib/x.php"])`)
	default:
		return nil, usageError(
			fmt.Sprintf("'paths' must be an array of strings, got %T", raw),
			`git(action:"diff", paths:["src/file.php"])`)
	}
}

func implicitGitPathspec(path, repoRoot string) (string, bool) {
	if path == "" || repoRoot == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// truncateOutput truncates text to maxLines and appends a footer if truncated.
// Footer per docs/git-tool-spec.md §3:
//
//	[TRUNCADO: N líneas totales, mostradas M]
//	→ Acota con paths:["archivo"] o usa output:"stat" para el resumen.
func truncateOutput(text string, maxLines int) string {
	if maxLines <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	total := len(lines)
	kept := strings.Join(lines[:maxLines], "\n")
	return kept + fmt.Sprintf(
		"\n[TRUNCADO: %d líneas totales, mostradas %d]\n→ Acota con paths:[\"archivo\"] o usa output:\"stat\" para el resumen.",
		total, maxLines)
}

// parseIntArg extracts an integer arg with default fallback. JSON numbers arrive as float64.
func parseIntArg(args map[string]interface{}, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) > 0 {
				return int(n)
			}
		case int:
			if n > 0 {
				return n
			}
		}
	}
	return def
}

// parseOutputArg extracts an output enum arg with default fallback and value validation.
// Returns (value, nil) on success, (value, usageError) on invalid enum value.
func parseOutputArg(args map[string]interface{}, key, def string, valid []string) (string, *mcp.CallToolResult) {
	v, _ := args[key].(string)
	if v == "" {
		return def, nil
	}
	for _, ok := range valid {
		if v == ok {
			return v, nil
		}
	}
	return v, usageError(
		fmt.Sprintf("invalid %s %q. Valid: %s", key, v, strings.Join(valid, ", ")),
		fmt.Sprintf(`git(action:"diff", output:"%s", paths:["file.php"])`, valid[0]))
}

// countChangedFiles runs `git diff --name-only` and returns the count of changed files.
// Used by the Layer-2 guardrail in gitDiff: when output=="full" is requested without paths,
// we count first to decide whether to downgrade to stat.
func countChangedFiles(repoRoot, rev string, staged bool) (int, error) {
	cmdArgs := []string{"diff", "--no-ext-diff", "--name-only"}
	if staged {
		cmdArgs = append(cmdArgs, "--cached")
	}
	if rev != "" {
		cmdArgs = append(cmdArgs, rev)
	}
	output, err := execGitCommand(repoRoot, "git", cmdArgs...)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(output) == "" {
		return 0, nil
	}
	return len(strings.Split(strings.TrimRight(output, "\n"), "\n")), nil
}
