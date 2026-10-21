package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func registerContextTools(reg *toolRegistry) {
	engine := reg.engine
	tool := mcp.NewTool("context_pack",
		mcp.WithTitleAnnotation("Context Pack"),
		mcp.WithDescription("context_pack — Deterministic task context: ranked files, definition-centred ranges, bounded snippets, and related git status. "+
			"Not a semantic index. budget_chars bounds the characters carried by the text fallback and the structured payload together; "+
			"fragments degrade to signature, then locator, and every omission is counted in coverage. "+
			"Pass focus_paths or query; then read_file only the ranges you need. "+
			"Ultra only. Related: search_files, analyze_code, git, read_file."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path", mcp.Required(), mcp.Description("Repository or directory root inside allowed paths")),
		mcp.WithString("query", mcp.Description("Literal/regex content query used to rank files. Omit when focus_paths is enough.")),
		mcp.WithArray("focus_paths", mcp.WithStringItems(), mcp.Description("Files to rank first. Relative to path or absolute.")),
		mcp.WithNumber("budget_chars", mcp.Description("Total characters the pack may carry across the text fallback and the structured payload. Default 12000. Min 500. Max 32000.")),
		mcp.WithNumber("max_files", mcp.Description("Max files in the pack. Default 8. Max 20.")),
		mcp.WithBoolean("include_git", mcp.Description("Include a compact git review when path is a repository. Default true. Skipped while a file security policy is active.")),
	)
	reg.addTool(tool, auditWrap(engine, "context_pack", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := request.Params.Arguments.(map[string]interface{})
		if args == nil {
			args = map[string]interface{}{}
		}
		return contextPack(ctx, engine, args)
	}),
		`context_pack(path:"C:/repo", query:"expected_hash", focus_paths:["core/feedback.go"], budget_chars:8000)`,
	)
}

func contextPack(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if strings.TrimSpace(path) == "" {
		return usageError("missing 'path'", `context_pack(path:"C:/repo", query:"symbol")`), nil
	}
	path = core.NormalizePath(path)
	if !engine.IsPathAllowed(path) {
		return notAllowedResult(engine, path), nil
	}
	query := strings.TrimSpace(stringArg(args, "query"))
	budget := parseIntArg(args, "budget_chars", 12000)
	if budget < 500 {
		budget = 500
	}
	if budget > 32000 {
		budget = 32000
	}
	maxFiles := parseIntArg(args, "max_files", 8)
	if maxFiles < 1 {
		maxFiles = 1
	}
	if maxFiles > 20 {
		maxFiles = 20
	}
	includeGit := true
	if v, ok := args["include_git"].(bool); ok {
		includeGit = v
	}
	focus, errRes := stringListArg(args, "focus_paths")
	if errRes != nil {
		return errRes, nil
	}

	disc, err := packDiscover(ctx, engine, path, query, focus)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("context_pack search failed: %v", err)), nil
	}

	// Open the candidates that can still make the cut and derive their
	// fragments from one snapshot each.
	ident := packQueryIdentifier(query)
	examined := 0
	for _, c := range disc.Candidates {
		if examined >= packCandidateScanLimit {
			break
		}
		examined++
		packBuildCandidate(ctx, engine, c, ident)
	}
	// The class of a candidate is only known once its fragments are, so the
	// order is settled here and not before.
	sort.SliceStable(disc.Candidates, func(i, j int) bool {
		return packCandidateLess(disc.Candidates[i], disc.Candidates[j])
	})

	cov := packCoverage{
		Scope:                 path,
		Query:                 query,
		QueryMatched:          disc.QueryHadMatches,
		CandidateFiles:        len(disc.Candidates),
		SearchTruncated:       disc.SearchTruncated,
		SearchMatches:         disc.SearchMatches,
		FilesDenied:           disc.Denied,
		CandidatesNotExamined: len(disc.Candidates) - examined,
		SourceCandidates:      disc.SourceCandidates,
	}

	selected := disc.Candidates
	if len(selected) > maxFiles {
		cov.FilesOmittedMaxFiles = len(selected) - maxFiles
		selected = selected[:maxFiles]
	}
	for _, c := range selected {
		if c.Unreadable {
			cov.FilesUnreadable++
			continue
		}
		if c.Stale {
			cov.RangesStale++
		}
		cov.RangesMerged += c.Merged
	}

	gitText, gitPayload, gitNote := packGit(ctx, engine, path, includeGit)
	text, payload, cov := packRender(selected, cov, gitText, gitPayload, gitNote, budget)

	if engine.StructuredResponses() {
		text = packSummary(cov, payload)
		payload["presentation"] = core.ResponseModeStructured
	}
	return mcp.NewToolResultStructured(payload, text), nil
}

// packSummary is the brief text used under --response-mode=structured. The
// context itself stays in structuredContent, so the body appears once.
func packSummary(cov packCoverage, payload map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "context_pack %s", cov.Scope)
	if cov.Query != "" {
		fmt.Fprintf(&b, " query=%q", cov.Query)
	}
	files, _ := payload["files"].([]map[string]any)
	fmt.Fprintf(&b, " files:%d/%d budget_chars:%v", len(files), cov.CandidateFiles, payload["budget_chars"])
	if truncated, _ := payload["truncated"].(bool); truncated {
		b.WriteString(" truncated")
	}
	b.WriteString(" | context in structured content")
	return b.String()
}

func packGit(ctx context.Context, engine *core.UltraFastEngine, path string, include bool) (string, map[string]any, string) {
	if !include {
		return "", nil, "disabled"
	}
	if engine.PolicyEnabled() {
		return "", nil, "policy"
	}
	root, err := core.FindGitRoot(path)
	if err != nil || !engine.IsPathAllowed(root) {
		return "", nil, "not_a_repo"
	}
	res, err := gitReview(ctx, engine, root, map[string]interface{}{"limit": float64(12)})
	if err != nil || res == nil || res.IsError {
		return "", nil, "git_failed"
	}
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			text = tc.Text
		}
	}
	payload, _ := res.StructuredContent.(map[string]any)
	return text, payload, ""
}

func stringListArg(args map[string]interface{}, key string) ([]string, *mcp.CallToolResult) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...), nil
	case []interface{}:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, usageError(fmt.Sprintf("%s[%d] must be a string", key, i), `focus_paths:["a.go"]`)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, usageError(fmt.Sprintf("%s must be an array of strings", key), `focus_paths:["a.go"]`)
	}
}
