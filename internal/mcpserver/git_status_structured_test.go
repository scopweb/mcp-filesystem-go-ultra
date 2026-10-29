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
