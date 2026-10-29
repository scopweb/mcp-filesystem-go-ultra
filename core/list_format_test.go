package core

// The directory listing prints the absolute path once, as a header. Repeating
// it on every entry line doubled the size of a large listing without adding
// any information: the path is also in the structured payload of the tool.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDirectoryContent_PrintsPathOnce(t *testing.T) {
	root := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{root}, AllowedSourceCLI)
	for i := 0; i < 5; i++ {
		name := filepath.Join(root, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := engine.ListDirectoryContent(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(got, root); n != 1 {
		t.Fatalf("path repeated %d times:\n%s", n, got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if lines[0] != root {
		t.Fatalf("first line is not the path header: %q", lines[0])
	}
	for i := 0; i < 5; i++ {
		if !strings.Contains(got, fmt.Sprintf("file%d.txt", i)) {
			t.Fatalf("entry %d missing:\n%s", i, got)
		}
	}
	if !strings.Contains(got, "5 files") {
		t.Fatalf("totals lost:\n%s", got)
	}
}
