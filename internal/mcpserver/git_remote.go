package mcpserver

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

var (
	gitURLUserPass = regexp.MustCompile(`(?i)(https?://)([^/@:\s]+):([^/@\s]+)@`)
	gitURLUserInfo = regexp.MustCompile(`(?i)(https?://)([^/@\s]+)@`)
)

type insteadOfRule struct {
	Base      string
	InsteadOf string
}

type resolvedRemote struct {
	Name  string
	Fetch []string
	Push  []string
}

func redactGitURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	s = gitURLUserPass.ReplaceAllString(s, "${1}***@")
	s = gitURLUserInfo.ReplaceAllString(s, "${1}***@")
	return s
}

func redactGitText(s string) string {
	return redactGitURL(s)
}

func parseGitInsteadOfRules(configOut string) []insteadOfRule {
	var rules []insteadOfRule
	for _, line := range strings.Split(configOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		key, instead := fields[0], fields[1]
		const prefix, suffix = "url.", ".insteadof"
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(strings.ToLower(key), suffix) {
			continue
		}
		base := key[len(prefix) : len(key)-len(suffix)]
		rules = append(rules, insteadOfRule{Base: base, InsteadOf: instead})
	}
	return rules
}

func applyInsteadOf(u string, rules []insteadOfRule) string {
	bestLen := -1
	repl := ""
	prefix := ""
	for _, r := range rules {
		if strings.HasPrefix(u, r.InsteadOf) && len(r.InsteadOf) > bestLen {
			bestLen = len(r.InsteadOf)
			repl = r.Base
			prefix = r.InsteadOf
		}
	}
	if bestLen < 0 {
		return u
	}
	return repl + u[len(prefix):]
}

func gitConfigURLs(repoRoot, name string, push bool) []string {
	args := []string{"remote", "get-url", "--all", name}
	if push {
		args = []string{"remote", "get-url", "--push", "--all", name}
	}
	out, err := execGitCommand(repoRoot, "git", args...)
	if err != nil || strings.TrimSpace(out) == "" {
		if push {
			out, err = execGitCommand(repoRoot, "git", "remote", "get-url", "--push", name)
		} else {
			out, err = execGitCommand(repoRoot, "git", "remote", "get-url", name)
		}
		if err != nil {
			return nil
		}
	}
	var urls []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		urls = append(urls, line)
	}
	return urls
}

func loadInsteadOfRules(repoRoot string) []insteadOfRule {
	out, err := execGitCommand(repoRoot, "git", "config", "--get-regexp", `^url\..*\.insteadof$`)
	if err != nil {
		return nil
	}
	return parseGitInsteadOfRules(out)
}

func resolveGitRemote(repoRoot, name string) (resolvedRemote, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return resolvedRemote{}, fmt.Errorf("remote name is empty")
	}
	rules := loadInsteadOfRules(repoRoot)
	fetch := gitConfigURLs(repoRoot, name, false)
	push := gitConfigURLs(repoRoot, name, true)
	if len(fetch) == 0 && len(push) == 0 {
		return resolvedRemote{}, fmt.Errorf("remote %q not found", name)
	}
	apply := func(list []string) []string {
		out := make([]string, 0, len(list))
		seen := map[string]bool{}
		for _, u := range list {
			u = applyInsteadOf(u, rules)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
		return out
	}
	return resolvedRemote{
		Name:  name,
		Fetch: apply(fetch),
		Push:  apply(push),
	}, nil
}

