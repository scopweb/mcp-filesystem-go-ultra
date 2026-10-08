package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

type gitFileChange struct {
	Status string
	Path   string
}

func stringArg(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}

func gitStatusSnapshot(ctx context.Context, repoRoot string) (string, string, error) {
	out, err := execGitCommandCtx(ctx, repoRoot, "git", "status", "--porcelain=v1", "-b")
	if err != nil {
		return "", "", err
	}
	return out, contentHashBytes([]byte(out)), nil
}

func parsePorcelainV1(out string) (branch string, files []gitFileChange) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			branch = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		files = append(files, gitFileChange{Status: line[:2], Path: path})
	}
	return branch, files
}

func pageFiles(files []gitFileChange, offset, limit int) ([]gitFileChange, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(files) {
		offset = len(files)
	}
	end := offset + limit
	if end > len(files) {
		end = len(files)
	}
	return files[offset:end], end < len(files)
}

func staleGitResult(path, got, want string) *mcp.CallToolResult {
	return pathErrorResult(errCodeStaleGit,
		"git status changed since status_hash was issued",
		path,
		map[string]string{"current_status_hash": got, "expected_status_hash": want},
		`call git(action:"review") again; do not continue the old offset`)
}

func checkStatusHash(ctx context.Context, repoRoot string, args map[string]interface{}) (string, string, *mcp.CallToolResult, error) {
	porcelain, hash, err := gitStatusSnapshot(ctx, repoRoot)
	if err != nil {
		return "", "", nil, err
	}
	want := strings.TrimSpace(stringArg(args, "expected_status_hash"))
	if want != "" && want != hash {
		return porcelain, hash, staleGitResult(repoRoot, hash, want), nil
	}
	return porcelain, hash, nil, nil
}

func gitReview(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	offset := parseIntArg(args, "offset", 0)
	if _, ok := args["offset"]; ok && offset < 0 {
		return usageError("offset must be >= 0", `git(action:"review", offset:0)`), nil
	}
	limit := parseIntArg(args, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	porcelain, hash, stale, err := checkStatusHash(ctx, repoRoot, args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git review failed: %v", err)), nil
	}
	if stale != nil {
		return stale, nil
	}
	branch, files := parsePorcelainV1(porcelain)
	page, more := pageFiles(files, offset, limit)
	staged, unstaged, untracked, conflicts := 0, 0, 0, 0
	for _, f := range files {
		switch {
		case f.Status == "??":
			untracked++
		default:
			if strings.Contains(f.Status, "U") {
				conflicts++
			}
			if f.Status[0] != ' ' && f.Status[0] != '?' {
				staged++
			}
			if len(f.Status) > 1 && f.Status[1] != ' ' && f.Status[1] != '?' {
				unstaged++
			}
		}
	}
	items := make([]map[string]any, 0, len(page))
	var b strings.Builder
	fmt.Fprintf(&b, "%s | %s | +%d ~%d ?%d", repoRoot, branch, staged, unstaged, untracked)
	if conflicts > 0 {
		fmt.Fprintf(&b, " conflicts:%d", conflicts)
	}
	fmt.Fprintf(&b, " | status_hash:%s\n", hash)
	for _, f := range page {
		fmt.Fprintf(&b, "%s %s\n", f.Status, f.Path)
		items = append(items, map[string]any{"status": f.Status, "path": f.Path})
	}
	payload := map[string]any{
		"action":      "review",
		"status_hash": hash,
		"branch":      branch,
		"staged":      staged,
		"unstaged":    unstaged,
		"untracked":   untracked,
		"conflicts":   conflicts,
		"files":       items,
		"offset":      offset,
		"file_count":  len(files),
		"truncated":   more,
	}
	if more {
		next := offset + len(page)
		payload["continuation"] = map[string]any{
			"offset":               next,
			"limit":                limit,
			"expected_status_hash": hash,
			"hint":                 "pass offset and expected_status_hash; do not repeat this page",
		}
		fmt.Fprintf(&b, "[page %d-%d of %d; next offset=%d expected_status_hash=%s]\n", offset+1, offset+len(page), len(files), next, hash)
	}
	_ = engine
	return mcp.NewToolResultStructured(payload, b.String()), nil
}

func gitDiffPage(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}, staged bool, rev, out string, maxLines int, paths []string) (*mcp.CallToolResult, error) {
	offset := 0
	if _, ok := args["offset"]; ok {
		offset = parseIntArg(args, "offset", 0)
		if offset < 0 {
			return usageError("offset must be >= 0", `git(action:"diff", offset:0, output:"stat")`), nil
		}
	}
	limit := parseIntArg(args, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	_, hash, stale, err := checkStatusHash(ctx, repoRoot, args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git diff failed: %v", err)), nil
	}
	if stale != nil {
		return stale, nil
	}
	names := paths
	if len(names) == 0 {
		cmdArgs := []string{"diff", "--no-ext-diff", "--name-only"}
		if staged {
			cmdArgs = append(cmdArgs, "--cached")
		}
		if rev != "" {
			cmdArgs = append(cmdArgs, rev)
		}
		listed, lerr := execGitCommandCtx(ctx, repoRoot, "git", cmdArgs...)
		if lerr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git diff failed: %v\n%s", lerr, listed)), nil
		}
		for _, line := range strings.Split(listed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "[TRUNCADO:") {
				continue
			}
			names = append(names, line)
		}
	}
	if offset > len(names) {
		offset = len(names)
	}
	end := offset + limit
	more := end < len(names)
	if end > len(names) {
		end = len(names)
	}
	page := names[offset:end]
	if len(page) == 0 {
		return mcp.NewToolResultStructured(map[string]any{
			"action": "diff", "status_hash": hash, "files": []string{}, "offset": offset, "truncated": false,
		}, "No changes in this page\n"), nil
	}
	cmdArgs := []string{"diff", "--no-ext-diff"}
	if staged {
		cmdArgs = append(cmdArgs, "--cached")
	}
	if rev != "" {
		cmdArgs = append(cmdArgs, rev)
	}
	switch out {
	case "stat":
		cmdArgs = append(cmdArgs, "--stat")
	case "name-only":
		cmdArgs = append(cmdArgs, "--name-only")
	}
	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, page...)
	body, werr := execGitCommandCtx(ctx, repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git diff failed: %v\n%s", werr, body)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "status_hash:%s | files %d-%d of %d\n", hash, offset+1, end, len(names))
	b.WriteString(truncateOutput(body, maxLines))
	if more {
		fmt.Fprintf(&b, "\n[next offset=%d expected_status_hash=%s]\n", end, hash)
	}
	payload := map[string]any{
		"action": "diff", "status_hash": hash, "files": page, "offset": offset,
		"file_count": len(names), "truncated": more,
	}
	if more {
		payload["continuation"] = map[string]any{
			"offset": end, "limit": limit, "expected_status_hash": hash,
			"hint": "pass offset and expected_status_hash with the same output and rev",
		}
	}
	text := b.String()
	if engine.IsCompactMode() && len(text) > 10000 {
		text = text[:10000] + "\n... (truncated by compact mode)"
	}
	return mcp.NewToolResultStructured(payload, text), nil
}
