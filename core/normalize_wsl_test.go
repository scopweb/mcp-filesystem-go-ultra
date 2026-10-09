package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizePath_WindowsDriveFollowsWSL(t *testing.T) {
	in := `C:\temp\fsu\a.txt`
	got := NormalizePath(in)
	wsl, _ := DetectEnvironment()
	if wsl {
		if got != "/mnt/c/temp/fsu/a.txt" {
			t.Fatalf("WSL normalize = %q", got)
		}
		return
	}
	if strings.HasPrefix(got, "/mnt/") {
		t.Fatalf("native host must not invent /mnt: %q", got)
	}
}

func TestAllowedPath_SameFormAsToolPath(t *testing.T) {
	dir := t.TempDir()
	engine, err := NewUltraFastEngine(&Config{
		AllowedPaths: []string{dir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	target := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !engine.IsPathAllowed(NormalizePath(target)) {
		t.Fatalf("normalized tool path denied: %s", NormalizePath(target))
	}
}
