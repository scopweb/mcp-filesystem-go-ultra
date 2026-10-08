package mcpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGraduatedSchema_SecurityPolicy(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	res := callToolSweep(t, reg, "security_policy", map[string]any{})
	payload := structuredPayload(t, res)
	assertPayloadConforms(t, parseSchema(t, securityPolicyOutputSchema), payload)
	if payload["agent_can_modify"] != false || payload["precedence"] != "most_restrictive" {
		t.Fatalf("%#v", payload)
	}
	if textBlock(t, res) == "" || !strings.Contains(textBlock(t, res), `"enabled"`) {
		t.Fatalf("text fallback must stay JSON: %s", textBlock(t, res))
	}
}

func TestGraduatedSchema_GitHubIssues(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	schema := parseSchema(t, githubIssuesOutputSchema)
	useGH(t, func(inv ghInvocation) ghRunResult {
		stdin := string(inv.Stdin)
		switch {
		case strings.Contains(stdin, "issues(first:"):
			return okGH(listJSON(issueNode(12, "Bug")))
		case strings.Contains(strings.Join(inv.Args, " "), "graphql"):
			return okGH(`{"data":{"repository":{"issue":{"number":12,"title":"Bug","body":"hello","state":"OPEN","url":"https://github.com/acme/widgets/issues/12","updatedAt":"2026-10-01T00:00:00Z","labels":{"nodes":[{"name":"bug"}]},"assignees":{"nodes":[{"login":"octocat"}]},"comments":{"pageInfo":{"hasNextPage":false},"nodes":[]}},"pullRequest":null}}}`)
		default:
			return okGH(`{"number":12,"html_url":"https://github.com/acme/widgets/issues/12"}`)
		}
	})
	cases := []struct {
		name string
		args map[string]any
	}{
		{"list", map[string]any{"action": "list", "repo": "acme/widgets"}},
		{"view", map[string]any{"action": "view", "repo": "acme/widgets", "number": float64(12)}},
		{"create", map[string]any{"action": "create", "repo": "acme/widgets", "title": "Bug"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callToolSweep(t, reg, "github_issues", tc.args)
			assertPayloadConforms(t, schema, structuredPayload(t, res))
		})
	}
}

func TestGraduatedSchema_GitLabIssues(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	schema := parseSchema(t, gitlabIssuesOutputSchema)
	clearGitlabAuth()
	t.Cleanup(clearGitlabAuth)
	t.Setenv("GITLAB_TOKEN", "glpat-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues/") && strings.Contains(r.URL.Path, "/notes"):
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues/"):
			_, _ = io.WriteString(w, `{"iid":7,"title":"Bug","description":"hello","state":"opened","web_url":"http://gitlab.example/group/project/-/issues/7","labels":["bug"],"updated_at":"2026-10-01T00:00:00Z"}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues"):
			_, _ = io.WriteString(w, `[{"iid":7,"title":"Bug","state":"opened","web_url":"http://gitlab.example/group/project/-/issues/7","labels":["bug"]}]`)
		case r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{"id":3}`)
		case r.Method == http.MethodPut:
			_, _ = io.WriteString(w, `{"iid":7,"state":"closed","web_url":"http://gitlab.example/group/project/-/issues/7"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	base := map[string]any{"base_url": srv.URL, "repo": "group/project"}
	cases := []struct {
		name string
		args map[string]any
	}{
		{"list", mergeArgs(base, map[string]any{"action": "list"})},
		{"view", mergeArgs(base, map[string]any{"action": "view", "number": float64(7)})},
		{"comment", mergeArgs(base, map[string]any{"action": "comment", "number": float64(7), "body": "note"})},
		{"close", mergeArgs(base, map[string]any{"action": "close", "number": float64(7)})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callToolSweep(t, reg, "gitlab_issues", tc.args)
			assertPayloadConforms(t, schema, structuredPayload(t, res))
		})
	}
}

func mergeArgs(base, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestGraduatedSchema_JSONValid(t *testing.T) {
	for _, raw := range []json.RawMessage{securityPolicyOutputSchema, githubIssuesOutputSchema, gitlabIssuesOutputSchema} {
		parseSchema(t, raw)
	}
}

func TestContextPack_ExperimentalHasNoSchema(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	st, ok := reg.server.ListTools()["context_pack"]
	if !ok {
		t.Fatal("context_pack not registered")
	}
	if st.Tool.RawOutputSchema != nil || st.Tool.OutputSchema.Type != "" {
		t.Fatal("experimental context_pack must not declare an outputSchema")
	}
	if !strings.Contains(st.Tool.Description, "[EXPERIMENTAL since v4.8.0") {
		t.Fatalf("description=%s", st.Tool.Description)
	}
}
