package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestEditFile_SetsNewHash verifies the post-edit content_hash (new point 1)
// equals the hash of the file actually on disk — i.e. a caller can feed it back
// as expected_hash on the next edit without re-reading.
func TestEditFile_SetsNewHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha beta gamma\n"), 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)

	res, err := engine.EditFile(context.Background(), path, "beta", "BETA", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewHash == "" {
		t.Fatal("NewHash was not set")
	}
	raw, _ := os.ReadFile(path)
	if want := contentHashFNV(string(raw)); res.NewHash != want {
		t.Errorf("NewHash = %s, want %s (hash of file on disk)", res.NewHash, want)
	}
}

func TestDeleteLineRange_SetsNewHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)

	_, res, err := engine.DeleteLineRange(context.Background(), path, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if want := contentHashFNV(string(raw)); res.NewHash != want {
		t.Errorf("NewHash = %s, want %s (hash of file on disk)", res.NewHash, want)
	}
}
