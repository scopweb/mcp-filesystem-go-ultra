package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEncodedDriveRoot_AllowsChild(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter URI")
	}
	dir := t.TempDir()
	slash := filepath.ToSlash(dir)
	uri := "file:///" + strings.Replace(slash, ":", "%3A", 1)
	decoded, err := FileURIToPath(uri)
	if err != nil {
		t.Fatal(err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{decoded}, AllowedSourceRoots)
	child := filepath.Join(dir, "inside.txt")
	if err := os.WriteFile(child, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !engine.IsPathAllowed(child) {
		t.Fatalf("child of %s not allowed", uri)
	}
}

func TestUNCInsideAllowlist(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC")
	}
	if err := ValidatePathSecurity(`\\server\share\file.txt`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	vol := filepath.VolumeName(dir)
	if len(vol) < 2 || vol[1] != ':' {
		t.Skip("temp dir is not a drive path")
	}
	rest := strings.TrimPrefix(dir, vol)
	uncRoot := `\\localhost\` + vol[:1] + `$` + rest
	if _, err := os.Stat(uncRoot); err != nil {
		t.Skipf("admin share unavailable: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{uncRoot}, AllowedSourceCLI)
	child := filepath.Join(uncRoot, "unc.txt")
	if err := os.WriteFile(child, []byte("unc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !engine.IsPathAllowed(child) {
		t.Fatalf("UNC child %s denied", child)
	}
}

func TestSearchFiles_DoesNotSplitOnSeparator(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	dir := engine.config.AllowedPaths[0]
	nested := filepath.Join(dir, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(nested, "note.txt")
	if err := os.WriteFile(target, []byte("needle"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := engine.SearchFiles(context.Background(), SearchOptions{
		Path:           dir,
		Pattern:        "needle",
		IncludeContent: true,
		MaxResults:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.MatchCount != 1 || len(out.Matches) != 1 {
		t.Fatalf("matches=%d text=%s", out.MatchCount, out.Text)
	}
	got := out.Matches[0].File
	if got != target && filepath.Clean(got) != filepath.Clean(target) {
		t.Fatalf("path split or rewritten: %q", got)
	}
	if strings.Count(got, string(os.PathSeparator)) < 2 {
		t.Fatalf("separator missing from result path %q", got)
	}
}

func TestRealpathENOENT_DoesNotDenyOrPanic(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	dir := engine.config.AllowedPaths[0]
	missing := filepath.Join(dir, "no-such-file.txt")
	if engine.IsPathAllowed(missing) == false {
		t.Fatal("missing file under an allowed dir was denied")
	}
	resolved, _, err := ResolveSymlinks(missing)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(resolved) != filepath.Clean(missing) && !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(missing)) {
		t.Fatalf("resolved %q", resolved)
	}
}
