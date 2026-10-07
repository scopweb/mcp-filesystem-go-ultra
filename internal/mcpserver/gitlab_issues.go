package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const gitlabTimeout = 20 * time.Second

type glTarget struct {
	Base    string
	Project string
}

func (t glTarget) api(path string) string {
	return strings.TrimRight(t.Base, "/") + "/api/v4/projects/" + gitlabEncodeProject(t.Project) + path
}

type glSession struct {
	base   string
	token  string
	csrf   string
	client *http.Client
}

type glCredFunc func(ctx context.Context, base string) (user, pass string, err error)

var (
	gitlabSessions sync.Map
	gitlabCreds    glCredFunc = gitCredentialFill
)

func clearGitlabAuth() {
	gitlabSessions = sync.Map{}
}

var gitlabActionParams = map[string]map[string]bool{
	"list":    {"action": true, "path": true, "repo": true, "base_url": true, "state": true, "labels": true, "assignee": true, "limit": true, "page": true},
	"view":    {"action": true, "path": true, "repo": true, "base_url": true, "number": true, "limit": true, "page": true},
	"comment": {"action": true, "path": true, "repo": true, "base_url": true, "number": true, "body": true},
	"close":   {"action": true, "path": true, "repo": true, "base_url": true, "number": true},
	"reopen":  {"action": true, "path": true, "repo": true, "base_url": true, "number": true},
}

func handleGitLabIssues(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) *mcp.CallToolResult {
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	allowed, ok := gitlabActionParams[action]
	if !ok {
		return ghFail(errCodeInvalidParams, fmt.Sprintf("unknown action %q", action),
			`gitlab_issues(action:"view", base_url:"http://host", repo:"group/project", number:1)`,
			false, map[string]any{"field": "action", "expected": "list, view, comment, close, reopen"})
	}
	for k := range args {
		if !allowed[k] {
			return ghFail(errCodeInvalidParams, fmt.Sprintf("parameter %q is not valid for action %q", k, action),
				suggestionValidation, false, map[string]any{"field": k, "action": action})
		}
	}
	target, errRes := resolveGitLabTarget(engine, args)
	if errRes != nil {
		return errRes
	}
	if denied := rejectGitLabHost(engine, target); denied != nil {
		return denied
	}
	switch action {
	case "list":
		return gitlabList(ctx, target, args)
	case "view":
		return gitlabView(ctx, target, args)
	case "comment":
		return gitlabComment(ctx, target, args)
	case "close":
		return gitlabState(ctx, target, args, "close")
	case "reopen":
		return gitlabState(ctx, target, args, "reopen")
	default:
		return ghFail(errCodeInvalidParams, fmt.Sprintf("unknown action %q", action), suggestionValidation, false, map[string]any{"field": "action"})
	}
}

