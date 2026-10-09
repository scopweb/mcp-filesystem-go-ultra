package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStatWithinRoot_ReadsInsideAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	inside := filepath.Join(root, "a.txt")
	if err := os.WriteFile(inside, []byte("in"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "out.txt"), []byte("out"), 0644); err != nil {
		t.Fatal(err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)

	info, err := engine.statWithinRoot(inside)
	if err != nil || info.Size() != 2 {
		t.Fatalf("inside stat: %v size=%v", err, info)
	}
	if _, err := engine.statWithinRoot(filepath.Join(outside, "out.txt")); err == nil {
		t.Fatal("stat outside the root must fail")
	}
	if _, err := engine.statWithinRoot(filepath.Join(root, "..", filepath.Base(outside), "out.txt")); err == nil {
		t.Fatal("dotdot escape must fail")
	}
}

func TestStatWithinRoot_SymlinkOutsideRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	if _, err := engine.statWithinRoot(link); err == nil {
		t.Fatal("os.Root must not stat a symlink that leaves the root")
	}
}

func TestReadFileBytes_SymlinkOutsideRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	data, err := engine.ReadFileBytes(context.Background(), link)
	if err == nil || string(data) == "secret" {
		t.Fatalf("read escaped: %q %v", data, err)
	}
}

func TestCloseRoots_DropsHandlesOnAllowlistChange(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{first}, AllowedSourceCLI)
	if _, err := engine.statWithinRoot(filepath.Join(first, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if len(engine.rootHandles) == 0 {
		t.Fatal("expected an open root handle")
	}
	engine.SetAllowedPaths([]string{second}, AllowedSourceCLI)
	if len(engine.rootHandles) != 0 {
		t.Fatal("allowlist change must close root handles")
	}
}
