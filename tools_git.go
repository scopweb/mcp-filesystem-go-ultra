package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// registerGitTools registers the git tool
func registerGitTools(reg *toolRegistry) {
	engine := reg.engine

	gitTool := mcp.NewTool("git",
		mcp.WithTitleAnnotation("Git Version Control"),
		mcp.WithDescription("git — Git operations: status, diff, log, show, add, commit, restore, branch, init. "+
			"Must be run from within a git repository. Related: analyze_operation, edit_file, help."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true), // restore, branch delete
		mcp.WithIdempotentHintAnnotation(false), // commit, restore, branch delete are not idempotent

		mcp.WithString("action", mcp.Required(), mcp.Description("Action: status, diff, log, show, add, commit, restore, branch, init")),
		mcp.WithString("path", mcp.Description("Working directory or file path (default: auto-detect repo root). If a file path, used as implicit pathspec for diff/log/status.")),
		mcp.WithArray("paths", mcp.WithStringItems(),
			mcp.Description("Pathspec: native array of file/dir paths relative to repo root. Limits diff/log/status/add/restore to these paths. Equivalent to 'git <cmd> -- <paths>'.")),
		mcp.WithString("output", mcp.Description("Output format. diff/show: 'stat' (default) | 'name-only' | 'full'. status: 'name-only' (default) | 'full'. log: 'oneline' (default) | 'full'.")),
		mcp.WithNumber("max_lines", mcp.Description("Max output lines before truncation with hint footer (default: 200)")),
		mcp.WithNumber("limit", mcp.Description("log: max commits to return (default: 10)")),
		mcp.WithString("rev", mcp.Description("Revision or range (e.g. 'HEAD~3', 'abc123..def456', 'main'). diff/log/show.")),
		mcp.WithBoolean("staged", mcp.Description("diff: compare index vs HEAD (--cached). Default: false.")),
		mcp.WithString("message", mcp.Description("commit: message text (required for commit)")),
		mcp.WithString("name", mcp.Description("branch: branch name to create/delete/checkout")),
		mcp.WithBoolean("checkout", mcp.Description("branch: with name=true, also switch to new branch (git switch -c). Default: false.")),
		mcp.WithBoolean("force", mcp.Description("branch delete: true → -D (force). Other actions: ignored.")),
	)

	reg.addTool(gitTool, auditWrap(engine, "git", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})

		action, _ := args["action"].(string)
		path, _ := args["path"].(string)

		if action == "" {
			return usageError(
				"missing 'action' parameter",
				`git(action:"status")  // or: diff, log, show, add, commit, restore, branch, init`), nil
		}

		// Normalize path
		if path != "" {
			path = core.NormalizePath(path)
		}

		// Dispatch without repo check for init action
		if action == "init" {
			return gitInit(ctx, engine, path, args)
		}

		// For all other actions, detect git repo
		repoRoot, err := core.FindGitRoot(path)
		if err != nil {
			return usageError(
				fmt.Sprintf("path is not inside a git repository: %s", path),
				`git(action:"init", path:"C:/path/to/repo")  // to create one`), nil
		}

		// Check access control
		if !engine.IsPathAllowed(repoRoot) {
			return mcp.NewToolResultError("access denied: path outside allowed directories"+engine.AllowedDirsSuffix()), nil
		}

		if _, hasPaths := args["paths"]; !hasPaths {
			switch action {
			case "status", "diff", "log":
				if pathspec, ok := implicitGitPathspec(path, repoRoot); ok {
					args["paths"] = []interface{}{pathspec}
				}
			}
		}

		// Option-injection guard runs BEFORE the destructive gate so an
		// option-like value (leading '-') is rejected coherently regardless of
		// force/gate state, rather than depending on each handler checking it
		// after the gate. Applies to every user-supplied value that reaches git
		// as a positional argument.
		for _, f := range []string{"rev", "name"} {
			if v, _ := args[f].(string); v != "" {
				if errRes := rejectOptionLike(f, v); errRes != nil {
					return errRes, nil
				}
			}
		}

		// Anti-destructive protection for dangerous git operations
		if isDestructiveGitAction(action, args) && !getBoolArg(args, "force") {
			return usageError(
				fmt.Sprintf("destructive git operation '%s' requires force:true for safety", action),
				fmt.Sprintf(`git(action:"%s", paths:["file.txt"], force:true)`, action)), nil
		}

		switch action {
		case "status":
			return gitStatus(ctx, engine, repoRoot, args)
		case "diff":
			return gitDiff(ctx, engine, repoRoot, args)
		case "log":
			return gitLog(ctx, engine, repoRoot, args)
		case "show":
			return gitShow(ctx, engine, repoRoot, args)
		case "add":
			return gitAdd(ctx, engine, repoRoot, args)
		case "commit":
			return gitCommit(ctx, engine, repoRoot, args)
		case "restore":
			return gitRestore(ctx, engine, repoRoot, args)
		case "branch":
			return gitBranch(ctx, engine, repoRoot, args)
		default:
			return usageError(
				fmt.Sprintf("unknown action %q", action),
				`git(action:"status")  // valid: status, diff, log, show, add, commit, restore, branch, init`), nil
		}
	}),
		// Examples for help(tool:"git") — manually curated per docs/git-tool-spec.md §5
		`git(action:"status")`,
		`git(action:"diff", paths:["lib/dPeticiones.php"], output:"stat")`,
		`git(action:"log", limit:5, paths:["public_html/ajax/"])`,
		`git(action:"show", rev:"HEAD", output:"full")`,
		`git(action:"add", paths:["src/file.php"])`,
		`git(action:"commit", message:"fix: short description")`,
		`git(action:"restore", paths:["file.txt"], staged:true)`,
		`git(action:"branch", name:"feature/new", checkout:true)`,
	)
}

