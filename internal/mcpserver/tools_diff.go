package mcpserver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func parseNormalizeRules(args map[string]any) ([]core.NormalizeRule, error) {
	if args == nil {
		return nil, nil
	}
	_, hasNative := args["normalize"]
	js, _ := args["normalize_json"].(string)
	hasJSON := strings.TrimSpace(js) != ""
	if hasNative && args["normalize"] != nil && hasJSON {
		return nil, fmt.Errorf("pass normalize or normalize_json, not both")
	}
	if hasJSON {
		var rules []core.NormalizeRule
		if err := json.Unmarshal([]byte(js), &rules); err != nil {
			return nil, fmt.Errorf("normalize_json: %w", err)
		}
		return rules, nil
	}
	raw, ok := args["normalize"]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("normalize must be an array of {pattern, replacement}")
	}
	rules := make([]core.NormalizeRule, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("normalize rule %d must be an object", i+1)
		}
		pattern, _ := m["pattern"].(string)
		replacement, _ := m["replacement"].(string)
		rules = append(rules, core.NormalizeRule{Pattern: pattern, Replacement: replacement})
	}
	return rules, nil
}

func diffResult(pathA, pathB, nameB, against string, a, b string, rules []core.NormalizeRule, offset, byteOffset int) (*mcp.CallToolResult, error) {
	cmp, err := core.CompareTexts(a, b, rules)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	label := nameB
	if label == "" {
		label = pathB
	}
	if cmp.Identical {
		msg := fmt.Sprintf("identical: %s %s", pathA, pathB)
		payload := map[string]any{
			"status": "identical", "path_a": pathA, "path_b": pathB, "identical": true,
			"added": 0, "removed": 0, "message": msg,
		}
		if against != "" {
			payload["against"] = against
		}
		if len(rules) > 0 {
			payload["comparison_basis"] = "literal"
			payload["rules_applied"] = len(rules)
		}
		return mcp.NewToolResultStructured(payload, msg), nil
	}
	if cmp.Equivalent {
		msg := fmt.Sprintf("equivalent after normalization: %s %s | rules=%d matches_a=%d matches_b=%d", pathA, pathB, cmp.RulesApplied, cmp.MatchesA, cmp.MatchesB)
		payload := map[string]any{
			"status": "equivalent", "path_a": pathA, "path_b": pathB, "identical": false,
			"equivalent": true, "comparison_basis": cmp.Basis, "rules_applied": cmp.RulesApplied,
			"matches_a": cmp.MatchesA, "matches_b": cmp.MatchesB,
			"added": 0, "removed": 0, "message": msg,
		}
		if against != "" {
			payload["against"] = against
		}
		return mcp.NewToolResultStructured(payload, msg), nil
	}
	diff := cmp.Diff
	if nameB != "" && nameB != "b" {
		diff = strings.Replace(diff, "+++ b/b", "+++ b/"+nameB, 1)
		diff = strings.Replace(diff, "--- a/b", "--- a/"+nameB, 1)
	}
	msg := fmt.Sprintf("%s\n+%d -%d", strings.TrimSpace(diff), cmp.Added, cmp.Removed)
	if cmp.NormalizedDiff {
		msg = "normalized diff (not an applicable patch)\n" + msg
	}
	payload := map[string]any{
		"status": statusOK, "path_a": pathA, "path_b": pathB, "identical": false,
		"added": cmp.Added, "removed": cmp.Removed, "message": msg,
	}
	if against != "" {
		payload["against"] = against
	}
	if len(rules) > 0 {
		payload["equivalent"] = false
		payload["comparison_basis"] = cmp.Basis
		payload["rules_applied"] = cmp.RulesApplied
		payload["matches_a"] = cmp.MatchesA
		payload["matches_b"] = cmp.MatchesB
		payload["normalized_diff"] = cmp.NormalizedDiff
	}
	bounded, cont, boundErr := boundDiffText(msg, offset, byteOffset)
	if boundErr != nil {
		return mcp.NewToolResultError(boundErr.Error()), nil
	}
	payload["message"] = bounded
	if cont != nil {
		payload["truncated"] = true
		payload["continuation"] = cont
		payload["status"] = statusTruncated
	}
	return mcp.NewToolResultStructured(payload, bounded), nil
}

func boundDiffText(msg string, offset, byteOffset int) (string, map[string]any, error) {
	lines := splitContentLines(msg)
	if len(lines) == 0 {
		return msg, nil, nil
	}
	if offset < 1 {
		offset = 1
	}
	if offset > len(lines) {
		return "", map[string]any{"offset": offset, "hint": "offset is past the diff"}, fmt.Errorf("diff offset %d is past the diff (%d lines)", offset, len(lines))
	}
	if byteOffset > 0 {
		if msg := trimLineByteOffset(lines, offset, byteOffset); msg != "" {
			return "", nil, fmt.Errorf("%s", msg)
		}
	}
	body := strings.Join(lines[offset-1:], "\n")
	if len(body) <= claudeSpillChars {
		return body, nil, nil
	}
	proj := clampFormattedSpillLimit(readProjection{
		Text: body, StartLine: offset, EndLine: len(lines), TotalLines: len(lines), LineOffset: byteOffset,
	}, "diff", claudeSpillChars)
	cont := map[string]any{
		"offset": proj.ContinueAt,
		"hint":   "Continue with diff_files offset (1-based line). If byte_offset is set, pass it too; do not skip that line.",
	}
	if proj.ResumeOffset > 0 {
		cont["byte_offset"] = proj.ResumeOffset
	}
	return proj.Text, cont, nil
}
