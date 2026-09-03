package core

import (
	"path/filepath"
	"testing"
)

func TestSetAllowedPaths_SwapsSandbox(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	engine := newTestEngine(dir1)
	defer engine.Close()

	inside1 := filepath.Join(dir1, "a.txt")
	inside2 := filepath.Join(dir2, "b.txt")
	if !engine.IsPathAllowed(inside1) {
		t.Fatal("dir1 should be allowed before swap")
	}

	engine.SetAllowedPaths([]string{dir2}, AllowedSourceRoots)
	if engine.AllowedSource() != AllowedSourceRoots {
		t.Fatalf("source=%s", engine.AllowedSource())
	}
	if engine.IsPathAllowed(inside1) {
		t.Fatal("dir1 must be denied after replace")
	}
	if !engine.IsPathAllowed(inside2) {
		t.Fatal("dir2 should be allowed after replace")
	}
	listed := engine.ListedAllowedPaths()
	if len(listed) != 1 {
		t.Fatalf("listed=%v", listed)
	}
}