// gitInit initializes a new git repository
func gitInit(ctx context.Context, engine *core.UltraFastEngine, path string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	targetPath := path
	if targetPath == "" {
		targetPath = "."
	}
	targetPath = core.NormalizePath(targetPath)

	if !engine.IsPathAllowed(targetPath) {
		return mcp.NewToolResultError("access denied: path outside allowed directories"+engine.AllowedDirsSuffix()), nil
	}

	// githooks for init - use pre-create hook context
	hookCtx := &core.HookContext{
		Event:     core.HookPreCreate,
		ToolName:  "git",
		FilePath:  targetPath,
		Operation: "init",
		Metadata:  map[string]interface{}{"git_operation": "init"},
	}
	if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreCreate, hookCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git init denied by hook: %v", err)), nil
	}

	output, err := execGitCommand(targetPath, "git", "init")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git init failed: %v\n%s", err, output)), nil
	}

	hookCtx.Event = core.HookPostCreate
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostCreate, hookCtx)

	if engine.IsCompactMode() {
		return mcp.NewToolResultText("OK: repository initialized"), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Initialized empty Git repository in %s\n%s", targetPath, output)), nil
}

// gitStatus returns the current git status.
//
// output:
//   - "name-only" (default) → porcelain v1 with branch line (`-b`)
//   - "full"                → porcelain v1 with branch line AND branch tracking info (porcelain=v2 -b)
//
// paths: optional native array; appended after `--` to limit the scope.
func gitStatus(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	output, _ := parseOutputArg(args, "output", "name-only", []string{"name-only", "full"})
	// parseOutputArg never errors with a valid default, but defensive:
	if output == "" {
		output = "name-only"
	}

	paths, errRes := pathsFromArgs(args)
	if errRes != nil {
		return errRes, nil
	}

	cmdArgs := []string{"status", "--porcelain=v1", "-b"}
	if output == "full" {
		cmdArgs = []string{"status", "--porcelain=v2", "-b"}
	}
	if len(paths) > 0 {
		cmdArgs = append(cmdArgs, "--")
		for _, p := range paths {
			cmdArgs = append(cmdArgs, core.NormalizePath(p))
		}
	}

	gitOutput, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git status failed: %v\n%s", werr, gitOutput)), nil
	}

	if engine.IsCompactMode() {
		return gitStatusCompact(repoRoot, gitOutput)
	}
	return mcp.NewToolResultText(gitOutput), nil
}

