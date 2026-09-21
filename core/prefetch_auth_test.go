package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
)

func TestPrefetch_EngineRejectsSecretAndOutside(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewUltraFastEngine(&Config{
		Cache:        c,
		AllowedPaths: []string{dir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })

	main := filepath.Join(dir, "main.go")
	secret := filepath.Join(dir, ".env")
	okFile := filepath.Join(dir, "ok.go")
	if err := os.WriteFile(main, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("SECRET=1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(okFile, []byte("package ok"), 0644); err != nil {
		t.Fatal(err)
	}

	outsideFile := filepath.Join(outside, "leak.go")
	if err := os.WriteFile(outsideFile, []byte("package leak"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.go")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Log("symlink skipped:", err)
	}

	for i := 0; i < 3; i++ {
		c.TrackAccess(main)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, hit := c.PeekFileFresh(okFile); hit {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, hit := c.GetFileFresh(secret); hit {
		t.Fatal(".env must not be prefetched")
	}
	if _, err := os.Lstat(link); err == nil {
		if _, hit := c.GetFileFresh(link); hit {
			t.Fatal("symlink outside roots must not be prefetched")
		}
	}
}
