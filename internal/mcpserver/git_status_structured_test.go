package mcpserver

// The git status result carries the state of the repository in
// structuredContent (branch, upstream, ahead/behind and the change
// counters) so a client does not have to parse the porcelain header. The
// changed files stay in the text block, in the porcelain listing, so the same
// file is never sent twice.

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestGitStatus_TruncationPreservesCounters(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	for _, name := range []string{"aaa.txt", "bbb.txt", "zzz.txt"} {
		if err := os.WriteFile(dir+"/"+name, []byte("hi\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, compact := range []bool{false, true} {
		engine.GetConfig().CompactMode = compact
		for _, output := range []string{"name-only", "full"} {
			for _, limit := range []float64{1, 100} {
				res, err := gitStatus(context.Background(), engine, dir, map[string]interface{}{
					"action": "status", "output": output, "max_lines": limit,
				})
				if err != nil || res.IsError {
					t.Fatalf("status failed: %v %#v", err, res)
				}
				m := res.StructuredContent.(map[string]any)
				truncated := limit == 1
				if m["truncated"] != truncated || m["untracked"] != 3 || m["file_count"] != 3 {
					t.Fatalf("output=%s limit=%v compact=%v: %#v", output, limit, compact, m)
				}
				text := mcpText(t, res)
				if strings.Contains(text, "[TRUNCADO:") != truncated || strings.Contains(text, "zzz.txt") == truncated {
					t.Fatalf("bound not respected: %s", text)
				}
				if truncated && m["continuation"] == nil {
					t.Fatalf("missing recovery hint: %#v", m)
				}
			}
		}
	}
}

func TestGitStatus_AllUnmergedCodesCountAsConflicts(t *testing.T) {
	for _, code := range []string{"DD", "AU", "UD", "UA", "DU", "AA", "UU", " M", "A ", "??"} {
		t.Run(code, func(t *testing.T) {
			m := gitStatusPayload("repo", "## main\n"+code+" file.txt\n")
			want := 1
			if code == " M" || code == "A " || code == "??" {
				want = 0
			}
			if m["conflicts"] != want || m["file_count"] != 1 {
				t.Fatalf("wrong conflict count for %q: %#v", code, m)
			}
		})
	}
}
func TestGitStatus_StructuredPayload(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/dirty.txt", []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := gitStatus(context.Background(), engine, dir, map[string]interface{}{"action": "status"})
	if err != nil {
		t.Fatalf("gitStatus: %v", err)
	}
	if res.IsError {
		t.Fatal(mcpText(t, res))
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("no structured payload: %#v", res.StructuredContent)
	}
	if m["action"] != "status" {
		t.Fatalf("action: %#v", m)
	}
	if b, _ := m["branch"].(string); b == "" {
		t.Fatalf("branch: %#v", m)
	}
	if m["untracked"] != 1 {
		t.Fatalf("untracked: %#v", m)
	}
	if m["file_count"] != 1 {
		t.Fatalf("file_count: %#v", m)
	}
	if s, _ := m["summary"].(string); !strings.Contains(s, "?1") {
		t.Fatalf("summary: %#v", m)
	}
	// The file list stays in the text block: it must not be duplicated.
	if _, duplicated := m["files"]; duplicated {
		t.Fatalf("files duplicated in structuredContent: %#v", m)
	}
	if !strings.Contains(mcpText(t, res), "dirty.txt") {
		t.Fatalf("changed file missing from the text block: %s", mcpText(t, res))
	}
}

func TestGitStatus_FullStartsWithReadableSummary(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	if err := os.WriteFile(dir+"/dirty.txt", []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := gitStatus(context.Background(), engine, dir, map[string]interface{}{"action": "status", "output": "full"})
	if err != nil {
		t.Fatalf("gitStatus: %v", err)
	}
	text := mcpText(t, res)
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "|") {
		t.Fatalf("full status does not start with a readable summary: %s", text)
	}
	// The porcelain body follows the summary: nothing is lost.
	if !strings.Contains(text, "branch.head") {
		t.Fatalf("full status lost the porcelain body: %s", text)
	}
}
