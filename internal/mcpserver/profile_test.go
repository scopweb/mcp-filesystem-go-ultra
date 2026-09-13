package mcpserver

import (
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func TestParseToolProfile(t *testing.T) {
	p, err := parseToolProfile("")
	if err != nil || p != profileUltra {
		t.Fatalf("empty: %q %v", p, err)
	}
	p, err = parseToolProfile("ultra")
	if err != nil || p != profileUltra {
		t.Fatalf("ultra: %q %v", p, err)
	}
	p, err = parseToolProfile("STRICT")
	if err != nil || p != profileStrict {
		t.Fatalf("STRICT: %q %v", p, err)
	}
	if _, err := parseToolProfile("full"); err == nil {
		t.Fatal("want error for unknown profile")
	}
}

func TestProfile_StrictToolSetExact(t *testing.T) {
	dir := t.TempDir()
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})
	got := map[string]bool{}
	for name := range reg.server.ListTools() {
		got[name] = true
	}
	if len(got) != len(strictToolSet) {
		t.Fatalf("strict registered %d tools, want %d: %v", len(got), len(strictToolSet), keys(got))
	}
	for name := range strictToolSet {
		if !got[name] {
			t.Errorf("strict missing %q", name)
		}
	}
	for name := range got {
		if _, ok := strictToolSet[name]; !ok {
			t.Errorf("strict extra %q", name)
		}
	}
	for _, banned := range []string{
		"git", "wsl", "minify_js", "project_replace", "batch_operations",
		"backup", "analyze_operation", "copy_file", "server_info",
	} {
		if got[banned] {
			t.Errorf("strict must not register %q", banned)
		}
	}
}

func TestProfile_UltraHasFullCatalog(t *testing.T) {
	dir := t.TempDir()
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileUltra})
	n := len(reg.server.ListTools())
	if n != 24 {
		t.Fatalf("ultra registered %d tools, want 24", n)
	}
	for _, name := range []string{"git", "minify_js", "backup", "help", "apply_patch"} {
		if _, ok := reg.server.ListTools()[name]; !ok {
			t.Errorf("ultra missing %q", name)
		}
	}
}

func TestProfile_StrictOutputSchemaSweepSubset(t *testing.T) {
	dir := t.TempDir()
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})
	withSchema := 0
	for name, st := range reg.server.ListTools() {
		if st.Tool.RawOutputSchema != nil || st.Tool.OutputSchema.Type != "" {
			withSchema++
			_ = name
		}
	}
	if withSchema < 8 {
		t.Fatalf("strict profile should keep schema-declared core tools, got %d", withSchema)
	}
}

func newProfileRegistry(t *testing.T, allowedDir string, opts registerOpts) *toolRegistry {
	t.Helper()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedDir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	s := server.NewMCPServer("test", "0.0.0")
	if err := registerToolsOpts(s, engine, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	return &toolRegistry{server: s, engine: engine, handlers: map[string]toolHandler{}, profile: opts.Profile, gitNetwork: opts.GitNetwork}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
