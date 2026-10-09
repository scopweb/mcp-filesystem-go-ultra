package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFileTool_DanglingSymlinkDestDoesNotEscape(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	s, _ := newIncidentFixServer(t, allowed)
	src := filepath.Join(allowed, "src.txt")
	if err := os.WriteFile(src, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "copy.txt")
	link := filepath.Join(allowed, "evil")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("symlink not permitted in this environment: %v", err)
	}
	res := callServer(t, s, "copy_file", map[string]any{
		"source_path": src,
		"dest_path":   link,
	})
	if !res.IsError {
		t.Fatalf("copy_file followed a dangling symlink: %s", textFromResult(t, res))
	}
	if _, err := os.Lstat(outsideFile); !os.IsNotExist(err) {
		t.Fatalf("copy_file created %s outside the root: %v", outsideFile, err)
	}
}
