package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestWriteFileBytes_SymlinkOutsideRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "kept.txt")
	if err := os.WriteFile(target, []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	err := engine.WriteFileBytes(context.Background(), link, []byte("overwrite"))
	if err == nil {
		t.Fatal("write through an outside symlink must fail")
	}
	got, rerr := os.ReadFile(target)
	if rerr != nil || string(got) != "kept" {
		t.Fatalf("outside file changed: %q %v", got, rerr)
	}
}

func TestWriteFileBytes_InsideRoot(t *testing.T) {
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	path := filepath.Join(root, "nested", "a.txt")
	if err := engine.WriteFileBytes(context.Background(), path, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "ok" {
		t.Fatalf("wrote %q %v", got, err)
	}
}

func TestDeleteFile_InsideRoot(t *testing.T) {
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	file := filepath.Join(root, "gone.txt")
	dir := filepath.Join(root, "sub")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "n.txt"), []byte("n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := engine.DeleteFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if err := engine.DeleteFile(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("file still present: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir still present: %v", err)
	}
}

func TestRemoveWithinRoot_SymlinkOutsideLeavesTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "kept.txt")
	if err := os.WriteFile(target, []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	_ = engine.removeWithinRoot(link, false)
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "kept" {
		t.Fatalf("outside file changed: %q %v", got, err)
	}
}

func TestMoveFile_SameRoot(t *testing.T) {
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("moved"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "nested", "b.txt")
	if err := engine.MoveFile(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "moved" {
		t.Fatalf("moved %q %v", got, err)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}
}

func TestMoveFile_SymlinkSourceLeavesTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "kept.txt")
	if err := os.WriteFile(target, []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	err := engine.MoveFile(context.Background(), link, filepath.Join(root, "gone.txt"))
	if err == nil {
		t.Fatal("move of an outside symlink must fail")
	}
	got, rerr := os.ReadFile(target)
	if rerr != nil || string(got) != "kept" {
		t.Fatalf("outside file changed: %q %v", got, rerr)
	}
}

func TestCopyFile_SameRoot(t *testing.T) {
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("copied"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "nested", "b.txt")
	if err := engine.CopyFile(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "copied" {
		t.Fatalf("copied %q %v", got, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source missing: %v", err)
	}
}

func TestCopyFile_SymlinkSourceLeavesTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "kept.txt")
	if err := os.WriteFile(target, []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	err := engine.CopyFile(context.Background(), link, filepath.Join(root, "gone.txt"))
	if err == nil {
		t.Fatal("copy of an outside symlink must fail")
	}
	got, rerr := os.ReadFile(target)
	if rerr != nil || string(got) != "kept" {
		t.Fatalf("outside file changed: %q %v", got, rerr)
	}
	if _, err := os.Stat(filepath.Join(root, "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("destination was created: %v", err)
	}
}

func TestWriteFileBytes_PreservesFileNameCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows rename changes filename case")
	}
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	path := filepath.Join(root, "SubDir", "NavThMenu.razor")
	if err := engine.WriteFileBytes(context.Background(), path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := engine.WriteFileBytes(context.Background(), path, []byte("y")); err != nil {
		t.Fatal(err)
	}
	got := filepath.Join(root, "SubDir", "NavThMenu.razor")
	entries, err := os.ReadDir(filepath.Join(root, "SubDir"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "NavThMenu.razor" {
		t.Fatalf("case changed: %v", namesOf(entries))
	}
	body, err := os.ReadFile(got)
	if err != nil || string(body) != "y" {
		t.Fatalf("body %q %v", body, err)
	}
}

func namesOf(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func TestCopyDirectory_SameRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	src := filepath.Join(root, "Src")
	if err := os.MkdirAll(filepath.Join(src, "Nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "NavThMenu.razor"), []byte("menu"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Nested", "ITruckerService.cs"), []byte("svc"), 0644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(outside, "kept.txt")
	if err := os.WriteFile(kept, []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kept, filepath.Join(src, "link.txt")); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	dst := filepath.Join(root, "Dst")
	if err := engine.CopyFile(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	menu, err := os.ReadFile(filepath.Join(dst, "NavThMenu.razor"))
	if err != nil || string(menu) != "menu" {
		t.Fatalf("menu %q %v", menu, err)
	}
	svc, err := os.ReadFile(filepath.Join(dst, "Nested", "ITruckerService.cs"))
	if err != nil || string(svc) != "svc" {
		t.Fatalf("svc %q %v", svc, err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("symlink was copied: %v", err)
	}
	got, err := os.ReadFile(kept)
	if err != nil || string(got) != "kept" {
		t.Fatalf("outside file changed: %q %v", got, err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "navthmenu.razor" {
			t.Fatal("copied name was lowercased")
		}
	}
}

func TestCreateDirectory_SameRootPreservesCase(t *testing.T) {
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	path := filepath.Join(root, "SubDir", "NavThMenu")
	if err := engine.CreateDirectory(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "SubDir"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "NavThMenu" || !entries[0].IsDir() {
		t.Fatalf("case changed: %v", namesOf(entries))
	}
}

func TestCreateDirectory_SymlinkParentLeavesOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	err := engine.CreateDirectory(context.Background(), filepath.Join(link, "NewDir"))
	if err == nil {
		t.Fatal("create through an outside symlink must fail")
	}
	if _, statErr := os.Lstat(filepath.Join(outside, "NewDir")); !os.IsNotExist(statErr) {
		t.Fatalf("outside directory was created: %v", statErr)
	}
}

func TestListDirectory_SameRootPreservesCase(t *testing.T) {
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	if err := os.WriteFile(filepath.Join(root, "NavThMenu.razor"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := engine.ListDirectoryContent(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "NavThMenu.razor") {
		t.Fatalf("listing lost case: %s", got)
	}
}

func TestListDirectory_SymlinkOutsideRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("no"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	got, err := engine.ListDirectoryContent(context.Background(), link)
	if err == nil && strings.Contains(got, "secret.txt") {
		t.Fatalf("listed outside names: %s", got)
	}
	if err == nil {
		t.Fatal("listing an outside symlink must fail")
	}
}

func TestSearchFiles_SymlinkOutsideNotRead(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hit.txt"), []byte("inside-token\n"), 0644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret-token\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	out, err := engine.SearchFiles(context.Background(), SearchOptions{
		Path: root, Pattern: "token", IncludeContent: true, NoIgnore: true, MaxResults: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "inside-token") {
		t.Fatalf("missed inside match: %s", out.Text)
	}
	if strings.Contains(out.Text, "secret-token") {
		t.Fatalf("read outside symlink: %s", out.Text)
	}
}

func TestAnalyzeSymbols_SymlinkOutsideNotRead(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.go"), []byte("package p\nfunc InsideFunc() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(secret, []byte("package p\nfunc SecretFunc() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "leak.go")); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	got := AnalyzeSymbols(engine, root, "", 50)
	text := got.Message
	for _, f := range got.Findings {
		text += f.Symbol
	}
	if !strings.Contains(text, "InsideFunc") {
		t.Fatalf("missed inside symbol: %+v", got)
	}
	if strings.Contains(text, "SecretFunc") {
		t.Fatalf("read outside symlink: %+v", got)
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
