package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func TestGitPullAndStash(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	parent := t.TempDir()
	origin := filepath.Join(parent, "origin")
	clone := filepath.Join(parent, "clone")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(origin, "init", "-b", "main")
	git(origin, "config", "user.email", "t@example.com")
	git(origin, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(origin, "f.txt"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git(origin, "add", "f.txt")
	git(origin, "commit", "-m", "v1")
	git(parent, "clone", origin, "clone")
	git(clone, "config", "user.email", "t@example.com")
	git(clone, "config", "user.name", "t")

	reg := newGitNetworkRegistry(t, parent)
	denied := callPatchTool(t, newHelpTestRegistry(t, parent), "git", map[string]any{"action": "pull", "path": clone})
	if !denied.IsError || !strings.Contains(resultText(t, denied), "--git-network") {
		t.Fatalf("pull without network: %s", resultText(t, denied))
	}

	if err := os.WriteFile(filepath.Join(origin, "f.txt"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git(origin, "add", "f.txt")
	git(origin, "commit", "-m", "v2")
	res := callPatchTool(t, reg, "git", map[string]any{"action": "pull", "path": clone})
	if res.IsError || !strings.Contains(resultText(t, res), "fast-forward") {
		t.Fatalf("pull: %s", resultText(t, res))
	}
	got, err := os.ReadFile(filepath.Join(clone, "f.txt"))
	if err != nil || strings.TrimSpace(string(got)) != "v2" {
		t.Fatalf("clone file %q err=%v", got, err)
	}

	if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	st := callPatchTool(t, reg, "git", map[string]any{"action": "stash", "stash_action": "push", "path": clone, "message": "wip"})
	if st.IsError {
		t.Fatal(resultText(t, st))
	}
	oid := stashOID(t, resultText(t, st))
	clean, _ := os.ReadFile(filepath.Join(clone, "f.txt"))
	if strings.TrimSpace(string(clean)) != "v2" {
		t.Fatalf("stash did not restore HEAD content: %q", clean)
	}
	apply := callPatchTool(t, reg, "git", map[string]any{"action": "stash", "stash_action": "apply", "path": clone, "rev": oid})
	if apply.IsError {
		t.Fatal(resultText(t, apply))
	}
	restored, _ := os.ReadFile(filepath.Join(clone, "f.txt"))
	if strings.TrimSpace(string(restored)) != "local" {
		t.Fatalf("apply got %q", restored)
	}
	listed := callPatchTool(t, reg, "git", map[string]any{"action": "stash", "stash_action": "list", "path": clone})
	if !strings.Contains(resultText(t, listed), oid[:7]) && !strings.Contains(resultText(t, listed), "wip") {
		t.Fatalf("stash not kept: %s", resultText(t, listed))
	}
}

func stashOID(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "stashed ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "stashed "))
		}
	}
	t.Fatalf("no oid in %q", text)
	return ""
}

func newGitNetworkRegistry(t *testing.T, allowedDir string) *toolRegistry {
	t.Helper()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedDir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	s := server.NewMCPServer("test", "0.0.0")
	reg := &toolRegistry{
		server:     s,
		engine:     engine,
		handlers:   make(map[string]toolHandler),
		profile:    profileUltra,
		gitNetwork: true,
	}
	registerGitTools(reg)
	return reg
}
