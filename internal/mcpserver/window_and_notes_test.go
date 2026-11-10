package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A truncated git output must be continuable: the agent asks for the lines it
// did not see instead of re-reading everything.
func TestGitOutputWindow(t *testing.T) {
	text := "l1\nl2\nl3\nl4\nl5"

	got := gitOutputWindow(text, map[string]interface{}{"start_line": float64(3)}, 10)
	if !strings.Contains(got, "l3") || !strings.Contains(got, "l5") {
		t.Fatalf("window missing lines: %q", got)
	}
	if strings.Contains(got, "l1") || strings.Contains(got, "l2") {
		t.Fatalf("window kept earlier lines: %q", got)
	}
	if !strings.Contains(got, "[lines 3-5 of 5]") {
		t.Fatalf("window footer: %q", got)
	}

	// The window and max_lines compose: the window selects, max_lines cuts.
	got = gitOutputWindow(text, map[string]interface{}{"start_line": float64(2)}, 2)
	if !strings.Contains(got, "[lines 2-3 of 5]") || !strings.Contains(got, "To continue: start_line=4") {
		t.Fatalf("cut footer: %q", got)
	}

	// Without a window the behavior is the plain max_lines truncation.
	got = gitOutputWindow(text, map[string]interface{}{}, 2)
	if !strings.Contains(got, "TRUNCADO") {
		t.Fatalf("no window must keep the truncation footer: %q", got)
	}

	// A window past the end says so instead of returning nothing.
	got = gitOutputWindow(text, map[string]interface{}{"start_line": float64(9)}, 10)
	if !strings.Contains(got, "no lines in range") {
		t.Fatalf("out of range: %q", got)
	}
}

// End to end: git show with start_line returns the requested window of the
// commit body and says how to continue.
func TestRealUsageGitShowStartLine(t *testing.T) {
	dir := t.TempDir()
	if _, err := execGitCommandCtx(context.Background(), dir, "git", "init"); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&body, "line%02d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-m", "v1")

	reg := newHelpTestRegistry(t, dir)
	res := callNamed(t, reg, context.Background(), "git", map[string]any{"action": "show", "path": dir, "rev": "HEAD", "output": "full"})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "+line01") {
		t.Fatalf("full show: %s", resultText(t, res))
	}

	res = callNamed(t, reg, context.Background(), "git", map[string]any{"action": "show", "path": dir, "rev": "HEAD", "output": "full", "start_line": float64(30)})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	window := resultText(t, res)
	if strings.Contains(window, "+line01") {
		t.Fatalf("window kept the first lines: %s", window)
	}
	if !strings.Contains(window, "[lines 30-") {
		t.Fatalf("window footer: %s", window)
	}
	if !strings.Contains(window, "+line24") {
		t.Fatalf("window body: %s", window)
	}
}

// The notes action reads the comments of an issue on their own, without
// pulling the issue body on every page.
func TestRealUsageGitLabNotes(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "test-token")
	useGLCreds(t, "", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/notes") {
			t.Errorf("notes action must not fetch the issue: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("activity_filter") != "only_comments" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "[{\"id\":1,\"body\":\"first\",\"system\":false,\"author\":{\"username\":\"ada\"},\"created_at\":\"2026-01-01T00:00:00Z\"}]")
	}))
	defer srv.Close()

	args := map[string]any{"action": "notes", "repo": "group/project", "base_url": srv.URL, "number": float64(28)}
	res := callGL(t, t.TempDir(), args)
	if res.IsError {
		t.Fatal(issueText(t, res))
	}
	payload := res.StructuredContent.(map[string]any)
	if payload["action"] != "notes" {
		t.Fatalf("action: %#v", payload)
	}
	notes, ok := payload["comments"].([]map[string]any)
	if !ok || len(notes) != 1 {
		t.Fatalf("notes: %#v", payload["comments"])
	}
	if notes[0]["body"] != "first" || notes[0]["author"] != "ada" {
		t.Fatalf("note: %#v", notes[0])
	}
	if !strings.Contains(issueText(t, res), "first") {
		t.Fatalf("text: %s", issueText(t, res))
	}

	// Reading comments is a read: it must work under --readonly.
	reg := newHelpTestRegistry(t, t.TempDir())
	reg.engine.GetConfig().ReadOnly = true
	res = callNamed(t, reg, context.Background(), "gitlab_issues", args)
	if res.IsError {
		t.Fatalf("readonly must allow notes: %s", issueText(t, res))
	}
}
