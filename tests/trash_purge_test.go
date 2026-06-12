package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestBackupPurgeTrash verifies that PurgeTrash removes entries older than
// the cutoff, and that dry_run doesn't actually delete anything.
func TestBackupPurgeTrash(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "old.txt")
	if err := os.WriteFile(srcFile, []byte("old content"), 0644); err != nil {
		t.Fatal(err)
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
	info, err := engine.SoftDeleteFile(ctx, srcFile)
	if err != nil {
		t.Fatalf("SoftDeleteFile failed: %v", err)
	}

	// Backdate the metadata.json's timestamp field so the entry looks >7 days
	// old. (PurgeTrash filters by the JSON timestamp, not the file's mtime.)
	metaPath := filepath.Join(backupDir, "filesdelete", info.SDID, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta core.SoftDeleteInfo
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	meta.Timestamp = time.Now().AddDate(0, 0, -30) // 30 days ago
	back, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(metaPath, back, 0600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	// Dry run should report 1, not delete
	deleted, freed, err := engine.GetBackupManager().PurgeTrash(7, true)
	if err != nil {
		t.Fatalf("PurgeTrash dry-run failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("dry-run deleted = %d, want 1", deleted)
	}
	if freed == 0 {
		t.Error("dry-run freed = 0, want > 0")
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("dry-run deleted the file: %v", err)
	}

	// Real run should actually delete
	deleted, _, err = engine.GetBackupManager().PurgeTrash(7, false)
	if err != nil {
		t.Fatalf("PurgeTrash failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("real-run deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Errorf("real-run did not delete the file: %v", err)
	}
	trashDir := filepath.Join(backupDir, "filesdelete", info.SDID)
	if _, err := os.Stat(trashDir); !os.IsNotExist(err) {
		t.Errorf("real-run did not remove trash subdir: %v", err)
	}
}

// TestBackupPurgeTrashRespectsCutoff verifies that entries newer than the
// cutoff are NOT purged.
func TestBackupPurgeTrashRespectsCutoff(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "recent.txt")
	if err := os.WriteFile(srcFile, []byte("recent"), 0644); err != nil {
		t.Fatal(err)
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
	info, err := engine.SoftDeleteFile(ctx, srcFile)
	if err != nil {
		t.Fatalf("SoftDeleteFile failed: %v", err)
	}

	// metadata.json was written just now — purge with 7-day cutoff must NOT delete it
	deleted, _, err := engine.GetBackupManager().PurgeTrash(7, false)
	if err != nil {
		t.Fatalf("PurgeTrash failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("purged %d recent entries; want 0", deleted)
	}
	metaPath := filepath.Join(backupDir, "filesdelete", info.SDID, "metadata.json")
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("recent entry was incorrectly purged: %v", err)
	}
}
