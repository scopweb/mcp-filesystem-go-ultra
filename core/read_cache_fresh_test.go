package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
)

func TestReadFileContent_SeesExternalWrite(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	path := filepath.Join(dir, "shared.txt")
	if err := os.WriteFile(path, []byte("from-writer\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-writer\n" {
		t.Fatalf("first read: %q", got)
	}

	if err := os.WriteFile(path, []byte("from-writer-2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err = engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-writer-2\n" {
		t.Fatalf("stale cache after external write: %q", got)
	}
}

func TestReadFileContent_SeesSameSizeRewrite(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	path := filepath.Join(dir, "same-size.txt")
	if err := os.WriteFile(path, []byte("aaaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ReadFileContent(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bbbb"), 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	got, err := engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "bbbb" {
		t.Fatalf("same-size rewrite: %q", got)
	}
}

func TestReadFileContent_SeesMtimeRewind(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	path := filepath.Join(dir, "rewind.txt")
	if err := os.WriteFile(path, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ReadFileContent(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	got, err := engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "body" {
		t.Fatalf("rewind read: %q", got)
	}
}

func TestReadFileContent_DeletedIsErrorNotCachedHit(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	path := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ReadFileContent(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ReadFileContent(context.Background(), path); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestReadFileContent_SeesRenameReplace(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	path := filepath.Join(dir, "replaced.txt")
	if err := os.WriteFile(path, []byte("old-content"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ReadFileContent(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "replaced.txt.tmp")
	if err := os.WriteFile(tmp, []byte("new-content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	got, err := engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "new-content" {
		t.Fatalf("rename replace: %q", got)
	}
}

func TestReadFileContent_InFlightCaptureDoesNotRepopulateAfterInvalidate(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	path := filepath.Join(dir, "race.txt")
	if err := os.WriteFile(path, []byte("version-1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	opened := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	cache.SetCaptureHooksForTest(func(p string) {
		if filepath.Base(p) != "race.txt" {
			return
		}
		once.Do(func() { close(opened) })
		<-proceed
	}, nil)
	t.Cleanup(func() {
		cache.SetCaptureHooksForTest(nil, nil)
		select {
		case <-proceed:
		default:
			close(proceed)
		}
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := engine.ReadFileContent(context.Background(), path)
		errCh <- err
	}()

	<-opened
	if err := os.WriteFile(path, []byte("version-2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	engine.invalidateMutatedPath(path)
	close(proceed)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	got, err := engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "version-2\n" {
		t.Fatalf("cached in-flight capture: %q", got)
	}
}