func resolveGitLabTarget(engine *core.UltraFastEngine, args map[string]interface{}) (glTarget, *mcp.CallToolResult) {
	repo, repoSet, errRes := stringField(args, "repo")
	if errRes != nil {
		return glTarget{}, errRes
	}
	baseArg, baseSet, errRes := stringField(args, "base_url")
	if errRes != nil {
		return glTarget{}, errRes
	}
	path, pathSet, errRes := stringField(args, "path")
	if errRes != nil {
		return glTarget{}, errRes
	}
	explicit := repoSet && strings.TrimSpace(repo) != ""
	if pathSet && strings.TrimSpace(path) != "" {
		path = core.NormalizePath(strings.TrimSpace(path))
		if engine == nil || !engine.IsPathAllowed(path) {
			return glTarget{}, notAllowedResult(engine, path)
		}
	} else {
		pathSet = false
	}
	var base string
	if baseSet {
		if strings.TrimSpace(baseArg) == "" {
			return glTarget{}, ghFail(errCodeInvalidParams, `parameter "base_url" is empty; omit it or pass http://host`,
				suggestionValidation, false, map[string]any{"field": "base_url"})
		}
		base, errRes = normalizeGitLabBase(baseArg)
		if errRes != nil {
			return glTarget{}, errRes
		}
	}
	if explicit {
		project, errRes := parseGitLabProject(repo)
		if errRes != nil {
			return glTarget{}, errRes
		}
		if base == "" {
			return glTarget{}, ghFail(errCodeGLUnresolved, `explicit repo requires base_url; this tool does not assume a GitLab host`,
				`gitlab_issues(action:"view", base_url:"http://192.168.0.20", repo:"group/project", number:1)`,
				false, map[string]any{"field": "base_url"})
		}
		return glTarget{Base: base, Project: project}, nil
	}
	if !pathSet {
		return glTarget{}, ghFail(errCodeGLUnresolved, "cannot determine the GitLab project: pass path or repo+base_url",
			`gitlab_issues(action:"list", path:"C:/repo")`, false, map[string]any{"field": "repo"})
	}
	if engine != nil && engine.PolicyEnabled() {
		return glTarget{}, policyResult(core.GitLabIssuesResolveBlock())
	}
	root, err := core.FindGitRoot(path)
	if err != nil {
		return glTarget{}, ghFail(errCodeGLUnresolved, "path is not inside a git repository",
			`pass repo:"group/project" and base_url:"http://host"`, false, map[string]any{"path": path})
	}
	if engine == nil || !engine.IsPathAllowed(root) {
		return glTarget{}, notAllowedResult(engine, root)
	}
	cands, errRes := gitlabRemotes(root)
	if errRes != nil {
		return glTarget{}, errRes
	}
	return selectGitLabRemote(cands, base)
}

func normalizeGitLabBase(raw string) (string, *mcp.CallToolResult) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", ghFail(errCodeInvalidParams, `parameter "base_url" must be an http(s) origin without credentials`,
			`base_url:"http://192.168.0.20"`, false, map[string]any{"field": "base_url"})
	}
	return u.Scheme + "://" + u.Host, nil
}

