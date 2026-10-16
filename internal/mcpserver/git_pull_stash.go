package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func gitPull(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	remote, _ := args["remote"].(string)
	if remote == "" {
		remote = "origin"
	}
	if errRes := rejectOptionLike("remote", remote); errRes != nil {
		return errRes, nil
	}
	name, _ := args["name"].(string)
	if name != "" && !validGitBranch(name) {
		return mcp.NewToolResultError("invalid branch name"), nil
	}
	resolved, rerr := resolveGitRemote(repoRoot, remote)
	if rerr != nil {
		return mcp.NewToolResultError(redactGitText(fmt.Sprintf("git pull: cannot resolve remote %q: %v", remote, rerr))), nil
	}
	dests := resolved.Fetch
	if len(dests) == 0 {
		dests = resolved.Push
	}
	if denied := rejectUnauthorizedRemotes(engine, dests); denied != nil {
		return denied, nil
	}

	before, berr := gitOneLine(ctx, repoRoot, "rev-parse", "HEAD")
	if berr != nil {
		return mcp.NewToolResultError("git pull: cannot read HEAD: " + berr.Error()), nil
	}
	branch, _ := gitOneLine(ctx, repoRoot, "rev-parse", "--abbrev-ref", "HEAD")

	output, ferr := execGitCommandCtx(ctx, repoRoot, "git", "fetch", remote)
	if ferr != nil {
		return mcp.NewToolResultError(redactGitText(fmt.Sprintf("fetch failed; integration not attempted: %v\n%s", ferr, output))), nil
	}

	ref := ""
	if name != "" {
		ref = remote + "/" + name
	} else if branch != "" && branch != "HEAD" {
		up, uerr := gitOneLine(ctx, repoRoot, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
		if uerr != nil || up == "" {
			return mcp.NewToolResultText(fmt.Sprintf("fetch updated %s; integration not attempted: no upstream (pass name). HEAD %s unchanged", remote, before)), nil
		}
		ref = up
	} else {
		return mcp.NewToolResultText(fmt.Sprintf("fetch updated %s; integration not attempted: detached HEAD. local HEAD %s unchanged", remote, before)), nil
	}
	if !validGitBranch(strings.TrimPrefix(ref, remote+"/")) && !strings.Contains(ref, "/") {
		return mcp.NewToolResultError("refusing integration ref " + ref), nil
	}

	mergeOut, merr := execGitCommandCtx(ctx, repoRoot, "git", "merge", "--ff-only", ref)
	after, _ := gitOneLine(ctx, repoRoot, "rev-parse", "HEAD")
	if merr != nil {
		return mcp.NewToolResultError(redactGitText(fmt.Sprintf("integration failed; local HEAD unchanged (%s). fetch updated remote-tracking refs.\n%s", before, mergeOut))), nil
	}
	kind := "fast-forward"
	if after == before {
		kind = "already-up-to-date"
	}
	msg := fmt.Sprintf("pull --ff-only %s | %s | before %s | after %s", ref, kind, before, after)
	if strings.TrimSpace(mergeOut) != "" && !engine.IsCompactMode() {
		msg += "\n" + redactGitText(strings.TrimSpace(mergeOut))
	}
	return mcp.NewToolResultText(msg), nil
}

func gitStash(ctx context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	op, _ := args["stash_action"].(string)
	if op == "" {
		op = "list"
	}
	switch op {
	case "list", "show", "push", "apply", "drop", "pop":
	default:
		return usageError("stash_action must be list, show, push, apply, drop, or pop", `git(action:"stash", stash_action:"list")`), nil
	}
	switch op {
	case "list":
		out, err := execGitCommandCtx(ctx, repoRoot, "git", "stash", "list")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out = strings.TrimSpace(out)
		if out == "" {
			return mcp.NewToolResultText("stash: empty"), nil
		}
		return mcp.NewToolResultText(out), nil
	case "show":
		oid, errRes := requireStashOID(args)
		if errRes != nil {
			return errRes, nil
		}
		out, err := execGitCommandCtx(ctx, repoRoot, "git", "stash", "show", "-p", oid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("stash show %s: %v\n%s", oid, err, out)), nil
		}
		return mcp.NewToolResultText(out), nil
	case "push":
		cmd := []string{"stash", "push"}
		if msg, _ := args["message"].(string); msg != "" {
			cmd = append(cmd, "-m", msg)
		}
		if getBoolArg(args, "include_untracked") {
			cmd = append(cmd, "-u")
		}
		if paths := gitPathArgs(args); len(paths) > 0 {
			cmd = append(cmd, "--")
			cmd = append(cmd, paths...)
		}
		out, err := execGitCommandCtx(ctx, repoRoot, "git", cmd...)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("stash push: %v\n%s", err, out)), nil
		}
		oid, oerr := gitOneLine(ctx, repoRoot, "rev-parse", "stash")
		if oerr != nil {
			return mcp.NewToolResultText(strings.TrimSpace(out) + "\noid: unavailable"), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("stashed %s\n%s", oid, strings.TrimSpace(out))), nil
	case "apply":
		oid, errRes := requireStashOID(args)
		if errRes != nil {
			return errRes, nil
		}
		out, err := execGitCommandCtx(ctx, repoRoot, "git", "stash", "apply", oid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("stash apply %s failed; stash kept\n%s", oid, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("applied %s; stash kept\n%s", oid, strings.TrimSpace(out))), nil
	case "pop":
		oid, errRes := requireStashOID(args)
		if errRes != nil {
			return errRes, nil
		}
		out, err := execGitCommandCtx(ctx, repoRoot, "git", "stash", "apply", oid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("stash pop %s failed; stash kept\n%s", oid, out)), nil
		}
		if derr := dropStashOID(ctx, repoRoot, oid); derr != nil {
			return mcp.NewToolResultText(fmt.Sprintf("applied %s; drop failed, stash kept: %v", oid, derr)), nil
		}
		return mcp.NewToolResultText("popped " + oid), nil
	case "drop":
		oid, errRes := requireStashOID(args)
		if errRes != nil {
			return errRes, nil
		}
		if err := dropStashOID(ctx, repoRoot, oid); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("dropped " + oid), nil
	default:
		return usageError("unknown stash_action", `git(action:"stash", stash_action:"list")`), nil
	}
}

