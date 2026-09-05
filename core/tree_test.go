package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDirectoryTree_RespectsGitignore(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.txt\nskipme/\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "ok.go"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(root, "secret.txt"), []byte("nope"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "skipme"), 0755)
	_ = os.WriteFile(filepath.Join(root, "skipme", "x.go"), []byte("x"), 0644)

	engine := newTestEngine(root)
	defer engine.Close()

	out, err := engine.ListDirectoryTreeOpts(context.Background(), root, TreeOpts{
		MaxDepth: 3, MaxNodes: 50, RespectIgnore: true, Format: "compact",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret.txt") {
		t.Fatalf("gitignore should hide secret.txt:\n%s", out)
	}
	if strings.Contains(out, "skipme") {
		t.Fatalf("gitignore should hide skipme/:\n%s", out)
	}
	if !strings.Contains(out, "ok.go") {
		t.Fatalf("ok.go missing:\n%s", out)
	}
}

func TestListDirectoryTree_NoIgnoreShowsSecret(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.txt\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "secret.txt"), []byte("nope"), 0644)

	engine := newTestEngine(root)
	defer engine.Close()

	out, err := engine.ListDirectoryTreeOpts(context.Background(), root, TreeOpts{
		MaxDepth: 2, MaxNodes: 50, RespectIgnore: false, Format: "compact",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "secret.txt") {
		t.Fatalf("no-ignore should show secret.txt:\n%s", out)
	}
}

func TestListDirectoryTree_Exclude(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(root, "drop.min.js"), []byte("x"), 0644)

	engine := newTestEngine(root)
	defer engine.Close()

	out, err := engine.ListDirectoryTreeOpts(context.Background(), root, TreeOpts{
		MaxDepth: 2, MaxNodes: 50, Exclude: []string{"*.min.js"}, Format: "compact",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "drop.min.js") {
		t.Fatalf("exclude failed:\n%s", out)
	}
	if !strings.Contains(out, "keep.go") {
		t.Fatalf("keep.go missing:\n%s", out)
	}
}
