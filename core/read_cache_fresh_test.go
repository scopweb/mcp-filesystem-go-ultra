package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
