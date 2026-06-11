package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestSoftDeleteUsesBackupDir verifies that when --backup-dir is configured,
// SoftDeleteFile moves the file to <backup-dir>/filesdelete/<sd-id>/<basename>
// with a metadata.json sidecar. This is the core fix for issue #16.
func TestSoftDeleteUsesBackupDir(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "important.txt")
	if err := os.WriteFile(srcFile, []byte("critical content"), 0644); err != nil {
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

	// SD-ID must be non-empty and well-formed
	if info.SDID == "" {
		t.Fatal("SDID is empty; expected a non-empty ID like sd-20260611-150455-a1b2c3d4")
	}
	if !strings.HasPrefix(info.SDID, "sd-") {
		t.Errorf("SDID %q does not start with 'sd-'", info.SDID)
	}

	// The file must be GONE from the original location
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatal("source file still exists after SoftDeleteFile")
	}

	// The file must be in <backup-dir>/filesdelete/<sd-id>/<basename>
	expectedDir := filepath.Join(backupDir, "filesdelete", info.SDID)
	if _, err := os.Stat(expectedDir); err != nil {
		t.Fatalf("expected trash subdir %s: %v", expectedDir, err)
	}
	expectedDest := filepath.Join(expectedDir, "important.txt")
	if _, err := os.Stat(expectedDest); err != nil {
		t.Fatalf("expected trashed file %s: %v", expectedDest, err)
	}

	// The metadata.json sidecar must exist and parse
	metaPath := filepath.Join(expectedDir, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("metadata.json missing: %v", err)
	}
	var meta core.SoftDeleteInfo
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("metadata.json invalid: %v", err)
	}
	if meta.OriginalPath != srcFile {
		t.Errorf("metadata.OriginalPath = %q, want %q", meta.OriginalPath, srcFile)
	}
	if meta.DestPath != expectedDest {
		t.Errorf("metadata.DestPath = %q, want %q", meta.DestPath, expectedDest)
	}
	if meta.Size != int64(len("critical content")) {
		t.Errorf("metadata.Size = %d, want %d", meta.Size, len("critical content"))
	}
	if meta.Hash == "" {
		t.Error("metadata.Hash is empty; expected SHA-256 hex")
	}
	if meta.Kind != "soft_delete" {
		t.Errorf("metadata.Kind = %q, want \"soft_delete\"", meta.Kind)
	}

	// Trash directory must NOT contain the legacy "filesdelete/__REPOS/..." style path
	// (which was the bug — file got mis-rooted to C:\temp\__REPOS\...)
	trashRoot := filepath.Join(backupDir, "filesdelete")
	walkErr := filepath.Walk(trashRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Any path under the trash should be <sd-id>/<basename> or <sd-id>/metadata.json
		// Never __REPOS or arbitrary deep trees.
		if info.IsDir() && info.Name() == "__REPOS" {
			t.Errorf("trash contains __REPOS/ — legacy walk-up path leaked: %s", path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk trash failed: %v", walkErr)
	}
}
