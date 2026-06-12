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

// TestSoftDeleteReturnsSoftDeleteInfo verifies that the new SoftDeleteFile
// signature returns a populated *SoftDeleteInfo so the caller can format a
// response with a restore command.
func TestSoftDeleteReturnsSoftDeleteInfo(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "doc.go")
	if err := os.WriteFile(srcFile, []byte("package x\n"), 0644); err != nil {
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

	// All fields the response formatter needs must be populated
	if info.SDID == "" {
		t.Error("SDID is empty")
	}
	if !strings.HasPrefix(info.SDID, "sd-") {
		t.Errorf("SDID %q missing 'sd-' prefix", info.SDID)
	}
	if info.OriginalPath != srcFile {
		t.Errorf("OriginalPath = %q, want %q", info.OriginalPath, srcFile)
	}
	if info.DestPath == "" {
		t.Error("DestPath is empty")
	}
	if !strings.Contains(info.DestPath, backupDir) {
		t.Errorf("DestPath %q is not under backup-dir %q", info.DestPath, backupDir)
	}
	if info.Size != int64(len("package x\n")) {
		t.Errorf("Size = %d, want %d", info.Size, len("package x\n"))
	}
	if info.Hash == "" {
		t.Error("Hash is empty; expected SHA-256")
	}
	if info.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if info.Kind != "soft_delete" {
		t.Errorf("Kind = %q, want \"soft_delete\"", info.Kind)
	}
}

// TestSoftDeleteLegacyReturnsInfoWithEmptySDID verifies the legacy fallback
// (no --backup-dir) still returns a *SoftDeleteInfo so the response formatter
// can show a "manual move_file" hint, even though the SD-ID is empty.
func TestSoftDeleteLegacyReturnsInfoWithEmptySDID(t *testing.T) {
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "legacy.txt")
	if err := os.WriteFile(srcFile, []byte("legacy content"), 0644); err != nil {
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
		// BackupDir intentionally empty → triggers legacy walk-up
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

	// In legacy mode, SDID is empty (the file is in a parallel filesdelete/ folder
	// without metadata.json, so it's not discoverable via backup(action:"list_trash")).
	if info.SDID != "" {
		t.Errorf("SDID = %q in legacy mode; expected empty (legacy entries are not discoverable)", info.SDID)
	}
	if info.DestPath == "" {
		t.Error("DestPath is empty; legacy mode should still record the trash location")
	}
	if info.OriginalPath != srcFile {
		t.Errorf("OriginalPath = %q, want %q", info.OriginalPath, srcFile)
	}
	if info.Kind != "soft_delete_legacy" {
		t.Errorf("Kind = %q, want \"soft_delete_legacy\"", info.Kind)
	}
}
