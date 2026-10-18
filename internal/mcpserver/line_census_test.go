package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDirectory_LinesCensusSkipsBin(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Auth/A.cs", "one\ntwo\n")
	write("bin/Skip.cs", "nope\n")
	write("notes.txt", "keep\n")
	res := callNamed(t, reg, context.Background(), "list_directory", map[string]interface{}{
		"path": dir, "output_format": "lines", "include": ".cs",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "2 ") || !strings.Contains(text, "A.cs") {
		t.Fatalf("missing A.cs count:\n%s", text)
	}
	if strings.Contains(text, "Skip.cs") {
		t.Fatalf("bin should be skipped:\n%s", text)
	}
	if strings.Contains(text, "notes.txt") {
		t.Fatalf("include should drop txt:\n%s", text)
	}
}

func TestSandboxMountHint(t *testing.T) {
	if sandboxMountHint(`$HOME/mnt/Fabricacio/FabricacioAPI`) == "" {
		t.Fatal("sandbox mount must be named")
	}
	if sandboxMountHint(`/home/user/mnt/Fabricacio/x`) == "" {
		t.Fatal("home mnt must be named")
	}
	if sandboxMountHint(`/mnt/c/Users/x`) != "" {
		t.Fatal("WSL drive mount is not a sandbox")
	}
	if sandboxMountHint(`C:\__REPOS\jotajotape\FABRICACIO`) != "" {
		t.Fatal("Windows path is not a sandbox")
	}
}
