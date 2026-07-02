package main

import (
	"bytes"
	"context"
	"encoding/json"
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
		mcp.WithDescription("git — Git operations: status, diff, log, add, commit, branch, restore, init. "+
			"Must be run from within a git repository. Related: analyze_operation, edit_file."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true), // Some actions (restore, branch delete, etc.) are destructive
		mcp.WithIdempotentHintAnnotation(false), // commit, restore, branch delete are not idempotent

		mcp.WithString("action", mcp.Required(), mcp.Description("Action: status, diff, log, add, commit, branch, restore, init")),
		mcp.WithString("path", mcp.Description("Working directory or file path (default: auto-detect repo root)")),
		mcp.WithString("message", mcp.Description("Commit message (for commit action)")),
		mcp.WithBoolean("auto_message", mcp.Description("Auto-generate conventional commit message from staged changes (default: false)")),
		mcp.WithString("paths", mcp.Description("JSON array of file paths (for add, restore)")),
		mcp.WithString("branch_action", mcp.Description("For branch: list (default), create, delete")),
		mcp.WithString("branch_name", mcp.Description("Branch name for create/delete")),
		mcp.WithString("source", mcp.Description("Source commit for restore (e.g. HEAD~1, abc1234)")),
		mcp.WithBoolean("staged", mcp.Description("Restore staged changes instead of working tree (for restore action)")),
		mcp.WithBoolean("all", mcp.Description("Stage all modified files (for add action)")),
		mcp.WithString("commit_range", mcp.Description("Diff between two commits: commit1..commit2")),
		mcp.WithNumber("max_count", mcp.Description("Max log entries to return (default: 10)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview without applying (for add, restore)")),
		mcp.WithBoolean("force", mcp.Description("Force push/delete (skip safety checks)")),
	)

	reg.addTool(gitTool, auditWrap(engine, "git", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})

		action, _ := args["action"].(string)
		path, _ := args["path"].(string)

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
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Check access control
		if !engine.IsPathAllowed(repoRoot) {
			return mcp.NewToolResultError("access denied: path outside allowed directories"), nil
		}

		// Anti-destructive protection for dangerous git operations
		if isDestructiveGitAction(action, args) && !getBoolArg(args, "force") {
			return mcp.NewToolResultError(
				fmt.Sprintf("Destructive git operation '%s' requires force=true for safety. "+
					"Use force=true only if you are sure.", action)), nil
		}

		switch action {
		case "status":
			return gitStatus(ctx, engine, repoRoot)
		case "diff":
			return gitDiff(ctx, engine, repoRoot, args)
		case "log":
			return gitLog(ctx, engine, repoRoot, args)
		case "add":
			return gitAdd(ctx, engine, repoRoot, args)
		case "commit":
			return gitCommit(ctx, engine, repoRoot, args)
		case "restore":
			return gitRestore(ctx, engine, repoRoot, args)
		case "branch":
			return gitBranch(ctx, engine, repoRoot, args)
		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown action: %s. Valid: status, diff, log, add, commit, restore, branch, init", action)), nil
		}
	}))
}