// gitStatusCompact returns a compact one-line status summary
func gitStatusCompact(repoRoot, output string) (*mcp.CallToolResult, error) {
	lines := strings.Split(output, "\n")

	currentBranch, upstream := "", ""
	stagedCount, unstagedCount, untrackedCount := 0, 0, 0

	for _, line := range lines {
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "## "):
			branchState := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			switch {
			case strings.HasPrefix(branchState, "No commits yet on "):
				currentBranch = strings.TrimPrefix(branchState, "No commits yet on ")
			case strings.HasPrefix(branchState, "Initial commit on "):
				currentBranch = strings.TrimPrefix(branchState, "Initial commit on ")
			default:
				fields := strings.Fields(branchState)
				if len(fields) > 0 {
					currentBranch = fields[0]
				}
			}
			continue
		case strings.HasPrefix(line, "# branch.head "):
			currentBranch = strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
			continue
		case strings.HasPrefix(line, "# branch.upstream "):
			upstream = strings.TrimSpace(strings.TrimPrefix(line, "# branch.upstream "))
			continue
		case strings.HasPrefix(line, "# "):
			continue
		case strings.HasPrefix(line, "? "):
			untrackedCount++
			continue
		case strings.HasPrefix(line, "! "):
			continue
		case len(line) >= 4 && line[1] == ' ' && (line[0] == '1' || line[0] == '2' || line[0] == 'u'):
			fields := strings.Fields(line)
			if len(fields) > 1 && len(fields[1]) >= 2 {
				if fields[1][0] != '.' {
					stagedCount++
				}
				if fields[1][1] != '.' {
					unstagedCount++
				}
			}
			continue
		}

		if len(line) < 2 {
			continue
		}
		staged, unstaged := line[0], line[1]
		if staged == '?' && unstaged == '?' {
			untrackedCount++
			continue
		}
		if staged != ' ' {
			stagedCount++
		}
		if unstaged != ' ' {
			unstagedCount++
		}
	}

	if currentBranch != "" && upstream != "" {
		currentBranch += "..." + upstream
	}

	repoName := repoRoot
	if idx := strings.LastIndex(repoRoot, "/"); idx != -1 {
		repoName = repoRoot[idx+1:]
	}
	if runtime.GOOS == "windows" {
		if idx := strings.LastIndex(repoRoot, "\\"); idx != -1 {
			repoName = repoRoot[idx+1:]
		}
	}

	status := "clean"
	if stagedCount+unstagedCount+untrackedCount > 0 {
		status = "dirty"
	}

	return mcp.NewToolResultText(fmt.Sprintf("%s (%s) | +%d ~%d ?%d | %s",
		repoName, currentBranch, stagedCount, unstagedCount, untrackedCount, status)), nil
}

// diffGuardrailThreshold is the file-count above which output:"full" without
// paths is downgraded to stat with a banner. See upgrade plan §B guardrail L2.
const diffGuardrailThreshold = 20