func parseGitLabProject(raw string) (string, *mcp.CallToolResult) {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	raw = strings.TrimSuffix(raw, ".git")
	if strings.Contains(raw, "://") || strings.Contains(raw, "@") || strings.Contains(raw, `\`) || strings.Contains(raw, "..") {
		return "", ghFail(errCodeInvalidParams, `parameter "repo" must be group/project, not a URL`,
			`repo:"group/project"`, false, map[string]any{"field": "repo"})
	}
	parts := strings.Split(raw, "/")
	if len(parts) < 2 {
		return "", ghFail(errCodeInvalidParams, `parameter "repo" must be group/project`,
			`repo:"group/project"`, false, map[string]any{"field": "repo"})
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, " ?#") {
			return "", ghFail(errCodeInvalidParams, `parameter "repo" has an invalid path segment`,
				suggestionValidation, false, map[string]any{"field": "repo"})
		}
	}
	return raw, nil
}

type glRemote struct {
	Base    string
	Project string
}

func (c glRemote) label() string { return c.Base + "/" + c.Project }

func gitlabRemotes(repoRoot string) ([]glRemote, *mcp.CallToolResult) {
	names, err := listGitRemoteNames(repoRoot)
	if err != nil {
		return nil, ghFail(errCodeGLUnresolved, "cannot list git remotes: "+err.Error(),
			`pass repo and base_url`, false, map[string]any{"path": repoRoot})
	}
	var out []glRemote
	seen := map[string]bool{}
	for _, name := range names {
		resolved, err := resolveGitRemote(repoRoot, name)
		if err != nil {
			continue
		}
		urls := resolved.Fetch
		if len(urls) == 0 {
			urls = resolved.Push
		}
		for _, raw := range urls {
			c, ok := parseGitLabRemote(raw)
			if !ok || seen[c.label()] {
				continue
			}
			seen[c.label()] = true
			out = append(out, c)
		}
	}
	return out, nil
}

func parseGitLabRemote(raw string) (glRemote, bool) {
	raw = strings.TrimSpace(raw)
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		project := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
		if strings.Count(project, "/") < 1 || strings.Contains(project, "..") {
			return glRemote{}, false
		}
		return glRemote{Base: u.Scheme + "://" + u.Host, Project: project}, true
	}
	return glRemote{}, false
}

func selectGitLabRemote(cands []glRemote, base string) (glTarget, *mcp.CallToolResult) {
	var matched []glRemote
	for _, c := range cands {
		if base != "" && !strings.EqualFold(c.Base, base) {
			continue
		}
		matched = append(matched, c)
	}
	if base != "" {
		cands = matched
	}
	switch len(cands) {
	case 1:
		return glTarget{Base: cands[0].Base, Project: cands[0].Project}, nil
	case 0:
		return glTarget{}, ghFail(errCodeGLUnresolved, "no GitLab project could be determined from http(s) remotes; SSH remotes are not assumed to be GitLab",
			`pass repo:"group/project" and base_url:"http://host"`, false, nil)
	default:
		labels := make([]string, len(cands))
		for i, c := range cands {
			labels[i] = c.label()
		}
		return glTarget{}, ghFail(errCodeGLAmbiguous, "more than one GitLab project matches",
			`pass repo and base_url`, false, map[string]any{"candidates": labels})
	}
}

func rejectGitLabHost(engine *core.UltraFastEngine, t glTarget) *mcp.CallToolResult {
	allow := gitRemoteAllowlist(engine)
	if len(allow) == 0 {
		return nil
	}
	dest := t.Base + "/" + t.Project
	if gitDestAllowed(dest, allow) {
		return nil
	}
	return ghFail(errCodeGHHostDenied, "GitLab host is outside --git-remote-allow",
		"Use an allowed host or restart with a matching --git-remote-allow. This tool has no force bypass.",
		false, map[string]any{"base_url": t.Base, "repo": t.Project})
}

func gitlabEncodeProject(project string) string {
	parts := strings.Split(project, "/")
	enc := make([]string, len(parts))
	for i, p := range parts {
		enc[i] = url.PathEscape(p)
	}
	return strings.Join(enc, "%2F")
}

func gitlabList(ctx context.Context, t glTarget, args map[string]interface{}) *mcp.CallToolResult {
	state, errRes := enumField(args, "state", "opened", []string{"opened", "closed", "all", "open"})
	if errRes != nil {
		return errRes
	}
	if state == "open" {
		state = "opened"
	}
	limit, errRes := pageLimit(args, 20)
	if errRes != nil {
		return errRes
	}
	page, errRes := gitlabPage(args)
	if errRes != nil {
		return errRes
	}
	q := url.Values{}
	q.Set("state", state)
	q.Set("per_page", fmt.Sprint(limit))
	q.Set("page", fmt.Sprint(page))
	q.Set("order_by", "updated_at")
	q.Set("sort", "desc")
	if labels, set, errRes := stringArrayField(args, "labels"); errRes != nil {
		return errRes
	} else if set {
		if len(labels) == 0 {
			return ghFail(errCodeInvalidParams, `parameter "labels" is empty; omit it to skip the filter`, suggestionValidation, false, map[string]any{"field": "labels"})
		}
		q.Set("labels", strings.Join(labels, ","))
	}
	if assignee, set, errRes := stringField(args, "assignee"); errRes != nil {
		return errRes
	} else if set {
		if strings.TrimSpace(assignee) == "" {
			return ghFail(errCodeInvalidParams, `parameter "assignee" is empty; omit it to skip the filter`, suggestionValidation, false, map[string]any{"field": "assignee"})
		}
		q.Set("assignee_username", assignee)
	}
	status, hdr, body, errRes := gitlabCall(ctx, t, http.MethodGet, "/issues?"+q.Encode(), nil, false)
	if errRes != nil {
		return errRes
	}
	if fail := gitlabStatus(status, body, false); fail != nil {
		return fail
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ghFail(errCodeGLNetwork, "GitLab returned non-JSON output", "Retry this read.", true, map[string]any{"action": "list"})
	}
	issues := make([]map[string]any, 0, len(raw))
	var lines []string
	for _, item := range raw {
		sum := glSummary(item)
		issues = append(issues, sum)
		lines = append(lines, formatIssueLine(sum))
	}
	next := hdr.Get("X-Next-Page")
	truncated := next != ""
	st := statusOK
	if len(issues) == 0 {
		st = statusEmpty
	} else if truncated {
		st = statusTruncated
	}
	payload := map[string]any{
		"status": st, "action": "list", "repo": t.Project, "base_url": t.Base,
		"state": state, "issues": issues, "count": len(issues), "truncated": truncated,
		"untrusted_content": len(issues) > 0,
	}
	if truncated {
		payload["continuation"] = map[string]any{"page": next, "limit": limit, "hint": "pass page to read the next page; do not repeat this page"}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s | %s | %d", t.Project, state, len(issues))
	if truncated {
		b.WriteString(" | more")
	}
	b.WriteByte('\n')
	if len(lines) > 0 {
		b.WriteString(untrustedBanner + "\n" + strings.Join(lines, "\n") + "\n" + untrustedEnd + "\n")
	}
	return mcp.NewToolResultStructured(payload, b.String())
}

func gitlabView(ctx context.Context, t glTarget, args map[string]interface{}) *mcp.CallToolResult {
	n, errRes := requiredNumber(args)
	if errRes != nil {
		return errRes
	}
	status, _, body, errRes := gitlabCall(ctx, t, http.MethodGet, fmt.Sprintf("/issues/%d", n), nil, false)
	if errRes != nil {
		return errRes
	}
	if fail := gitlabStatus(status, body, false); fail != nil {
		return fail
	}
	var issue map[string]any
	if err := json.Unmarshal(body, &issue); err != nil {
		return ghFail(errCodeGLNetwork, "GitLab returned non-JSON output", "Retry this read.", true, map[string]any{"action": "view"})
	}
	limit, errRes := pageLimit(args, 20)
	if errRes != nil {
		return errRes
	}
	page, errRes := gitlabPage(args)
	if errRes != nil {
		return errRes
	}
	nstatus, nhdr, nbody, errRes := gitlabCall(ctx, t, http.MethodGet, fmt.Sprintf("/issues/%d/notes?per_page=%d&page=%d&sort=asc", n, limit, page), nil, false)
	notes := []map[string]any{}
	truncated := false
	if errRes == nil && nstatus == http.StatusOK {
		var raw []map[string]any
		if json.Unmarshal(nbody, &raw) == nil {
			for _, note := range raw {
				if sys, _ := note["system"].(bool); sys {
					continue
				}
				bodyText, cut := capText(stringFieldAny(note["body"]), 20000)
				author := ""
				if a, ok := note["author"].(map[string]any); ok {
					author, _ = a["username"].(string)
				}
				notes = append(notes, map[string]any{
					"id": note["id"], "author": author, "body": bodyText, "created_at": note["created_at"], "truncated": cut,
				})
			}
		}
		truncated = nhdr.Get("X-Next-Page") != ""
	}
	desc, descCut := capText(stringFieldAny(issue["description"]), 100000)
	payload := map[string]any{
		"status": statusOK, "action": "view", "repo": t.Project, "base_url": t.Base,
		"number": glIID(issue), "title": issue["title"], "body": desc, "body_truncated": descCut,
		"state": issue["state"], "labels": issue["labels"], "url": issue["web_url"],
		"updated_at": issue["updated_at"], "comments": notes, "truncated": truncated || descCut,
		"untrusted_content": true,
	}
	if truncated {
		payload["continuation"] = map[string]any{"page": nhdr.Get("X-Next-Page"), "hint": "pass page to read the next comment page"}
		payload["status"] = statusTruncated
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#%v | %v | %v\n%s\ntitle: %v\n%s\n", payload["number"], payload["state"], payload["url"], untrustedBanner, payload["title"], desc)
	for _, c := range notes {
		fmt.Fprintf(&b, "@%v %v\n%v\n", c["author"], c["created_at"], c["body"])
	}
	b.WriteString(untrustedEnd + "\n")
	return mcp.NewToolResultStructured(payload, b.String())
}

func gitlabComment(ctx context.Context, t glTarget, args map[string]interface{}) *mcp.CallToolResult {
	n, errRes := requiredNumber(args)
	if errRes != nil {
		return errRes
	}
	body, set, errRes := stringField(args, "body")
	if errRes != nil {
		return errRes
	}
	if !set || strings.TrimSpace(body) == "" {
		return ghFail(errCodeInvalidParams, `parameter "body" is required`,
			`gitlab_issues(action:"comment", base_url:"http://host", repo:"group/project", number:1, body:"note")`,
			false, map[string]any{"field": "body"})
	}
	if errRes = boundText("body", body, 65536); errRes != nil {
		return errRes
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	status, _, resp, errRes := gitlabCall(ctx, t, http.MethodPost, fmt.Sprintf("/issues/%d/notes", n), payload, true)
	if errRes != nil {
		return errRes
	}
	if fail := gitlabStatus(status, resp, true); fail != nil {
		return fail
	}
	var note map[string]any
	_ = json.Unmarshal(resp, &note)
	out := map[string]any{
		"status": statusApplied, "action": "comment", "repo": t.Project, "base_url": t.Base,
		"number": n, "note_id": note["id"], "url": t.Base + "/" + t.Project + "/-/issues/" + fmt.Sprint(n),
		"message": fmt.Sprintf("commented #%d", n),
	}
	return mcp.NewToolResultStructured(out, fmt.Sprintf("commented #%d %s\n", n, out["url"]))
}

func gitlabState(ctx context.Context, t glTarget, args map[string]interface{}, event string) *mcp.CallToolResult {
	n, errRes := requiredNumber(args)
	if errRes != nil {
		return errRes
	}
	payload, _ := json.Marshal(map[string]string{"state_event": event})
	status, _, resp, errRes := gitlabCall(ctx, t, http.MethodPut, fmt.Sprintf("/issues/%d", n), payload, true)
	if errRes != nil {
		return errRes
	}
	if fail := gitlabStatus(status, resp, true); fail != nil {
		return fail
	}
	var issue map[string]any
	_ = json.Unmarshal(resp, &issue)
	urlStr := stringFieldAny(issue["web_url"])
	if urlStr == "" {
		urlStr = t.Base + "/" + t.Project + "/-/issues/" + fmt.Sprint(n)
	}
	verb := "closed"
	if event == "reopen" {
		verb = "reopened"
	}
	out := map[string]any{
		"status": statusApplied, "action": event, "repo": t.Project, "base_url": t.Base,
		"number": n, "state": issue["state"], "url": urlStr, "message": fmt.Sprintf("%s #%d", verb, n),
	}
	return mcp.NewToolResultStructured(out, fmt.Sprintf("%s #%d %s\n", verb, n, urlStr))
}

func gitlabPage(args map[string]interface{}) (int, *mcp.CallToolResult) {
	if _, ok := args["page"]; !ok {
		return 1, nil
	}
	n, _, errRes := numberField(args, "page")
	return n, errRes
}

func glSummary(item map[string]any) map[string]any {
	labels, _ := item["labels"].([]any)
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		if s, ok := l.(string); ok {
			names = append(names, s)
		}
	}
	var who []string
	if arr, ok := item["assignees"].([]any); ok {
		for _, a := range arr {
			if m, ok := a.(map[string]any); ok {
				if u, _ := m["username"].(string); u != "" {
					who = append(who, u)
				}
			}
		}
	}
	return map[string]any{
		"number": glIID(item), "title": item["title"], "state": item["state"],
		"labels": names, "assignees": who, "updated_at": item["updated_at"], "url": item["web_url"],
	}
}

func glIID(item map[string]any) int {
	switch n := item["iid"].(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return jsonInt(item["iid"])
	}
}

func stringFieldAny(v any) string {
	s, _ := v.(string)
	return s
}

type glCallResult struct {
	status int
	header http.Header
	body   []byte
}

func gitlabCall(ctx context.Context, t glTarget, method, rel string, payload []byte, write bool) (int, http.Header, []byte, *mcp.CallToolResult) {
	var last []byte
	var lastStatus int
	var lastHdr http.Header
	for attempt := 0; attempt < 2; attempt++ {
		sess, errRes := gitlabSessionFor(ctx, t.Base)
		if errRes != nil {
			return 0, nil, nil, errRes
		}
		status, hdr, body, err := sess.do(ctx, method, t.api(rel), payload)
		if err != nil {
			sug := "Retry this read. Do not retry a write that may have been applied."
			if write {
				sug = "Network error. The write may have been applied. Check the issue before retrying. Do not retry automatically."
			}
			return 0, nil, nil, ghFail(errCodeGLNetwork, clip(redactSensitive(err.Error()), 300), sug, !write, map[string]any{"action": method})
		}
		last, lastStatus, lastHdr = body, status, hdr
		if status == http.StatusUnauthorized && attempt == 0 {
			gitlabSessions.Delete(t.Base)
			continue
		}
		return status, hdr, body, nil
	}
	return lastStatus, lastHdr, last, nil
}

func gitlabStatus(status int, body []byte, write bool) *mcp.CallToolResult {
	if status >= 200 && status < 300 {
		return nil
	}
	msg := gitlabMessage(body)
	if msg == "" {
		msg = fmt.Sprintf("GitLab HTTP %d", status)
	}
	switch status {
	case http.StatusUnauthorized:
		sug := "Store a git credential for this host or set GITLAB_TOKEN with api scope. This tool does not print the secret and will not retry a write."
		if write {
			sug = "Reads can work with a session cookie; writes need X-CSRF-Token or GITLAB_TOKEN with api scope. Do not retry this write."
		}
		return ghFail(errCodeGLAuth, "GitLab authentication failed", sug, false, map[string]any{"status": status})
	case http.StatusForbidden:
		return ghFail(errCodeGLForbidden, clip(msg, 300), "The account lacks permission for this project or action.", false, map[string]any{"status": status})
	case http.StatusNotFound:
		return ghFail(errCodeGLNotFound, clip(msg, 300), "Check base_url, repo, and the issue number.", false, map[string]any{"status": status})
	case http.StatusTooManyRequests:
		sug := "Wait and retry this read. Do not retry a write."
		if write {
			sug = "Rate limited. The write may have been applied. Do not retry automatically."
		}
		return ghFail(errCodeGHRateLimit, "GitLab rate limit exceeded", sug, !write, map[string]any{"status": status})
	default:
		sug := "Do not retry a write that may have been applied."
		if !write {
			sug = "Retry this read only after correcting the error."
		}
		return ghFail(errCodeGLNetwork, clip(msg, 300), sug, !write && status >= 500, map[string]any{"status": status})
	}
}

func gitlabMessage(body []byte) string {
	var obj struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &obj) == nil {
		switch m := obj.Message.(type) {
		case string:
			if m != "" {
				return redactSensitive(m)
			}
		}
		if obj.Error != "" {
			return redactSensitive(obj.Error)
		}
	}
	return ""
}

func gitlabSessionFor(ctx context.Context, base string) (*glSession, *mcp.CallToolResult) {
	if v, ok := gitlabSessions.Load(base); ok {
		return v.(*glSession), nil
	}
	sess, errRes := gitlabLogin(ctx, base)
	if errRes != nil {
		return nil, errRes
	}
	actual, _ := gitlabSessions.LoadOrStore(base, sess)
	return actual.(*glSession), nil
}

func gitlabLogin(ctx context.Context, base string) (*glSession, *mcp.CallToolResult) {
	if tok := strings.TrimSpace(os.Getenv("GITLAB_TOKEN")); tok != "" {
		return &glSession{base: base, token: tok, client: gitlabHTTPClient(nil)}, nil
	}
	if tok := strings.TrimSpace(os.Getenv("GITLAB_PRIVATE_TOKEN")); tok != "" {
		return &glSession{base: base, token: tok, client: gitlabHTTPClient(nil)}, nil
	}
	user, pass, err := gitlabCreds(ctx, base)
	if err != nil || pass == "" {
		return nil, ghFail(errCodeGLAuth, "no GitLab credential for this host",
			"Store a git credential for the host or set GITLAB_TOKEN. Do not paste the token into the tool arguments.",
			false, map[string]any{"base_url": base})
	}
	if looksLikeGitLabToken(pass) {
		return &glSession{base: base, token: pass, client: gitlabHTTPClient(nil)}, nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, ghFail(errCodeGLAuth, "cannot start a GitLab session", suggestionValidation, false, nil)
	}
	client := gitlabHTTPClient(jar)
	csrf, err := gitlabFormLogin(ctx, client, base, user, pass)
	if err != nil {
		return nil, ghFail(errCodeGLAuth, "GitLab sign-in failed",
			"Check the stored git credential for this host. The password is not included in this error.",
			false, map[string]any{"base_url": base})
	}
	return &glSession{base: base, csrf: csrf, client: client}, nil
}

func looksLikeGitLabToken(pass string) bool {
	p := strings.ToLower(strings.TrimSpace(pass))
	for _, prefix := range []string{"glpat-", "gldt-", "glptt-", "glrt-", "glsoat-"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func gitlabHTTPClient(jar http.CookieJar) *http.Client {
	return &http.Client{Timeout: gitlabTimeout, Jar: jar}
}

func extractGitLabCSRF(page string) string {
	markers := []string{
		`name="csrf-token" content="`,
		`name='csrf-token' content='`,
		`name="authenticity_token" value="`,
		`name='authenticity_token' value='`,
	}
	for _, m := range markers {
		i := strings.Index(page, m)
		if i < 0 {
			continue
		}
		rest := page[i+len(m):]
		q := m[len(m)-1:]
		end := strings.Index(rest, q)
		if end > 0 {
			return rest[:end]
		}
	}
	return ""
}

