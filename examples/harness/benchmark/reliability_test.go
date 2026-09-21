package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchPaths(t *testing.T) {
	got := matchPaths([]any{
		map[string]any{"path": "a.txt", "line": 1},
		map[string]any{"path": "b.txt"},
		"skip",
	})
	if len(got) != 2 || got[0] != "a.txt" || got[1] != "b.txt" {
		t.Fatalf("%v", got)
	}
}

func TestSeedReliability(t *testing.T) {
	dir := t.TempDir()
	if err := seedReliability(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rel-search", "hit-30.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rel-pr", "a.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
}
