package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealUsageGitLabCreate(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "test-token")
	useGLCreds(t, "", "")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/issues") || r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["title"] != "New issue" || body["description"] != "Details" || body["labels"] != "bug,test" {
			t.Errorf("payload: %#v", body)
		}
		w.WriteHeader(201)
		fmt.Fprint(w, `{"iid":29,"web_url":"http://gitlab/issue/29"}`)
	}))
	defer srv.Close()
	args := map[string]any{"action": "create", "repo": "group/project", "base_url": srv.URL, "title": "New issue", "body": "Details", "labels": []any{"bug", "test"}}
	res := callGL(t, t.TempDir(), args)
	if res.IsError || calls != 1 {
		t.Fatalf("calls=%d %s", calls, issueText(t, res))
	}
	for _, title := range []string{"", "   ", strings.Repeat("x", 256)} {
		args["title"] = title
		res = callGL(t, t.TempDir(), args)
		if !res.IsError || calls != 1 {
			t.Fatalf("invalid title made a request: %s", issueText(t, res))
		}
	}
	args["title"] = "New issue"
	reg := newHelpTestRegistry(t, t.TempDir())
	reg.engine.GetConfig().ReadOnly = true
	res = callNamed(t, reg, context.Background(), "gitlab_issues", args)
	if !res.IsError || calls != 1 {
		t.Fatalf("readonly: %s calls=%d", issueText(t, res), calls)
	}
}
func TestRealUsageGitLabComments(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "test-token")
	useGLCreds(t, "", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/notes") {
			// https://docs.gitlab.com/api/notes/#list-all-issue-notes
			if r.URL.Query().Get("activity_filter") != "only_comments" {
				w.Header().Set("X-Next-Page", "2")
				fmt.Fprint(w, `[{"id":1,"system":true}]`)
				return
			}
			var limit, page int
			fmt.Sscan(r.URL.Query().Get("per_page"), &limit)
			fmt.Sscan(r.URL.Query().Get("page"), &page)
			if limit <= 0 || page <= 0 || r.URL.Query().Get("sort") != "asc" {
				t.Errorf("query: %s", r.URL.RawQuery)
				w.WriteHeader(400)
				return
			}
			start, end := (page-1)*limit, min(page*limit, 4)
			if end < 4 {
				w.Header().Set("X-Next-Page", fmt.Sprint(page+1))
			}
			notes := []map[string]any{}
			for i := start; i < end; i++ {
				notes = append(notes, map[string]any{"id": i + 1, "body": fmt.Sprintf("comment %d", i+1), "system": false})
			}
			json.NewEncoder(w).Encode(notes)
			return
		}
		fmt.Fprint(w, `{"iid":28,"title":"Issue","description":"","labels":[],"updated_at":"","state":"opened","web_url":"http://gitlab/28"}`)
	}))
	defer srv.Close()
	for _, limit := range []int{1, 3, 100} {
		args := map[string]any{"action": "view", "repo": "group/project", "base_url": srv.URL, "number": float64(28), "limit": float64(limit)}
		seen := 0
		for seen < 4 {
			res := callGL(t, t.TempDir(), args)
			if res.IsError {
				t.Fatal(issueText(t, res))
			}
			payload := res.StructuredContent.(map[string]any)
			notes := payload["comments"].([]map[string]any)
			if len(notes) != min(limit, 4-seen) {
				t.Fatalf("limit=%d seen=%d notes=%v", limit, seen, notes)
			}
			for _, note := range notes {
				seen++
				if note["body"] != fmt.Sprintf("comment %d", seen) {
					t.Fatalf("order: %#v", notes)
				}
			}
			if seen < 4 {
				next := payload["continuation"].(map[string]any)
				if next["limit"] != limit {
					t.Fatalf("continuation: %#v", next)
				}
				args["page"] = float64(next["page"].(int))
			} else if payload["truncated"] != false {
				t.Fatalf("last page: %#v", payload)
			}
		}
	}
}

func TestRealUsageSearchContext(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("before\nBUR8\nafter\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	for _, explicit := range []bool{false, true} {
		args := map[string]any{"path": dir, "pattern": "BUR8", "include_content": true, "context_lines": float64(1), "output_format": "text"}
		if explicit {
			args["include_context"] = true
		}
		res := callNamed(t, reg, context.Background(), "search_files", args)
		data, _ := json.Marshal(res.StructuredContent)
		if res.IsError || !strings.Contains(string(data), "before") || !strings.Contains(string(data), "after") || !strings.Contains(resultText(t, res), "before") {
			t.Fatalf("explicit=%v: %s\n%s", explicit, data, resultText(t, res))
		}
	}
}

func TestRealUsageCommitPaths(t *testing.T) {
	dir := t.TempDir()
	if _, err := execGitCommandCtx(context.Background(), dir, "git", "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("unstaged"), 0600); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res := callNamed(t, reg, context.Background(), "git", map[string]any{"action": "commit", "path": dir, "message": "test", "paths": []any{"a.txt"}})
	if !res.IsError || !strings.Contains(resultText(t, res), "paths is not supported for commit") {
		t.Fatal(resultText(t, res))
	}
	staged, err := execGitCommandCtx(context.Background(), dir, "git", "diff", "--cached", "--name-only")
	if err != nil || strings.TrimSpace(staged) != "" {
		t.Fatalf("index changed: %s %v", staged, err)
	}
}

func TestRealUsageJSONBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.json")
	data := `{"comandes":{"order":{"items":[{"name":"` + strings.Repeat("x", 5<<20) + `"}]}}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	args := map[string]any{"path": path, "query": "values(comandes)[].items[?contains(name, 'x') && contains(name, 'x') && contains(name, 'x') && contains(name, 'x')]"}
	res := callNamed(t, reg, context.Background(), "json_query", args)
	if !res.IsError || !strings.Contains(resultText(t, res), "67108864") {
		t.Fatalf("missing limit: %s", resultText(t, res))
	}
	args["budget_multiplier"] = float64(2)
	args["query"] = args["query"].(string) + " | length(@)"
	res = callNamed(t, reg, context.Background(), "json_query", args)
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	for _, invalid := range []float64{0, 5, 1.5} {
		args["budget_multiplier"] = invalid
		res = callNamed(t, reg, context.Background(), "json_query", args)
		if !res.IsError {
			t.Fatalf("accepted budget_multiplier=%v", invalid)
		}
	}
}

func TestRealUsageEditMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("actual line\n", 30)), 0600); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	callNamed(t, reg, context.Background(), "read_file", map[string]any{"path": path})
	res := callNamed(t, reg, context.Background(), "edit_file", map[string]any{"path": path, "old_text": "missing text", "new_text": "replacement"})
	if !res.IsError || strings.Contains(resultText(t, res), "ALWAYS read") {
		t.Fatal(resultText(t, res))
	}
}
