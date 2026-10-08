package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func registerContextTools(reg *toolRegistry) {
	engine := reg.engine
	tool := mcp.NewTool("context_pack",
		mcp.WithTitleAnnotation("Context Pack"),
		mcp.WithDescription("context_pack — Deterministic task context: ranked files, signatures, short snippets, and related git status. "+
			"Not a semantic index. Omissions and status_hash are explicit. Pass focus_paths or query; then read_file only the ranges you need. "+
			"Ultra only. Related: search_files, analyze_code, git, read_file."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path", mcp.Required(), mcp.Description("Repository or directory root inside allowed paths")),
		mcp.WithString("query", mcp.Description("Literal/regex content query used to rank files. Omit when focus_paths is enough.")),
		mcp.WithArray("focus_paths", mcp.WithStringItems(), mcp.Description("Files to rank first. Relative to path or absolute.")),
		mcp.WithNumber("budget_chars", mcp.Description("Max text characters. Default 12000. Max 32000.")),
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

type packFile struct {
	Path    string
	Score   int
	Line    int
	Snippet string
	Symbols []map[string]any
}

func contextPack(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if strings.TrimSpace(path) == "" {
		return usageError("missing 'path'", `context_pack(path:"C:/repo", query:"symbol")`), nil
	}
	path = core.NormalizePath(path)
	if !engine.IsPathAllowed(path) {
		return mcp.NewToolResultError("access denied: path outside allowed directories" + engine.AllowedDirsSuffix()), nil
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

	ranked := map[string]*packFile{}
	var omitted int
	add := func(p string, score, line int, snippet string) {
		p = core.NormalizePath(p)
		if p == "" || !engine.IsPathAllowed(p) {
			omitted++
			return
		}
		if core.IsSecretPath(p) && !engine.AllowSecrets() {
			omitted++
			return
		}
		if err := engine.Authorize(core.OpRead, p); err != nil {
			omitted++
			return
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			omitted++
			return
		}
		cur, ok := ranked[p]
		if !ok {
			ranked[p] = &packFile{Path: p, Score: score, Line: line, Snippet: snippet}
			return
		}
		if score > cur.Score {
			cur.Score = score
			if line > 0 {
				cur.Line = line
			}
			if snippet != "" {
				cur.Snippet = snippet
			}
		}
	}
	for _, f := range focus {
		fp := f
		if !filepath.IsAbs(fp) {
			fp = filepath.Join(path, fp)
		}
		add(fp, 1000, 0, "")
	}
	if query != "" {
		out, err := engine.SearchFiles(ctx, core.SearchOptions{
			Path: path, Pattern: query, IncludeContent: true, MaxResults: 40,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("context_pack search failed: %v", err)), nil
		}
		counts := map[string]int{}
		for _, m := range out.Matches {
			counts[m.File]++
			if counts[m.File] == 1 {
				add(m.File, 100+counts[m.File], m.LineNumber, trimPackLine(m.Line))
			}
		}
		for file, n := range counts {
			if cur, ok := ranked[core.NormalizePath(file)]; ok && cur.Score < 1000 {
				cur.Score += n
			}
		}
	}
	files := make([]*packFile, 0, len(ranked))
	for _, f := range ranked {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Score == files[j].Score {
			return files[i].Path < files[j].Path
		}
		return files[i].Score > files[j].Score
	})
	dropped := 0
	if len(files) > maxFiles {
		dropped = len(files) - maxFiles
		files = files[:maxFiles]
	}
	entries := make([]map[string]any, 0, len(files))
	for _, f := range files {
		f.Symbols = signatureLines(f.Path, 6)
		if f.Snippet == "" && f.Line == 0 && len(f.Symbols) > 0 {
			if line, ok := f.Symbols[0]["line"].(int); ok {
				f.Line = line
			}
		}
		if f.Snippet == "" && f.Line > 0 {
			f.Snippet = lineAt(f.Path, f.Line)
		}
		entries = append(entries, map[string]any{
			"path": f.Path, "score": f.Score, "line": f.Line, "snippet": f.Snippet, "symbols": f.Symbols,
		})
	}
	gitText, gitPayload, gitNote := packGit(ctx, engine, path, includeGit)
	text := renderPack(path, query, entries, omitted+dropped, gitText, gitNote)
	truncated := false
	if len(text) > budget {
		text = text[:budget] + "\n[TRUNCADO: budget_chars]\n"
		truncated = true
	}
	payload := map[string]any{
		"action": "context_pack", "path": path, "files": entries,
		"omitted": omitted + dropped, "truncated": truncated, "budget_chars": budget,
	}
	if query != "" {
		payload["query"] = query
	}
	if gitPayload != nil {
		payload["git"] = gitPayload
	}
	if gitNote != "" {
		payload["git_omitted"] = gitNote
	}
	return mcp.NewToolResultStructured(payload, text), nil
}

func renderPack(path, query string, files []map[string]any, omitted int, gitText, gitNote string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "context_pack %s", path)
	if query != "" {
		fmt.Fprintf(&b, " query=%q", query)
	}
	fmt.Fprintf(&b, " files:%d omitted:%d\n", len(files), omitted)
	if gitNote != "" {
		fmt.Fprintf(&b, "git: %s\n", gitNote)
	}
	if gitText != "" {
		b.WriteString(gitText)
		if !strings.HasSuffix(gitText, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, f := range files {
		p, _ := f["path"].(string)
		line, _ := f["line"].(int)
		fmt.Fprintf(&b, "\n%s", p)
		if line > 0 {
			fmt.Fprintf(&b, ":%d", line)
		}
		b.WriteByte('\n')
		if syms, ok := f["symbols"].([]map[string]any); ok {
			for _, s := range syms {
				fmt.Fprintf(&b, "  %s %v:%v\n", s["kind"], s["name"], s["line"])
			}
		}
		if snip, _ := f["snippet"].(string); snip != "" {
			fmt.Fprintf(&b, "  > %s\n", snip)
		}
	}
	if len(files) == 0 {
		b.WriteString("no files selected; pass query or focus_paths\n")
	}
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

func signatureLines(path string, max int) []map[string]any {
	res := core.AnalyzeSymbols(path, "", max)
	out := make([]map[string]any, 0, len(res.Findings))
	for _, f := range res.Findings {
		if len(out) >= max {
			break
		}
		out = append(out, map[string]any{
			"name": f.Symbol, "kind": f.Kind, "line": f.Line, "signature": lineAt(path, f.Line),
		})
	}
	return out
}

func lineAt(path string, line int) string {
	if line < 1 {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if line > len(lines) {
		return ""
	}
	return trimPackLine(lines[line-1])
}

func trimPackLine(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 180 {
		return s[:180] + "…"
	}
	return s
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
