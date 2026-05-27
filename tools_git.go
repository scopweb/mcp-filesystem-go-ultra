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
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
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
func gitAdd(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	all, _ := args["all"].(bool)
	dryRun, _ := args["dry_run"].(bool)

	var cmdArgs []string
	if all {
		cmdArgs = []string{"add", "-A"}
	} else {
		// Add specific files if provided, otherwise stage all
		filePath, _ := args["path"].(string)
		if filePath != "" {
			cmdArgs = []string{"add", core.NormalizePath(filePath)}
		} else {
			cmdArgs = []string{"add", "-A"}
		}
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

	if engine.IsCompactMode() {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 0 {
			commitInfo := lines[0]
			rest := ""
			if len(lines) > 1 && strings.Contains(lines[1], "file") {
				rest = " " + strings.TrimSpace(lines[1])
			}
			return mcp.NewToolResultText(commitInfo + rest), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	return mcp.NewToolResultText(output), nil
}

// parseShortStat parses "N files changed, M insertions(+), D deletions(-)" from git diff --shortstat
func parseShortStat(shortstat string) (files, insertions, deletions int) {
	parts := strings.Fields(shortstat)
	for i, p := range parts {
		if p == "file" && i > 0 {
			fmt.Sscanf(parts[i-1], "%d", &files)
		}
		if p == "insertion(+)" && i > 0 {
			fmt.Sscanf(parts[i-1], "%d", &insertions)
		}
		if strings.HasPrefix(p, "deletion") && i > 0 {
			fmt.Sscanf(parts[i-1], "%d", &deletions)
		}
	}
	return
}

// generateAutoCommitMessage generates a conventional commit message from staged changes
func generateAutoCommitMessage(repoRoot string, stagedFiles int) string {
	// Get diff to determine type
	diffOutput, _ := execGitCommand(repoRoot, "git", "diff", "--cached", "--stat")
	if diffOutput == "" {
		return ""
	}

	// Simple heuristic: check for test files, docs, config changes
	hasTests := strings.Contains(diffOutput, "_test.go") || strings.Contains(diffOutput, ".test.")
	hasDocs := strings.Contains(diffOutput, ".md") || strings.Contains(diffOutput, "docs/")
	hasConfig := strings.Contains(diffOutput, "config") || strings.Contains(diffOutput, ".json") || strings.Contains(diffOutput, ".yaml")

	// Determine type based on files changed
	commitType := "feat"
	if hasTests && !hasDocs {
		commitType = "test"
	} else if hasDocs && !hasTests {
		commitType = "docs"
	} else if hasConfig {
		commitType = "chore"
	}

	// Generate message from stats
	description := fmt.Sprintf("update %d file(s)", stagedFiles)

	return fmt.Sprintf("%s: %s", commitType, description)
}

// gitRestore restores files from index or a specific commit
func gitRestore(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	pathsStr, _ := args["paths"].(string)
	staged, _ := args["staged"].(bool)
	source, _ := args["source"].(string)
	dryRun, _ := args["dry_run"].(bool)

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

	var paths []string
	if pathsStr != "" {
		if err := json.Unmarshal([]byte(pathsStr), &paths); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid paths JSON: %v", err)), nil
		}
	}

	if len(paths) == 0 {
		return mcp.NewToolResultError("paths required for restore. Usage: git(action:\"restore\", paths:'[\"file1.txt\",\"file2.txt\"]')"), nil
	}

	for i := range paths {
		paths[i] = core.NormalizePath(paths[i])
	}

	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, paths...)

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
		return mcp.NewToolResultText(fmt.Sprintf("OK: restored %d file(s) from %s", len(paths), source)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Restored %d file(s)%s\n%s",
		len(paths), func() string { if source != "" { return " from " + source }; return "" }(), output)), nil
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

// execGitCommand executes a git command in the given directory
func execGitCommand(dir, command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir

	if runtime.GOOS == "windows" {
		cmd2 := exec.Command("git", args...)
		cmd2.Dir = dir
		var stderr bytes.Buffer
		cmd2.Stderr = &stderr
		output, err := cmd2.CombinedOutput()
		if err != nil {
			fullCmd := "git " + strings.Join(args, " ")
			cmd3 := exec.Command("cmd", "/c", fullCmd)
			cmd3.Dir = dir
			cmd3.Stderr = &stderr
			output, err = cmd3.CombinedOutput()
			return string(output), err
		}
		return string(output), nil
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%v: %s", err, stderr.String())
	}
	return string(output), nil
}
