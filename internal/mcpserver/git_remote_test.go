package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func TestRedactGitURL_HidesCredentials(t *testing.T) {
	in := "https://user:s3cret@github.com/org/repo.git"
	out := redactGitURL(in)
	if strings.Contains(out, "s3cret") || strings.Contains(out, "user:s3cret") {
		t.Fatalf("credential leaked: %s", out)
	}
	if !strings.Contains(out, "github.com/org/repo.git") {
		t.Fatalf("host lost: %s", out)
	}
}

func TestGitDestAllowed_HostNamespaceRepo(t *testing.T) {
	if !gitDestAllowed("https://github.com/org/repo.git", []string{"github.com"}) {
		t.Fatal("host allow")
	}
	if !gitDestAllowed("https://github.com/org/repo.git", []string{"github.com/org"}) {
		t.Fatal("namespace allow")
	}
	if gitDestAllowed("https://github.com/other/repo.git", []string{"github.com/org"}) {
		t.Fatal("other org should reject")
	}
	if gitDestAllowed("https://evil.example/org/repo.git", []string{"github.com"}) {
		t.Fatal("host mismatch")
	}
	if !gitDestAllowed("https://github.com/org/repo.git", nil) {
		t.Fatal("empty allowlist permits")
	}
}

func TestApplyInsteadOf_LongestPrefix(t *testing.T) {
	rules := []insteadOfRule{
		{Base: "https://github.com/", InsteadOf: "git@github.com:"},
		{Base: "https://github.com/org/", InsteadOf: "git@github.com:org/"},
	}
	got := applyInsteadOf("git@github.com:org/repo.git", rules)
	if got != "https://github.com/org/repo.git" {
		t.Fatalf("got %s", got)
	}
}

func TestGitRemote_ListsEffectiveURLWithoutNetwork(t *testing.T) {
	parent := t.TempDir()
	bare := filepath.Join(parent, "bare.git")
	local := filepath.Join(parent, "local")
	if err := os.Mkdir(bare, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(local, 0755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, bare, "init", "--bare", "-q")
	initGitRepo(t, local)
	writeFile(t, filepath.Join(local, "f.txt"), "v1\n")
	commitAll(t, local, "init")
	url := filepath.ToSlash(bare)
	mustGit(t, local, "remote", "add", "origin", url)

	engine := newGitTestEngine(t, parent)
	defer engine.Close()
	res, err := gitRemote(context.Background(), engine, local, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	text := mcpText(t, res)
	if res.IsError {
		t.Fatalf("remote failed: %s", text)
	}
	norm := strings.ReplaceAll(text, "\\", "/")
	if !strings.Contains(text, "origin") || !strings.Contains(norm, strings.ReplaceAll(url, "\\", "/")) {
		t.Fatalf("expected origin URL in %q", text)
	}
}

func TestGitPush_CompactShowsDestination(t *testing.T) {
	parent := t.TempDir()
	bare := filepath.Join(parent, "bare.git")
	local := filepath.Join(parent, "local")
	os.Mkdir(bare, 0755)
	os.Mkdir(local, 0755)
	mustGit(t, bare, "init", "--bare", "-q")
	initGitRepo(t, local)
	writeFile(t, filepath.Join(local, "f.txt"), "v1\n")
	commitAll(t, local, "init")
	url := filepath.ToSlash(bare)
	mustGit(t, local, "remote", "add", "origin", url)

	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        c,
		AllowedPaths: []string{parent},
		ParallelOps:  2,
		CompactMode:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	res, err := gitPush(context.Background(), engine, local, map[string]interface{}{"name": "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	text := mcpText(t, res)
	if res.IsError {
		t.Fatalf("push failed: %s", text)
	}
	norm := strings.ReplaceAll(text, "\\", "/")
	want := strings.ReplaceAll(url, "\\", "/")
	if !strings.Contains(text, "origin →") && !strings.Contains(norm, want) {
		t.Fatalf("compact push missing destination: %q", text)
	}
}

func TestGitPush_AllowlistRejectsBeforeNetwork(t *testing.T) {
	parent := t.TempDir()
	bare := filepath.Join(parent, "bare.git")
	local := filepath.Join(parent, "local")
	os.Mkdir(bare, 0755)
	os.Mkdir(local, 0755)
	mustGit(t, bare, "init", "--bare", "-q")
	initGitRepo(t, local)
	writeFile(t, filepath.Join(local, "f.txt"), "v1\n")
	commitAll(t, local, "init")
	mustGit(t, local, "remote", "add", "origin", filepath.ToSlash(bare))

	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:          c,
		AllowedPaths:   []string{parent},
		ParallelOps:    2,
		GitRemoteAllow: []string{"github.com/allowed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	res, err := gitPush(context.Background(), engine, local, map[string]interface{}{"force": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected allowlist rejection")
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "not allowed") {
		t.Fatalf("got %s", text)
	}
}

func TestGitRemoteAction_NoGitNetwork(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	mustGit(t, dir, "remote", "add", "origin", "https://user:s3cret@github.com/org/repo.git")
	h := newRegisteredGitHandlerOpts(engine, false)
	res := callRegisteredGit(t, h, map[string]interface{}{"action": "remote", "path": dir})
	text := mcpText(t, res)
	if res.IsError {
		t.Fatalf("remote without --git-network should work: %s", text)
	}
	if strings.Contains(text, "s3cret") {
		t.Fatalf("credential leaked: %s", text)
	}
	if !strings.Contains(text, "github.com/org/repo") {
		t.Fatalf("expected dest: %s", text)
	}
}