// gitInit initializes a new git repository
func gitInit(ctx context.Context, engine *core.UltraFastEngine, path string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	targetPath := path
	if targetPath == "" {
		targetPath = "."
	}
	targetPath = core.NormalizePath(targetPath)

	if !engine.IsPathAllowed(targetPath) {
		return mcp.NewToolResultError("access denied: path outside allowed directories"), nil
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

// gitStatus returns the current git status
func gitStatus(ctx context.Context, engine *core.UltraFastEngine, repoRoot string) (*mcp.CallToolResult, error) {
	output, werr := execGitCommand(repoRoot, "git", "status", "--porcelain=v1", "-b")
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git status failed: %v\n%s", werr, output)), nil
	}

	if engine.IsCompactMode() {
		return gitStatusCompact(repoRoot, output)
	}
	return mcp.NewToolResultText(output), nil
}

// gitStatusCompact returns a compact one-line status summary
func gitStatusCompact(repoRoot, output string) (*mcp.CallToolResult, error) {
	lines := strings.Split(output, "\n")

	currentBranch := ""
	stagedCount, unstagedCount, untrackedCount := 0, 0, 0

	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			arrowIdx := strings.Index(line, " -> ")
			if arrowIdx != -1 {
				currentBranch = line[arrowIdx+5:]
			} else {
				parts := strings.Fields(line[3:])
				if len(parts) > 0 {
					currentBranch = strings.TrimSuffix(parts[0], "(...")
				}
			}
			continue
		}

		staged := line[0]
		unstaged := line[1]

		if staged != ' ' && staged != '?' {
			stagedCount++
		}
		if unstaged == '?' {
			untrackedCount++
		} else if unstaged != ' ' {
			unstagedCount++
		}
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

// gitDiff returns the diff output
func gitDiff(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	staged, _ := args["staged"].(bool)
	commitRange, _ := args["commit_range"].(string)
	if commitRange != "" {
		if errRes := rejectOptionLike("commit_range", commitRange); errRes != nil {
			return errRes, nil
		}
	}

	var cmdArgs []string
	if commitRange != "" {
		cmdArgs = []string{"diff", "--no-ext-diff", commitRange}
	} else if staged {
		cmdArgs = []string{"diff", "--cached", "--no-ext-diff"}
	} else {
		cmdArgs = []string{"diff", "--no-ext-diff"}
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git diff failed: %v\n%s", werr, output)), nil
	}

	if output == "" {
		if commitRange != "" {
			return mcp.NewToolResultText("No changes in range " + commitRange), nil
		}
		if staged {
			return mcp.NewToolResultText("No staged changes"), nil
		}
		return mcp.NewToolResultText("No unstaged changes"), nil
	}

	if engine.IsCompactMode() {
		lines := strings.Split(output, "\n")
		maxLines := 50
		truncated := len(lines) > maxLines
		if truncated {
			lines = lines[:maxLines]
		}
		result := strings.Join(lines, "\n")
		if truncated {
			totalLines := len(strings.Split(output, "\n"))
			result += fmt.Sprintf("\n... (%d more lines — run git(action:\"diff\") for full output)", totalLines-maxLines)
		}
		return mcp.NewToolResultText(result), nil
	}

	return mcp.NewToolResultText(output), nil
}

// gitLog returns the commit log
func gitLog(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	maxCount := 10
	if mc, ok := args["max_count"].(float64); ok && mc > 0 {
		maxCount = int(mc)
	}

	cmdArgs := []string{"log", fmt.Sprintf("-%d", maxCount), "--oneline", "--decorate"}
	if !engine.IsCompactMode() {
		cmdArgs = []string{"log", fmt.Sprintf("-%d", maxCount),
			"--format=%H|%s|%an|%ad", "--date=relative"}
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git log failed: %v\n%s", werr, output)), nil
	}

	if output == "" {
		return mcp.NewToolResultText("No commits yet"), nil
	}

	if engine.IsCompactMode() {
		lines := strings.Split(output, "\n")
		var formatted []string
		for _, line := range lines {
			if strings.Contains(line, "|") {
				parts := strings.SplitN(line, "|", 4)
				if len(parts) >= 4 {
					hash := parts[0][:8]
					msg := parts[1]
					author := parts[2]
					date := parts[3]
					formatted = append(formatted, fmt.Sprintf("%s %q | %s | %s", hash, msg, author, date))
					continue
				}
			}
			formatted = append(formatted, line)
		}
		return mcp.NewToolResultText(strings.Join(formatted, "\n")), nil
	}

	return mcp.NewToolResultText(output), nil
}

// gitAdd stages files
// Scope resolution (priority order, no silent fallbacks):
//  1. paths (JSON array) — stage exactly those files
//  2. path (string)      — stage that single file
//  3. all:true           — stage entire working tree (-A)
//  4. dry_run:true       — preview whatever scope was selected above
//  5. none of the above  — explicit error, NEVER default to -A
func gitAdd(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	all, _ := args["all"].(bool)
	dryRun, _ := args["dry_run"].(bool)
	pathsStr, _ := args["paths"].(string)
	filePath, _ := args["path"].(string)

	var paths []string
	if pathsStr != "" {
		if err := json.Unmarshal([]byte(pathsStr), &paths); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid paths JSON: %v", err)), nil
		}
	}

	var cmdArgs []string
	switch {
	case len(paths) > 0:
		// Explicit list — highest priority, exact scope
		normalized := make([]string, len(paths))
		for i, p := range paths {
			normalized[i] = core.NormalizePath(p)
		}
		cmdArgs = append([]string{"add"}, normalized...)
	case filePath != "":
		// Single path — second priority
		cmdArgs = []string{"add", core.NormalizePath(filePath)}
	case all:
		// Explicit opt-in to stage entire tree
		cmdArgs = []string{"add", "-A"}
	default:
		// No scope specified — refuse silently-defaulting to -A
		return mcp.NewToolResultError(
			"git add requires one of: paths (JSON array), path (string), or all:true. " +
				"Refusing to stage entire repo without explicit scope."), nil
	}

	// Pre-write hook for add operation
	hookCtx := &core.HookContext{
		Event:     core.HookPreWrite,
		ToolName:  "git",
		FilePath:  repoRoot,
		Operation: "add",
		Metadata:  map[string]interface{}{"git_operation": "add", "all": all, "dry_run": dryRun},
	}
	if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreWrite, hookCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git add denied by hook: %v", err)), nil
	}

	if dryRun {
		cmdArgs = append(cmdArgs, "-n")
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git add failed: %v\n%s", werr, output)), nil
	}

	hookCtx.Event = core.HookPostWrite
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostWrite, hookCtx)

	if dryRun {
		if engine.IsCompactMode() {
			return mcp.NewToolResultText("DRY RUN: " + output), nil
		}
		return mcp.NewToolResultText("Dry run — would stage:\n" + output), nil
	}

	if engine.IsCompactMode() {
		// Count staged files from status
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
func gitCommit(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	message, _ := args["message"].(string)
	autoMessage, _ := args["auto_message"].(bool)
	force, _ := args["force"].(bool)

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

	// Get staged diff --stat for risk assessment
	stagedStat, werr := execGitCommand(repoRoot, "git", "diff", "--cached", "--shortstat")
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git commit failed: %v", werr)), nil
	}

	if strings.TrimSpace(stagedStat) == "" {
		return mcp.NewToolResultError("No staged changes to commit. Stage files first: git(action:\"add\") or git(action:\"add\", all:true)"), nil
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

	if riskLevel == "HIGH" && !force {
		return mcp.NewToolResultError(fmt.Sprintf(
			"HIGH risk commit detected: %d files, ~%d insertions, %d deletions. Set force:true if you want to proceed anyway.", stagedFiles, stagedInsertions, stagedDeletions)), nil
	}

	// Auto-generate message from staged content if requested
	if autoMessage && message == "" {
		message = generateAutoCommitMessage(repoRoot, stagedFiles)
		if message == "" {
			return mcp.NewToolResultError("auto_message: true but could not determine commit type. Please provide message manually."), nil
		}
	}

	if message == "" {
		return mcp.NewToolResultError("commit message is required. Usage: git(action:\"commit\", message:\"your message\") or git(action:\"commit\", auto_message:true)"), nil
	}

	hookCtx.Content = message
	hookCtx.Metadata["risk"] = riskLevel
	hookCtx.Metadata["staged_files"] = stagedFiles
	hookCtx.Metadata["staged_insertions"] = stagedInsertions

	// Execute commit
	output, werr := execGitCommand(repoRoot, "git", "commit", "-m", message)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git commit failed: %v\n%s", werr, output)), nil
	}

	hookCtx.Event = core.HookPostWrite
	hookCtx.Metadata["commit_hash"] = extractCommitHash(output)
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostWrite, hookCtx)

	// Build richer success response with commit metadata
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

	// Verbose mode: return structured summary
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