// gitDiff returns the diff output with the 4-layer guardrail from docs/git-tool-spec.md §3.
//
// Layer 1 — default output = "stat".
// Layer 2 — output=="full" && len(paths)==0 && changedFiles > diffGuardrailThreshold
//
//	→ downgrade to stat, prepend banner (no error, just degrade).
//
// Layer 3 — output=="full" with explicit paths → honor the request.
// Layer 4 — max_lines (default 200) applies to ALL output, with footer if truncated.
func gitDiff(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	staged, _ := args["staged"].(bool)
	rev, _ := args["rev"].(string)
	if rev != "" {
		if errRes := rejectOptionLike("rev", rev); errRes != nil {
			return errRes, nil
		}
	}

	out, errRes := parseOutputArg(args, "output", "stat", []string{"stat", "name-only", "full"})
	if errRes != nil {
		return errRes, nil
	}

	maxLines := parseIntArg(args, "max_lines", 200)
	paths, errRes := pathsFromArgs(args)
	if errRes != nil {
		return errRes, nil
	}

	// ---- Layer 2 guardrail: count first, downgrade if too many files ----
	banner := ""
	if out == "full" && len(paths) == 0 {
		count, cerr := countChangedFiles(repoRoot, rev, staged)
		if cerr == nil && count > diffGuardrailThreshold {
			out = "stat"
			banner = fmt.Sprintf(
				"[GUARDARRAIL: %d archivos modificados sin 'paths' — mostrando stat. Pide output:\"full\" con paths:[\"archivo\"] para diff concreto.]\n",
				count)
		}
	}

	// ---- Build git command ----
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
		// "full" → no extra flag, plain patch
	}
	if len(paths) > 0 {
		cmdArgs = append(cmdArgs, "--")
		for _, p := range paths {
			cmdArgs = append(cmdArgs, core.NormalizePath(p))
		}
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git diff failed: %v\n%s", werr, output)), nil
	}

	if strings.TrimSpace(output) == "" {
		// Friendly "no changes" message based on what was asked
		switch {
		case rev != "":
			return mcp.NewToolResultText("No changes in range " + rev), nil
		case staged:
			return mcp.NewToolResultText("No staged changes"), nil
		default:
			return mcp.NewToolResultText("No unstaged changes"), nil
		}
	}

	// Compact mode of the SERVER caps differently, but the spec's max_lines always wins
	// for LLM consumers. We apply max_lines unconditionally and prepend the banner if downgraded.
	final := banner + truncateOutput(output, maxLines)

	// Server compact mode still trims further if it's an extreme case (>10k chars).
	if engine.IsCompactMode() && len(final) > 10000 {
		final = final[:10000] + "\n... (truncated by compact mode)"
	}

	return mcp.NewToolResultText(final), nil
}

// gitLog returns the commit log.
//
// output: "oneline" (default) | "full"
// limit:  default 10
// rev:    optional single rev or range
// paths:  optional pathspec array
func gitLog(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	rev, _ := args["rev"].(string)
	if rev != "" {
		if errRes := rejectOptionLike("rev", rev); errRes != nil {
			return errRes, nil
		}
	}

	out, errRes := parseOutputArg(args, "output", "oneline", []string{"oneline", "full"})
	if errRes != nil {
		return errRes, nil
	}

	limit := parseIntArg(args, "limit", 10)
	maxLines := parseIntArg(args, "max_lines", 200)
	paths, errRes := pathsFromArgs(args)
	if errRes != nil {
		return errRes, nil
	}

	cmdArgs := []string{"log", fmt.Sprintf("-%d", limit)}
	if out == "oneline" {
		cmdArgs = append(cmdArgs, "--oneline", "--decorate")
	} else {
		// full: hash + subject + author + date relative
		cmdArgs = append(cmdArgs, "--format=%H|%s|%an|%ad", "--date=relative")
	}
	if rev != "" {
		cmdArgs = append(cmdArgs, rev)
	}
	if len(paths) > 0 {
		cmdArgs = append(cmdArgs, "--")
		for _, p := range paths {
			cmdArgs = append(cmdArgs, core.NormalizePath(p))
		}
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git log failed: %v\n%s", werr, output)), nil
	}

	if strings.TrimSpace(output) == "" {
		return mcp.NewToolResultText("No commits yet"), nil
	}

	// Apply max_lines truncation unconditionally; this is the LLM-consumer contract.
	return mcp.NewToolResultText(truncateOutput(output, maxLines)), nil
}