func gitlabFormLogin(ctx context.Context, client *http.Client, base, user, pass string) (string, error) {
	loginURL := strings.TrimRight(base, "/") + "/users/sign_in"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	page, err := readLimited(resp.Body, 1<<20)
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	token := extractGitLabCSRF(page)
	if token == "" {
		return "", fmt.Errorf("gitlab login page has no csrf token")
	}
	form := url.Values{}
	form.Set("user[login]", user)
	form.Set("user[password]", pass)
	form.Set("authenticity_token", token)
	form.Set("user[remember_me]", "0")
	post, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = client.Do(post)
	if err != nil {
		return "", err
	}
	body, err := readLimited(resp.Body, 1<<20)
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	if strings.Contains(body, "Invalid login") || strings.Contains(body, "Invalid Login or password") {
		return "", fmt.Errorf("invalid gitlab login")
	}
	csrf := extractGitLabCSRF(body)
	if csrf == "" {
		home, herr := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/", nil)
		if herr == nil {
			if hresp, herr := client.Do(home); herr == nil {
				hbody, _ := readLimited(hresp.Body, 1<<20)
				hresp.Body.Close()
				csrf = extractGitLabCSRF(hbody)
			}
		}
	}
	if csrf == "" {
		csrf = token
	}
	return csrf, nil
}

func (s *glSession) do(ctx context.Context, method, rawURL string, payload []byte) (int, http.Header, []byte, error) {
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return 0, nil, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if s.token != "" {
		req.Header.Set("PRIVATE-TOKEN", s.token)
	} else if s.csrf != "" {
		origin := strings.TrimRight(s.base, "/")
		req.Header.Set("X-CSRF-Token", s.csrf)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Referer", origin+"/")
		req.Header.Set("Origin", origin)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, 1<<20)
	if err != nil {
		return resp.StatusCode, resp.Header, nil, err
	}
	return resp.StatusCode, resp.Header, []byte(body), nil
}

func readLimited(r io.Reader, max int64) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > max {
		return "", fmt.Errorf("gitlab response exceeded %d bytes", max)
	}
	return string(b), nil
}

func gitCredentialFill(ctx context.Context, base string) (string, string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "protocol=%s\nhost=%s\n", u.Scheme, u.Hostname())
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		fmt.Fprintf(&b, "port=%s\n", p)
	}
	b.WriteString("\n")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "credential", "fill")
	cmd.Stdin = strings.NewReader(b.String())
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	var user, pass string
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "username":
			user = v
		case "password":
			pass = v
		}
	}
	return user, pass, nil
}