// generateAutoCommitMessage generates a conventional commit message from staged changes
// Uses a single --numstat --name-only call to get both file names and deletion counts
func generateAutoCommitMessage(repoRoot string, stagedFiles int) string {
	// Single call: --numstat --name-only gives "ins del filename" per line
	output, _ := execGitCommand(repoRoot, "git", "diff", "--cached", "--numstat", "--name-only")
	if output == "" {
		return ""
	}

	files := strings.Split(strings.TrimSpace(output), "\n")
	var fileNames []string
	hasTests, hasDocs, hasFix, hasConfig, hasRefactor := false, false, false, false, false
	totalDeletions := 0

	for _, line := range files {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			// "N     N     path/to/file" — fields[0]=ins, fields[1]=del, fields[2]=path
			if fields[1] != "-" {
				var del int
				fmt.Sscanf(fields[1], "%d", &del)
				totalDeletions += del
			}
			fileNames = append(fileNames, fields[2])
		} else if len(fields) >= 1 {
			fileNames = append(fileNames, fields[0])
		}
	}

	// Detect commit type from file names
	for _, f := range fileNames {
		hasTests = hasTests || strings.Contains(f, "_test.go") || strings.Contains(f, ".test.")
		hasDocs = hasDocs || strings.HasSuffix(f, ".md") || strings.Contains(f, "docs/")
		hasConfig = hasConfig || strings.Contains(f, "config") || strings.HasSuffix(f, ".json") || strings.HasSuffix(f, ".yaml") || strings.HasSuffix(f, ".yml")
	}

	// Fallback: inspect raw output for fix/refactor keywords
	lower := strings.ToLower(output)
	hasFix = strings.Contains(lower, "fix") || strings.Contains(lower, "bug") || strings.Contains(lower, "patch")
	hasRefactor = strings.Contains(lower, "refactor") || strings.Contains(lower, "rename") || strings.Contains(lower, "move")

	description := fmt.Sprintf("update %d file(s)", stagedFiles)

	switch {
	case hasFix || totalDeletions > 200:
		return "fix: " + description
	case hasTests && !hasDocs:
		return "test: " + description
	case hasDocs:
		return "docs: " + description
	case hasConfig:
		return "chore: " + description
	case hasRefactor || totalDeletions > 100:
		return "refactor: " + description
	default:
		return "feat: " + description
	}
}