// gitShow displays a commit: stat by default, full/Name-only on demand.
// rev is required; paths optionally scopes the diff shown.
func gitShow(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	rev, _ := args["rev"].(string)
	if rev == "" {
		return usageError(
			"missing 'rev' parameter (required for show)",
			`git(action:"show", rev:"HEAD")`), nil
	}
	if errRes := rejectOptionLike("rev", rev); errRes != nil {
		return errRes, nil
	}

	out, errRes := parseOutputArg(args, "output", "stat", []string{"stat", "name-only", "full"})
	if errRes != nil {
		return errRes, nil
	}

	maxLines := parseIntArg(args, "max_lines", 200)
	paths, errRes := pathsFromArgs(args)
	if errRes != nil {
		return errRes, nil
	}

	cmdArgs := []string{"show", "--no-ext-diff", rev}
	switch out {
	case "stat":
		cmdArgs = append(cmdArgs, "--stat")
	case "name-only":
		cmdArgs = append(cmdArgs, "--name-only")
		// "full" → plain patch + commit metadata
	}
	if len(paths) > 0 {
		cmdArgs = append(cmdArgs, "--")
		for _, p := range paths {
			cmdArgs = append(cmdArgs, core.NormalizePath(p))
		}
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git show failed: %v\n%s", werr, output)), nil
	}

	if strings.TrimSpace(output) == "" {
		return mcp.NewToolResultText("No commit data for " + rev), nil
	}

	return mcp.NewToolResultText(truncateOutput(output, maxLines)), nil
}

// gitAdd stages files
// Scope resolution (priority order, no silent fallbacks):
//  1. paths (native array) — stage exactly those files
//  2. none of the above    — explicit error, NEVER default to -A
func gitAdd(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	paths, errRes := pathsFromArgs(args)
	if errRes != nil {
		return errRes, nil
	}

	if len(paths) == 0 {
		return usageError(
			"git add requires explicit 'paths' (no implicit 'add .')",
			`git(action:"add", paths:["src/file.php"])`), nil
	}

	// Normalize. "--" separator is REQUIRED: without it a path like "-A" or
	// "--pathspec-from-file=/etc/passwd" would be parsed by git as an option.
	normalized := make([]string, len(paths))
	for i, p := range paths {
		normalized[i] = core.NormalizePath(p)
	}

	// Pre-write hook
	hookCtx := &core.HookContext{
		Event:     core.HookPreWrite,
		ToolName:  "git",
		FilePath:  repoRoot,
		Operation: "add",
		Metadata:  map[string]interface{}{"git_operation": "add"},
	}
	if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreWrite, hookCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git add denied by hook: %v", err)), nil
	}

	cmdArgs := append([]string{"add", "--"}, normalized...)
	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git add failed: %v\n%s", werr, output)), nil
	}

	hookCtx.Event = core.HookPostWrite
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostWrite, hookCtx)

	if engine.IsCompactMode() {
		statusOut, _ := execGitCommand(repoRoot, "git", "status", "--porcelain")
		lines := strings.Split(statusOut, "\n")
		staged := 0
		for _, line := range lines {
			if len(line) > 0 && line[0] != '?' && line[0] != ' ' {
				staged++
			}
		}
		return mcp.NewToolResultText(fmt.Sprintf("OK: staged %d file(s)", staged)), nil
	}
	return mcp.NewToolResultText("Staged changes:\n" + output), nil
}

