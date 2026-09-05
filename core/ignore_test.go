package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnoreMatcher_GitignoreBasics(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\nnode_modules/\n!keep.log\n.env\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "node_modules"), 0755)
	_ = os.WriteFile(filepath.Join(root, "a.log"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(root, "keep.log"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(root, ".env"), []byte("s"), 0644)
	_ = os.WriteFile(filepath.Join(root, "ok.go"), []byte("x"), 0644)

	m := NewIgnoreMatcher()
	if !m.Match(filepath.Join(root, "a.log"), false) {
		t.Fatal("*.log should ignore a.log")
	}
	if m.Match(filepath.Join(root, "keep.log"), false) {
		t.Fatal("!keep.log should not ignore keep.log")
	}
	if !m.Match(filepath.Join(root, "node_modules"), true) {
		t.Fatal("node_modules/ should ignore the directory")
	}
	if !m.Match(filepath.Join(root, ".env"), false) {
		t.Fatal(".env should be ignored")
	}
	if m.Match(filepath.Join(root, "ok.go"), false) {
		t.Fatal("ok.go should not be ignored")
	}
}

func TestIgnoreMatcher_Cursorignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".cursorignore"), []byte("secret.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewIgnoreMatcher()
	if !m.Match(filepath.Join(root, "secret.txt"), false) {
		t.Fatal(".cursorignore should apply")
	}
}

func TestIgnoreMatcher_Nested(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.tmp\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewIgnoreMatcher()
	if !m.Match(filepath.Join(sub, "x.tmp"), false) {
		t.Fatal("parent gitignore should apply in subdir")
	}
}

func TestParseIgnoreLine_SkipComments(t *testing.T) {
	if parseIgnoreLine("# foo") != nil {
		t.Fatal("comment")
	}
	if parseIgnoreLine("") != nil {
		t.Fatal("empty")
	}
	if parseIgnoreLine("*.a") == nil {
		t.Fatal("pattern")
	}
}