// gitRestore restores files from index or a specific commit.
//
// Validation order (intentional, all checks happen BEFORE the destructive
// force gate in the dispatcher):
//  1. source option-injection check (if source supplied)
//  2. required params: at least one of `paths` (non-empty JSON array) or `source`
//  3. paths JSON parse (if paths supplied)
//  4. path normalization
//  5. command construction
//
// Note: `git restore --staged` is non-destructive (only moves staged →
// unstaged, never touches the working tree) and therefore does NOT require
// force. The dispatcher accounts for this via isDestructiveGitAction.
func gitRestore(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	pathsStr, _ := args["paths"].(string)
	staged, _ := args["staged"].(bool)
	source, _ := args["source"].(string)
	dryRun, _ := args["dry_run"].(bool)

	if source != "" {
		if errRes := rejectOptionLike("source", source); errRes != nil {
			return errRes, nil
		}
	}

	// Parse paths (may be empty when source is provided — git restore --source
	// without paths is valid: restores the whole tree to that source).
	var paths []string
	if pathsStr != "" {
		if err := json.Unmarshal([]byte(pathsStr), &paths); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid paths JSON: %v", err)), nil
		}
	}

	// Required-params check runs BEFORE the force gate in the dispatcher so
	// the user sees a coherent error for a malformed call instead of being
	// bounced off the destructive-operation gate first.
	if len(paths) == 0 && source == "" {
		return mcp.NewToolResultError(
			"git restore requires either paths (JSON array) or source (commit ref). " +
				"Usage: git(action:\"restore\", paths:'[\"file.txt\"]') or " +
				"git(action:\"restore\", source:\"HEAD~1\")"), nil
	}

	for i := range paths {
		paths[i] = core.NormalizePath(paths[i])
	}

	// Pre-delete hook for restore (it's destructive to working tree)
	hookCtx := &core.HookContext{
		Event:     core.HookPreDelete,
		ToolName:  "git",
		FilePath:  repoRoot,
		Operation: "restore",
		Metadata:  map[string]interface{}{"git_operation": "restore", "staged": staged, "source": source},
	}
	if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreDelete, hookCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git restore denied by hook: %v", err)), nil
	}

	var cmdArgs []string
	if staged {
		cmdArgs = append(cmdArgs, "--staged")
	}
	if source != "" {
		cmdArgs = append(cmdArgs, source)
	}

	if len(paths) > 0 {
		cmdArgs = append(cmdArgs, "--")
		cmdArgs = append(cmdArgs, paths...)
	}

	if dryRun {
		cmdArgs = append([]string{"restore", "-n"}, cmdArgs...)
		output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
		if werr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git restore dry-run failed: %v\n%s", werr, output)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText("DRY RUN: would restore:\n" + output), nil
		}
		return mcp.NewToolResultText("Dry run — would restore:\n" + output), nil
	}

	output, werr := execGitCommand(repoRoot, "git", cmdArgs...)
	if werr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("git restore failed: %v\n%s", werr, output)), nil
	}

	hookCtx.Event = core.HookPostDelete
	engine.GetHookManager().ExecuteHooks(ctx, core.HookPostDelete, hookCtx)

	if engine.IsCompactMode() {
		src := ""
		if source != "" {
			src = " from " + source
		}
		return mcp.NewToolResultText(fmt.Sprintf("OK: restored %d file(s)%s", len(paths), src)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Restored %d file(s)%s\n%s",
		len(paths), func() string {
			if source != "" {
				return " from " + source
			}
			return ""
		}(), output)), nil
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

