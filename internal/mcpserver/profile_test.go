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
	p, err = parseToolProfile("agent")
	if err != nil || p != profileAgent {
		t.Fatalf("agent: %q %v", p, err)
	}
}

func TestProfile_AgentIsStrictPlusGit(t *testing.T) {
	dir := t.TempDir()
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileAgent})
	got := map[string]bool{}
	for name := range reg.server.ListTools() {
		got[name] = true
	}
	if len(got) != len(strictToolSet)+1 {
		t.Fatalf("agent registered %d tools, want %d", len(got), len(strictToolSet)+1)
	}
	if !got["git"] {
		t.Fatal("agent profile must register git")
	}
	for _, banned := range []string{"context_pack", "github_issues", "analyze_code", "wsl", "workspace"} {
		if got[banned] {
			t.Errorf("agent must not register %q", banned)
		}
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
		"git", "github_issues", "gitlab_issues", "wsl", "minify_js", "project_replace", "batch_operations",
		"analyze_operation", "copy_file", "server_info",
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
	if n != 34 {
		t.Fatalf("ultra registered %d tools, want 34", n)
	}
	for _, name := range []string{"git", "github_issues", "gitlab_issues", "minify_js", "backup", "help", "apply_patch", "workspace", "json_query", "file_bytes", "file_hash", "project_map"} {
		if _, ok := reg.server.ListTools()[name]; !ok {
			t.Errorf("ultra missing %q", name)
		}
	}
}

// TestProfile_StrictInventoryIsExplicit locks the strict catalog. The
// inventory is a deliberate contract: project_map (v4.8.1) was ADDED to the
// 17-tool core (18 now) — nothing was dropped to make room for it, and the
// 17 core tools stay frozen.
func TestProfile_StrictInventoryIsExplicit(t *testing.T) {
	want := []string{
		"list_allowed_directories", "project_map", "directory_tree", "list_directory",
		"search_files", "get_file_info", "read_file", "write_file", "edit_file",
		"multi_edit", "apply_patch", "diff_files", "create_directory", "move_file",
		"delete_file", "backup", "help", "security_policy",
	}
	if len(strictToolSet) != len(want) {
		t.Fatalf("strict inventory has %d tools, want %d", len(strictToolSet), len(want))
	}
	for _, name := range want {
		if _, ok := strictToolSet[name]; !ok {
			t.Errorf("strict inventory missing %q", name)
		}
	}
	dir := t.TempDir()
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})
	for _, name := range want {
		if _, ok := reg.server.ListTools()[name]; !ok {
			t.Errorf("strict profile did not register %q", name)
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
