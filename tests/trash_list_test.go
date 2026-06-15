package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestBackupListTrash verifies that ListTrash enumerates all soft-deleted
// files and respects the filter_path parameter.
func TestBackupListTrash(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()

	// Create three files in two different subdirs
	files := map[string]string{
		filepath.Join(allowedDir, "alpha", "a.txt"): "alpha-a",
		filepath.Join(allowedDir, "alpha", "b.txt"): "alpha-b",
		filepath.Join(allowedDir, "beta", "c.txt"):  "beta-c",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cacheSystem, err := cache.NewIntelligentCache(10 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer cacheSystem.Close()

	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheSystem,
		ParallelOps:  2,
		AllowedPaths: []string{allowedDir},
		BackupDir:    backupDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()
	for path := range files {
		if _, err := engine.SoftDeleteFile(ctx, path); err != nil {
			t.Fatalf("SoftDeleteFile(%s) failed: %v", path, err)
		}
	}

	// List all — expect 3
	all, err := engine.GetBackupManager().ListTrash(0, "", 0)
	if err != nil {
		t.Fatalf("ListTrash failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListTrash returned %d entries; want 3", len(all))
	}
	for _, e := range all {
		if e.SDID == "" {
			t.Error("entry has empty SDID")
		}
		if e.OriginalPath == "" {
			t.Error("entry has empty OriginalPath")
		}
	}

	// Filter by "alpha" — expect 2
	alpha, err := engine.GetBackupManager().ListTrash(0, "alpha", 0)
	if err != nil {
		t.Fatalf("ListTrash(alpha) failed: %v", err)
	}
	if len(alpha) != 2 {
		t.Errorf("ListTrash(alpha) returned %d; want 2", len(alpha))
	}
	for _, e := range alpha {
		if !strings.Contains(strings.ToLower(e.OriginalPath), "alpha") {
			t.Errorf("filter leaked: %s", e.OriginalPath)
		}
	}

	// Filter by "c.txt" — expect 1
	cOnly, err := engine.GetBackupManager().ListTrash(0, "c.txt", 0)
	if err != nil {
		t.Fatalf("ListTrash(c.txt) failed: %v", err)
	}
	if len(cOnly) != 1 {
		t.Errorf("ListTrash(c.txt) returned %d; want 1", len(cOnly))
	}
	if cOnly[0].OriginalPath != filepath.Join(allowedDir, "beta", "c.txt") {
		t.Errorf("got %s, want beta/c.txt", cOnly[0].OriginalPath)
	}

	// Limit — expect at most 2
	limited, err := engine.GetBackupManager().ListTrash(2, "", 0)
	if err != nil {
		t.Fatalf("ListTrash(limit=2) failed: %v", err)
	}
	if len(limited) > 2 {
		t.Errorf("ListTrash(limit=2) returned %d; want <= 2", len(limited))
	}
}