// gitBranch lists, creates, or deletes branches
func gitBranch(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	branchAction, _ := args["branch_action"].(string)
	branchName, _ := args["branch_name"].(string)

	if branchName != "" {
		if errRes := rejectOptionLike("branch_name", branchName); errRes != nil {
			return errRes, nil
		}
	}

	// List branches
	if branchAction == "" || branchAction == "list" {
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

	// Create branch
	if branchAction == "create" {
		if branchName == "" {
			return mcp.NewToolResultError("branch_name required for create. Usage: git(action:\"branch\", branch_action:\"create\", branch_name:\"feature/new\")"), nil
		}

		hookCtx := &core.HookContext{
			Event:     core.HookPreCreate,
			ToolName:  "git",
			FilePath:  repoRoot,
			Operation: "branch-create",
			Metadata:  map[string]interface{}{"git_operation": "branch-create", "branch": branchName},
		}
		if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreCreate, hookCtx); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git branch create denied: %v", err)), nil
		}

		output, werr := execGitCommand(repoRoot, "git", "branch", branchName)
		if werr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git branch create failed: %v\n%s", werr, output)), nil
		}

		hookCtx.Event = core.HookPostCreate
		engine.GetHookManager().ExecuteHooks(ctx, core.HookPostCreate, hookCtx)

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: created branch %s", branchName)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Created branch: %s", branchName)), nil
	}

	// Delete branch
	if branchAction == "delete" {
		if branchName == "" {
			return mcp.NewToolResultError("branch_name required for delete. Usage: git(action:\"branch\", branch_action:\"delete\", branch_name:\"feature/old\")"), nil
		}

		hookCtx := &core.HookContext{
			Event:     core.HookPreDelete,
			ToolName:  "git",
			FilePath:  repoRoot,
			Operation: "branch-delete",
			Metadata:  map[string]interface{}{"git_operation": "branch-delete", "branch": branchName},
		}
		force := false
		if f, ok := args["force"].(bool); ok && f {
			force = true
		}

		if _, err := engine.GetHookManager().ExecuteHooks(ctx, core.HookPreDelete, hookCtx); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git branch delete denied: %v", err)), nil
		}

		deleteCmd := "-d"
		if force {
			deleteCmd = "-D"
		}
		output, werr := execGitCommand(repoRoot, "git", "branch", deleteCmd, branchName)
		if werr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git branch delete failed: %v\n%s", werr, output)), nil
		}

		hookCtx.Event = core.HookPostDelete
		engine.GetHookManager().ExecuteHooks(ctx, core.HookPostDelete, hookCtx)

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: deleted branch %s", branchName)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Deleted branch: %s", branchName)), nil
	}

	return mcp.NewToolResultError(fmt.Sprintf("Unknown branch_action: %s. Valid: list, create, delete", branchAction)), nil
}

// execGitCommand executes a git command in the given directory.
//
// Implementation note: we use cmd.Run() with explicit Stdout/Stderr buffers
// instead of cmd.CombinedOutput(). Reason: CombinedOutput() internally
// re-assigns cmd.Stdout and cmd.Stderr to its own buffers and returns the
// merged stream — but it requires those fields to be unset when called.
// On Windows, the cmd.exe fallback in particular would pre-assign Stderr
// (and CombinedOutput would then panic with "exec: Stderr already set")
// whenever the primary `git` invocation exited non-zero and control fell
// through to the cmd.exe branch. Run() with caller-owned buffers avoids
// the collision entirely and also lets us build a structured error message
// that distinguishes stdout from stderr.
func execGitCommand(dir, command string, args ...string) (string, error) {
	if runtime.GOOS == "windows" {
		// Try git directly first (safest)
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			return stdout.String(), nil
		}

		// Safer fallback: pass arguments properly instead of string concatenation.
		// This prevents command injection even if paths contain special characters.
		cmdArgs := append([]string{"/c", "git"}, args...)
		cmd2 := exec.Command("cmd", cmdArgs...)
		cmd2.Dir = dir
		var stdout2, stderr2 bytes.Buffer
		cmd2.Stdout = &stdout2
		cmd2.Stderr = &stderr2
		err = cmd2.Run()
		out := stdout2.Bytes()
		if err != nil {
			out = append(stdout2.Bytes(), stderr2.Bytes()...)
		}
		return string(out), err
	}

	// Unix
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
		dryRun, _ := args["dry_run"].(bool)
		// Non-destructive variants: unstage-only or preview-only
		if staged || dryRun {
			return false
		}
		// git restore can discard changes or restore from other commits
		return true
	case "branch":
		branchAction, _ := args["branch_action"].(string)
		if branchAction == "delete" {
			return true
		}
	case "commit":
		// Commit itself is not extremely dangerous, but we can make it require force
		// in some contexts. For now we keep it lenient.
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
