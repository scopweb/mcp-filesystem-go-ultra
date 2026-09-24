package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitAdd_UpdateStagesTrackedOnly(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	engine := newGitTestEngine(t, dir)
	defer engine.Close()
	tracked := filepath.Join(dir, "tracked.txt")
	writeFile(t, tracked, "v1\n")
	commitAll(t, dir, "init")
	writeFile(t, tracked, "v2\n")
	if err := os.MkdirAll(filepath.Join(dir, ".agent"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".agent", "scratch.txt"), "nope\n")

	res, err := gitAdd(context.Background(), engine, dir, map[string]interface{}{
		"action": "add",
		"update": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s", firstText(res))
	}
	out := mustGit(t, dir, "status", "--porcelain")
	if !strings.Contains(out, "M  tracked.txt") && !strings.Contains(out, "M tracked.txt") {
		t.Fatalf("tracked change not staged:\n%s", out)
	}
	if strings.Contains(out, ".agent") && !strings.Contains(out, "??") {
		t.Fatalf("untracked .agent was staged:\n%s", out)
	}
	if strings.Contains(out, "A ") && strings.Contains(out, ".agent") {
		t.Fatalf("untracked .agent was staged:\n%s", out)
	}
}