func requireStashOID(args map[string]interface{}) (string, *mcp.CallToolResult) {
	oid, _ := args["rev"].(string)
	oid = strings.TrimSpace(oid)
	if !validStashOID(oid) {
		return "", mcp.NewToolResultError("rev must be a stash commit OID (7-40 hex digits), not stash@{n}")
	}
	return oid, nil
}

func validStashOID(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func validGitBranch(name string) bool {
	if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "@") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '/' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func gitOneLine(ctx context.Context, repo string, args ...string) (string, error) {
	out, err := execGitCommandCtx(ctx, repo, "git", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitPathArgs(args map[string]interface{}) []string {
	raw, ok := args["paths"]
	if !ok || raw == nil {
		return nil
	}
	paths, err := core.DecodePaths(raw)
	if err != nil {
		return nil
	}
	return paths
}

func dropStashOID(ctx context.Context, repo, oid string) error {
	out, err := execGitCommandCtx(ctx, repo, "git", "stash", "list", "--format=%H%x09%gd")
	if err != nil {
		return err
	}
	var ref string
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == oid || strings.HasPrefix(parts[0], oid) {
			ref = parts[1]
			oid = parts[0]
			break
		}
	}
	if ref == "" {
		return fmt.Errorf("stash %s not in stash list", oid)
	}
	again, err := execGitCommandCtx(ctx, repo, "git", "stash", "list", "--format=%H%x09%gd")
	if err != nil {
		return err
	}
	if !strings.Contains(again, oid+"\t"+ref) {
		return fmt.Errorf("stash ref moved before drop; retry with the current OID")
	}
	_, err = execGitCommandCtx(ctx, repo, "git", "stash", "drop", ref)
	return err
}
