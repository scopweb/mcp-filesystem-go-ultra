package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadAnchored_RegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hola"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := openReadAnchored(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 8)
	n, err := f.Read(buf)
	if err != nil || string(buf[:n]) != "hola" {
		t.Fatalf("read %q %v", buf[:n], err)
	}
}

func TestOpenReadAnchored_SymlinkLeafRefused(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	f, err := openReadAnchored(link)
	if err == nil {
		f.Close()
		t.Fatal("symlink leaf must be refused")
	}
	got, rerr := os.ReadFile(secret)
	if rerr != nil || string(got) != "secret" {
		t.Fatalf("outside file changed: %q %v", got, rerr)
	}
}

func TestOpenReadAnchored_SymlinkParentRefused(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "marker.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "flip")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	f, err := openReadAnchored(filepath.Join(link, "marker.txt"))
	if err == nil {
		buf := make([]byte, 16)
		n, _ := f.Read(buf)
		f.Close()
		if string(buf[:n]) == "secret" {
			t.Fatal("read followed a symlink parent")
		}
		t.Fatal("symlink parent must be refused")
	}
}

func TestRenameAnchored_MovesFile(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "dst")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "a.txt")
	if err := os.WriteFile(src, []byte("moved"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstDir, "b.txt")
	if err := renameAnchored(src, dst); err != nil {
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

func TestRenameAnchored_SymlinkParentDoesNotImport(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "important.conf")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "sub")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	dstDir := filepath.Join(dir, "in")
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := renameAnchored(filepath.Join(link, "important.conf"), filepath.Join(dstDir, "stolen.conf"))
	if err == nil {
		t.Fatal("rename through a symlink parent must fail")
	}
	if _, statErr := os.Lstat(filepath.Join(dstDir, "stolen.conf")); !os.IsNotExist(statErr) {
		t.Fatal("outside file was imported")
	}
	got, rerr := os.ReadFile(secret)
	if rerr != nil || string(got) != "secret" {
		t.Fatalf("outside file changed: %q %v", got, rerr)
	}
}

func TestMoveFile_AcrossRootsStillMoves(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{a, b}, AllowedSourceCLI)
	src := filepath.Join(a, "a.txt")
	if err := os.WriteFile(src, []byte("across"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(b, "nested", "b.txt")
	if err := engine.MoveFile(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "across" {
		t.Fatalf("moved %q %v", got, err)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}
}
