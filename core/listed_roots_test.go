package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListedAllowedPaths_DedupesDisplayOnly(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)
	defer engine.Close()

	folded := dir
	if runtime.GOOS == "windows" {
		folded = swapDriveCase(dir)
	}
	engine.SetAllowedPaths([]string{dir, dir + string(os.PathSeparator), filepath.ToSlash(dir), folded}, AllowedSourceCLI)
	listed := engine.ListedAllowedPaths()
	if len(listed) != 1 {
		t.Fatalf("equivalent roots listed %d times: %v", len(listed), listed)
	}
	if !engine.IsPathAllowed(filepath.Join(dir, "a.txt")) {
		t.Fatal("display dedupe changed authorization")
	}

	engine.SetAllowedPaths([]string{dir, child}, AllowedSourceCLI)
	listed = engine.ListedAllowedPaths()
	if len(listed) != 2 {
		t.Fatalf("nested roots must stay distinct: %v", listed)
	}
	if !engine.IsPathAllowed(filepath.Join(child, "b.txt")) || !engine.IsPathAllowed(filepath.Join(dir, "a.txt")) {
		t.Fatal("nested roots lost authorization")
	}
}

func TestListedAllowedPaths_DedupesSymlink(t *testing.T) {
	dir := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	engine := newTestEngine(dir)
	defer engine.Close()
	engine.SetAllowedPaths([]string{dir, link}, AllowedSourceCLI)
	listed := engine.ListedAllowedPaths()
	if len(listed) != 1 {
		t.Fatalf("symlink and target listed separately: %v", listed)
	}
	if !engine.IsPathAllowed(filepath.Join(dir, "a.txt")) {
		t.Fatal("symlink dedupe changed authorization")
	}
}

func swapDriveCase(p string) string {
	if len(p) >= 2 && p[1] == ':' {
		d := p[:1]
		if strings.ToLower(d) == d {
			return strings.ToUpper(d) + p[1:]
		}
		return strings.ToLower(d) + p[1:]
	}
	return strings.ToLower(p)
}
