package core

// project_map_test.go — bounds and safety of the structural walk.
//
// The walk must stay cheap (bounded entries, time and metadata bytes), must
// not leave the allowed root (symlinks), and must not leak hidden paths.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
)

func projectMapEngine(t *testing.T, dir string) *UltraFastEngine {
	t.Helper()
	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := NewUltraFastEngine(&Config{
		Cache:        c,
		AllowedPaths: []string{dir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectMap_DepthLimitsExpansion(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"root.txt":    "x",
		"a/a.txt":     "x",
		"a/b/b.txt":   "x",
		"a/b/c/c.txt": "x",
	})
	engine := projectMapEngine(t, dir)

	shallow, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 1, Detail: "full"})
	if err != nil {
		t.Fatal(err)
	}
	if projectMapHasPath(shallow.Structure, "a/b/c/c.txt") {
		t.Fatalf("depth 1 reached a/b/c/c.txt: %+v", shallow.Structure)
	}
	if !projectMapHasPath(shallow.Structure, "a/a.txt") {
		t.Fatalf("depth 1 lost a/a.txt: %+v", shallow.Structure)
	}

	deep, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 3, Detail: "full"})
	if err != nil {
		t.Fatal(err)
	}
	if !projectMapHasPath(deep.Structure, "a/b/c/c.txt") {
		t.Fatalf("depth 3 lost a/b/c/c.txt: %+v", deep.Structure)
	}
	if projectMapHasPath(deep.Structure, "a/b/c/c/") {
		t.Fatalf("depth 3 expanded one level too deep: %+v", deep.Structure)
	}
	if deep.Limits.MaxDepth != 3 {
		t.Fatalf("limits.max_depth=%d", deep.Limits.MaxDepth)
	}
}

// TestProjectMap_DetailFiltersStructure pins the detail contract: summary is
// the top level, normal is the directory skeleton, full adds the files.
func TestProjectMap_DetailFiltersStructure(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"top.txt":   "x",
		"a/a.txt":   "x",
		"a/b/b.txt": "x",
	})
	engine := projectMapEngine(t, dir)

	summary, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 3, Detail: "summary"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range summary.Structure {
		if e.Type != "dir" || projectMapEntryLevel(e.Path) != 1 {
			t.Fatalf("summary detail kept %+v", e)
		}
	}
	if !projectMapHasPath(summary.Structure, "a/") {
		t.Fatalf("summary detail lost the top level: %+v", summary.Structure)
	}

	normal, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 3, Detail: "normal"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range normal.Structure {
		if e.Type != "dir" {
			t.Fatalf("normal detail kept a file: %+v", e)
		}
	}
	if !projectMapHasPath(normal.Structure, "a/b/") {
		t.Fatalf("normal detail lost a nested directory: %+v", normal.Structure)
	}

	full, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 3, Detail: "full"})
	if err != nil {
		t.Fatal(err)
	}
	if !projectMapHasPath(full.Structure, "a/b/b.txt") {
		t.Fatalf("full detail lost a file: %+v", full.Structure)
	}
}

func TestProjectMap_TruncatesOnMaxEntries(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "x", "b.txt": "x", "c.txt": "x", "d.txt": "x", "e.txt": "x",
	})
	engine := projectMapEngine(t, dir)

	res, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatal("expected truncated=true")
	}
	if res.SummaryScope != ProjectMapSummaryScope {
		t.Fatalf("summary_scope=%q", res.SummaryScope)
	}
	if res.Limits.VisitedEntries > 2 {
		t.Fatalf("visited %d entries with a cap of 2", res.Limits.VisitedEntries)
	}
}

func TestProjectMap_TruncatesOnMaxTime(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "x"})
	engine := projectMapEngine(t, dir)

	res, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{MaxTime: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatal("expected truncated=true")
	}
	if res.SummaryScope != ProjectMapSummaryScope {
		t.Fatalf("summary_scope=%q", res.SummaryScope)
	}
	if res.Limits.MaxTimeMS != 0 {
		t.Fatalf("limits.max_time_ms=%d", res.Limits.MaxTimeMS)
	}
}

func TestProjectMap_CancelledContextStops(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "x"})
	engine := projectMapEngine(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := engine.ProjectMap(ctx, dir, ProjectMapOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatal("expected truncated=true on a cancelled context")
	}
	if res.Limits.VisitedEntries != 0 {
		t.Fatalf("cancelled walk visited %d entries", res.Limits.VisitedEntries)
	}
}

func TestProjectMap_DoesNotFollowSymlinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not permitted in this environment: %v", err)
	}
	engine := projectMapEngine(t, dir)

	res, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Structure {
		if filepath.Base(e.Path) == "secret.txt" || filepath.Base(e.Path) == "link" {
			t.Fatalf("symlink target leaked into the map: %+v", res.Structure)
		}
	}
	for _, f := range res.Summary.KeyFiles {
		if f == "link" {
			t.Fatalf("symlink leaked into key files: %+v", res.Summary.KeyFiles)
		}
	}
}

func TestProjectMap_HiddenPathsAreNotCounted(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.txt":        "x",
		"secrets/key.txt": "hidden-bytes",
	})
	engine := projectMapEngine(t, dir)

	policy, err := ParseFilePolicy([]byte(`{"version":1,"rules":[{"pattern":"**/secrets/**","level":"hidden"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	engine.SetFilePolicy(policy)

	res, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Structure {
		if filepath.Base(e.Path) == "secrets" || filepath.Base(e.Path) == "key.txt" {
			t.Fatalf("hidden path exposed: %+v", res.Structure)
		}
	}
	if res.Summary.VisitedFiles != 1 {
		t.Fatalf("visited_files=%d, want 1 (hidden files must not be counted)", res.Summary.VisitedFiles)
	}
	if len(res.Summary.ByExtension) != 1 {
		t.Fatalf("by_extension=%v, want only .txt", res.Summary.ByExtension)
	}
}

func TestProjectMap_KeyFilesAndExtensions(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod":                   "module x\n",
		"README.md":                "# x\n",
		"Dockerfile":               "FROM scratch\n",
		"app.csproj":               "<Project/>\n",
		".github/workflows/ci.yml": "on: push\n",
		"pkg/a.go":                 "package pkg\n",
		"pkg/b.go":                 "package pkg\n",
	})
	engine := projectMapEngine(t, dir)

	res, err := engine.ProjectMap(context.Background(), dir, ProjectMapOpts{Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"go.mod", "README.md", "Dockerfile", "app.csproj", ".github/workflows/ci.yml"} {
		found := false
		for _, k := range res.Summary.KeyFiles {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("key_files missing %q: %+v", want, res.Summary.KeyFiles)
		}
	}
	if res.Summary.ByExtension[".go"] != 2 {
		t.Fatalf("by_extension=%v", res.Summary.ByExtension)
	}
}

func projectMapHasPath(entries []ProjectMapEntry, path string) bool {
	for _, e := range entries {
		if e.Path == path {
			return true
		}
	}
	return false
}