// gitCommit commits staged changes
//
// message: required (string)
// paths:   optional native array; passed to `git commit -- <paths>` (commits only those)
// When nothing is staged → usageError with the add example.
func gitCommit(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	message, _ := args["message"].(string)
	if message == "" {
		return usageError(
			"missing 'message' parameter (required for commit)",
			`git(action:"commit", message:"fix: short description")`), nil
	}

	// Pre-write hook — can deny commit
	hookCtx := &core.HookContext{
		Event:     core.HookPreWrite,
		ToolName:  "git",
		FilePath:  repoRoot,
		Operation: "commit",
		Metadata:  map[string]interface{}{"git_operation": "commit"},
	}
	if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreWrite, hookCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git commit denied by hook: %v", err)), nil
	}

	// Optional paths filter (commits a subset of the staged changes)
	paths, errRes := pathsFromArgs(args)
	if errRes != nil {
		return errRes, nil
	}

	// Get staged diff --stat for risk assessment
	stagedStat, werr := execGitCommand(repoRoot, "git", "diff", "--cached", "--shortstat")
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git commit failed: %v", werr)), nil
	}

	if strings.TrimSpace(stagedStat) == "" {
		return usageError(
			"nothing staged to commit",
			`git(action:"add", paths:["file.txt"])`), nil
	}

	// Risk assessment
	stagedFiles, stagedInsertions, stagedDeletions := parseShortStat(stagedStat)
	riskLevel := "LOW"
	if stagedFiles > 15 || stagedInsertions > 800 {
		riskLevel = "MEDIUM"
	}
	if stagedFiles > 40 || stagedInsertions > 3000 || stagedDeletions > 500 {
		riskLevel = "HIGH"
	}

	hookCtx.Content = message
	hookCtx.Metadata["risk"] = riskLevel
	hookCtx.Metadata["staged_files"] = stagedFiles
	hookCtx.Metadata["staged_insertions"] = stagedInsertions

	cmdArgs := []string{"commit", "-m", message}
	if len(paths) > 0 {
		cmdArgs = append(cmdArgs, "--")
		for _, p := range paths {
			cmdArgs = append(cmdArgs, core.NormalizePath(p))
		}
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git commit failed: %v\n%s", werr, output)), nil
	}

	hookCtx.Event = core.HookPostWrite
	hookCtx.Metadata["commit_hash"] = extractCommitHash(output)
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostWrite, hookCtx)

	commitHash, _ := execGitCommand(repoRoot, "git", "rev-parse", "--short", "HEAD")

	if engine.IsCompactMode() {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 0 {
			commitInfo := lines[0]
			rest := ""
			if len(lines) > 1 && strings.Contains(lines[1], "file") {
				rest = " " + strings.TrimSpace(lines[1])
			}
			return mcp.NewToolResultText(commitInfo + rest + fmt.Sprintf(" | risk:%s", riskLevel)), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	summary := fmt.Sprintf("✅ Commit: %s\nMessage: %s\nFiles: %d | +%d -%d\nRisk: %s",
		strings.TrimSpace(commitHash), message, stagedFiles, stagedInsertions, stagedDeletions, riskLevel)
	return mcp.NewToolResultText(summary), nil
}

// parseShortStat parses "N files changed, M insertions(+), D deletions(-)" from git diff --shortstat
func parseShortStat(shortstat string) (files, insertions, deletions int) {
	// More robust parsing using Field sequences and contains checks
	var insStr, delStr string
	var fileCount int

	fields := strings.Fields(shortstat)
	for i := 0; i < len(fields)-1; i++ {
		p := fields[i]
		next := fields[i+1]
		if p == "file," || p == "files," {
			fmt.Sscanf(next, "%d", &fileCount)
		}
		if next == "insertion(+)" {
			fmt.Sscanf(p, "%d", &insStr)
		}
		if strings.HasPrefix(next, "deletion") {
			fmt.Sscanf(p, "%d", &delStr)
		}
	}

	// Fallback simple split if parsing failed
	if fileCount == 0 {
		parts := strings.Split(shortstat, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.Contains(part, "file") {
				var n int
				fmt.Sscanf(part, "%d", &n)
				fileCount = n
			}
		}
	}

	return fileCount, insertions, deletions
}

// gitRestore restores files from index or a specific commit.

// gitRestore restores files from index or a specific commit.
//
// params (per docs/git-tool-spec.md §1):
//   - paths  native array, REQUIRED (refuse to default to whole tree)
//   - staged bool: --staged variant (non-destructive, unstage-only)
//   - rev    string: --source=<rev> variant
//
// Spec §2 row 'restore': no implicit whole-tree restore. Must pass `paths`.
// `isDestructiveGitAction` exempts `staged:true` from the force gate.
func gitRestore(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	staged, _ := args["staged"].(bool)
	rev, _ := args["rev"].(string)
	if rev != "" {
		if errRes := rejectOptionLike("rev", rev); errRes != nil {
			return errRes, nil
		}
	}

	paths, errRes := pathsFromArgs(args)
	if errRes != nil {
		return errRes, nil
	}
	if len(paths) == 0 {
		return usageError(
			"git restore requires explicit 'paths' (no implicit whole-tree restore)",
			`git(action:"restore", paths:["file.txt"])`), nil
	}
	for i := range paths {
		paths[i] = core.NormalizePath(paths[i])
	}

	// Pre-delete hook (destructive to working tree when not --staged)
	hookCtx := &core.HookContext{
		Event:     core.HookPreDelete,
		ToolName:  "git",
		FilePath:  repoRoot,
		Operation: "restore",
		Metadata:  map[string]interface{}{"git_operation": "restore", "staged": staged, "rev": rev},
	}
	if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreDelete, hookCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git restore denied by hook: %v", err)), nil
	}

	// Build command. Subcommand "restore" ALWAYS first; `rev` (when present)
	// must be passed as `--source=<rev>` (positional is parsed as a pathspec).
	cmdArgs := []string{"restore"}
	if staged {
		cmdArgs = append(cmdArgs, "--staged")
	}
	if rev != "" {
		cmdArgs = append(cmdArgs, "--source="+rev)
	}
	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, paths...)

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git restore failed: %v\n%s", werr, output)), nil
	}

	hookCtx.Event = core.HookPostDelete
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostDelete, hookCtx)

	scope := fmt.Sprintf("%d file(s)", len(paths))
	src := ""
	if rev != "" {
		src = " from " + rev
	}
	if engine.IsCompactMode() {
		return mcp.NewToolResultText(fmt.Sprintf("OK: restored %s%s", scope, src)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Restored %s%s\n%s", scope, src, output)), nil
}

// extractCommitHash extracts the commit hash from git commit output
func extractCommitHash(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "]") && strings.Contains(line, "files changed") {
			// Format: "[main abc1234] commit message"
			parts := strings.Fields(line)
			for i, p := range parts {
				if strings.HasPrefix(p, "[") && i+1 < len(parts) {
					return parts[i+1]
				}
			}
		}
	}
	return ""
}