func listGitRemoteNames(repoRoot string) ([]string, error) {
	out, err := execGitCommand(repoRoot, "git", "remote")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func formatResolvedRemote(r resolvedRemote, compact bool) string {
	fetch := redactGitURLs(r.Fetch)
	push := redactGitURLs(r.Push)
	if compact {
		dest := strings.Join(push, ", ")
		if dest == "" {
			dest = strings.Join(fetch, ", ")
		}
		if dest == "" {
			dest = r.Name
		}
		return fmt.Sprintf("%s → %s", r.Name, dest)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", r.Name)
	if len(fetch) == 0 {
		b.WriteString("  fetch: (none)\n")
	} else {
		for _, u := range fetch {
			fmt.Fprintf(&b, "  fetch: %s\n", u)
		}
	}
	if len(push) == 0 {
		b.WriteString("  push: (none)\n")
	} else {
		for _, u := range push {
			fmt.Fprintf(&b, "  push: %s\n", u)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func redactGitURLs(urls []string) []string {
	out := make([]string, len(urls))
	for i, u := range urls {
		out[i] = redactGitURL(u)
	}
	return out
}

func parseGitEndpoint(raw string) (host, path string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	raw = strings.TrimSuffix(raw, ".git")
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "file://") || filepath.IsAbs(raw) || strings.Contains(raw, `\`) {
		p := raw
		if strings.HasPrefix(lower, "file://") {
			p = raw[len("file://"):]
			p = strings.TrimPrefix(p, "/")
		}
		return "", filepath.ToSlash(p)
	}
	if !strings.Contains(raw, "://") && strings.Contains(raw, "@") {
		at := strings.Index(raw, "@")
		rest := raw[at+1:]
		colon := strings.Index(rest, ":")
		if colon >= 0 && !strings.Contains(rest[:colon], "/") {
			return strings.ToLower(rest[:colon]), strings.TrimPrefix(rest[colon+1:], "/")
		}
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		p := strings.TrimPrefix(u.Path, "/")
		p = strings.TrimSuffix(p, ".git")
		return strings.ToLower(u.Hostname()), p
	}
	s := raw
	s = strings.TrimPrefix(s, "ssh://")
	s = strings.TrimPrefix(s, "git://")
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	parts := strings.SplitN(s, "/", 2)
	host = strings.ToLower(parts[0])
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	if len(parts) > 1 {
		path = strings.TrimSuffix(parts[1], ".git")
	}
	return host, path
}

func gitDestAllowed(dest string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	dh, dp := parseGitEndpoint(dest)
	dp = strings.TrimSuffix(dp, "/")
	for _, a := range allow {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		ah, ap := parseGitEndpoint(a)
		ap = strings.TrimSuffix(ap, "/")
		if ah == "" && ap != "" {
			if dh == "" && (dp == ap || strings.HasPrefix(strings.ToLower(dp), strings.ToLower(ap)+"/") || strings.EqualFold(dp, ap)) {
				return true
			}
			continue
		}
		if ah != "" && !strings.EqualFold(ah, dh) {
			continue
		}
		if ap == "" {
			return true
		}
		if strings.EqualFold(dp, ap) || strings.HasPrefix(strings.ToLower(dp), strings.ToLower(ap)+"/") {
			return true
		}
	}
	return false
}

func unauthorizedGitDestinations(dests, allow []string) []string {
	var bad []string
	seen := map[string]bool{}
	for _, d := range dests {
		if gitDestAllowed(d, allow) {
			continue
		}
		red := redactGitURL(d)
		if seen[red] {
			continue
		}
		seen[red] = true
		bad = append(bad, red)
	}
	return bad
}

func gitRemoteAllowlist(engine *core.UltraFastEngine) []string {
	if engine == nil || engine.GetConfig() == nil {
		return nil
	}
	return engine.GetConfig().GitRemoteAllow
}

func rejectUnauthorizedRemotes(engine *core.UltraFastEngine, dests []string) *mcp.CallToolResult {
	allow := gitRemoteAllowlist(engine)
	if len(allow) == 0 {
		return nil
	}
	bad := unauthorizedGitDestinations(dests, allow)
	if len(bad) == 0 {
		return nil
	}
	return mcp.NewToolResultError(fmt.Sprintf(
		"git destination not allowed: %s. Name of the remote is not sufficient; effective URL must match --git-remote-allow. force:true does not bypass this policy.",
		strings.Join(bad, ", ")))
}

func gitRemote(_ context.Context, engine *core.UltraFastEngine, repoRoot string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	name, _ := args["remote"].(string)
	var names []string
	if strings.TrimSpace(name) != "" {
		names = []string{name}
	} else {
		var err error
		names, err = listGitRemoteNames(repoRoot)
		if err != nil {
			return mcp.NewToolResultError(redactGitText(fmt.Sprintf("git remote failed: %v", err))), nil
		}
	}
	if len(names) == 0 {
		return mcp.NewToolResultText("No remotes configured"), nil
	}
	compact := engine.IsCompactMode()
	var b strings.Builder
	for i, n := range names {
		resolved, err := resolveGitRemote(repoRoot, n)
		if err != nil {
			return mcp.NewToolResultError(redactGitText(err.Error())), nil
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatResolvedRemote(resolved, compact))
	}
	return mcp.NewToolResultText(b.String()), nil
}