// gitBranch: list (default), create (+checkout switch), or delete.
//
// params:
//   - name     branch name; absence → list mode
//   - checkout bool; if true with name, also `git switch -c <name>` (create+switch)
//   - force    bool; delete only — true escalates -d → -D
//
// Decision: `gitBranch` itself dispatches based on presence of `name` (list vs create/delete)
// and whether the branch already exists (delete) or not (create). Pre-existing audit doc
// gates `-d` vs `-D` on `force` (security audit 2026-07-02); preserved.
func gitBranch(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	name, _ := args["name"].(string)
	if name != "" {
		if errRes := rejectOptionLike("name", name); errRes != nil {
			return errRes, nil
		}
	}
	checkout, _ := args["checkout"].(bool)
	force, _ := args["force"].(bool)

	// ---- LIST (default, no name) ----
	if name == "" {
		output, werr := execGitCommand(repoRoot, "git", "branch", "-a")
		if werr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git branch failed: %v\n%s", werr, output)), nil
		}
		if engine.IsCompactMode() {
			lines := strings.Split(output, "\n")
			var result []string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					result = append(result, line)
				}
			}
			return mcp.NewToolResultText(strings.Join(result, "\n")), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	// ---- CREATE (name does not exist yet) ----
	// Check existence cheaply: `git show-ref --verify refs/heads/<name>` exits non-zero if missing.
	existsOut, existsErr := execGitCommand(repoRoot, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	branchExists := existsErr == nil && strings.TrimSpace(existsOut) == ""

	if !branchExists {
		hookCtx := &core.HookContext{
			Event:     core.HookPreCreate,
			ToolName:  "git",
			FilePath:  repoRoot,
			Operation: "branch-create",
			Metadata:  map[string]interface{}{"git_operation": "branch-create", "branch": name, "checkout": checkout},
		}
		if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreCreate, hookCtx); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git branch create denied: %v", err)), nil
		}

		var cmdArgs []string
		var label string
		if checkout {
			// `git switch -c <name>` — create AND switch in one go
			cmdArgs = []string{"switch", "-c", name}
			label = "created and switched to"
		} else {
			cmdArgs = []string{"branch", name}
			label = "created branch"
		}
		output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
		if werr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git branch create failed: %v\n%s", werr, output)), nil
		}

		hookCtx.Event = core.HookPostCreate
		engine.GetHookManager().ExecuteHooks(ctx, core.HookPostCreate, hookCtx)

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s %s", label, name)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("%s: %s", label, name)), nil
	}

	// ---- DELETE (branch already exists) ----
	hookCtx := &core.HookContext{
		Event:     core.HookPreDelete,
		ToolName:  "git",
		FilePath:  repoRoot,
		Operation: "branch-delete",
		Metadata:  map[string]interface{}{"git_operation": "branch-delete", "branch": name, "force": force},
	}
	if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreDelete, hookCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git branch delete denied: %v", err)), nil
	}

	deleteCmd := "-d"
	if force {
		deleteCmd = "-D"
	}
	output, werr := execGitCommand(repoRoot, "git", "branch", deleteCmd, name)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git branch delete failed: %v\n%s", werr, output)), nil
	}

	hookCtx.Event = core.HookPostDelete
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostDelete, hookCtx)

	if engine.IsCompactMode() {
		return mcp.NewToolResultText(fmt.Sprintf("OK: deleted branch %s", name)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Deleted branch: %s", name)), nil
}

// execGitCommand executes a git command in the given directory.
//
// Security: arguments are passed straight to the git process via
// CreateProcess (Windows) / execve (Unix) — never through a command
// interpreter — so shell metacharacters (& | % ^ ") in commit messages,
// branch names or paths are inert. A previous version fell back to
// `cmd /c git <args>` on Windows, where cmd.exe re-parses the command
// line and interprets those metacharacters (command-injection vector).
// The fallback was removed: Go resolves and executes .cmd/.bat shims
// directly via exec.Command, so the cmd.exe path never fired in
// practice and only added attack surface.
//
// Implementation note: we use cmd.Run() with explicit Stdout/Stderr buffers
// instead of cmd.CombinedOutput(). CombinedOutput() internally re-assigns
// cmd.Stdout and cmd.Stderr to its own buffers and requires those fields
// to be unset when called; keeping caller-owned buffers lets us build a
// structured error message that distinguishes stdout from stderr.
func execGitCommand(dir, command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return string(append(stdout.Bytes(), stderr.Bytes()...)),
			fmt.Errorf("%v: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// isDestructiveGitAction returns true for git operations that can cause data loss
// or are generally considered dangerous in an AI-driven environment.
//
// Important nuance for restore: `git restore --staged` is NOT destructive —
// it only moves staged changes back to the working tree (the equivalent of
// `git reset HEAD <path>`), it never discards work. Likewise `dry_run:true`
// performs no writes. Both should bypass the destructive gate so callers
// can use them safely without `force:true`.
func isDestructiveGitAction(action string, args map[string]any) bool {
	switch action {
	case "restore":
		staged, _ := args["staged"].(bool)
		// Non-destructive variant: unstage-only (`--staged` only moves staged → unstaged).
		// Working-tree restore or restore-with-rev discards changes → destructive.
		if staged {
			return false
		}
		return true
	case "branch":
		// branch delete is NOT gated here: `git branch -d` is safe by design
		// (git refuses to delete an unmerged branch). Escalation to the unsafe
		// `-D` is controlled by force inside gitBranch. Gating -d behind force
		// would be backwards — it would force every delete into the -D path.
		return false
	case "commit":
		return false
	}
	return false
}

// rejectOptionLike guards against git argument injection. User-supplied
// revisions, ranges and branch names are passed to git as positional
// arguments; a value beginning with "-" could be reinterpreted by git as an
// option (e.g. --output=<file>) instead of data. git itself forbids branch
// names and refs starting with "-", so rejecting them costs no legitimate use.
// Returns a non-nil error result if the value is option-like.
func rejectOptionLike(field, value string) *mcp.CallToolResult {
	if strings.HasPrefix(value, "-") {
		return mcp.NewToolResultError(fmt.Sprintf("invalid %s %q: value must not start with '-'", field, value))
	}
	return nil
}

// getBoolArg safely extracts a boolean argument
func getBoolArg(args map[string]any, key string) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}
