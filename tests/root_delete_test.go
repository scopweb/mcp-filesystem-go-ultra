package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestDeleteAllowedPathRootBlocked verifies that delete operations cannot
// remove an allowed-path root directory (which would wipe the entire tree).
func TestDeleteAllowedPathRootBlocked(t *testing.T) {
	// Create a temp directory to act as the allowed-path root
	allowedDir := t.TempDir()

	// Create a file inside so there's something to protect
	testFile := filepath.Join(allowedDir, "important.txt")
	if err := os.WriteFile(testFile, []byte("critical data"), 0644); err != nil {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Test 1: permanent delete of allowed-path root must fail
	err = engine.DeleteFile(ctx, allowedDir)
	if err == nil {
		t.Fatal("DeleteFile should reject allowed-path root, but it succeeded")
	}
	if _, statErr := os.Stat(allowedDir); os.IsNotExist(statErr) {
		t.Fatal("allowed-path root was deleted despite expected protection")
	}

	// Test 2: soft delete of allowed-path root must fail
	_, err = engine.SoftDeleteFile(ctx, allowedDir)
	if err == nil {
		t.Fatal("SoftDeleteFile should reject allowed-path root, but it succeeded")
	}
	if _, statErr := os.Stat(allowedDir); os.IsNotExist(statErr) {
		t.Fatal("allowed-path root was soft-deleted despite expected protection")
	}

	// Test 3: move of allowed-path root must fail
	destDir := t.TempDir()
	dest := filepath.Join(destDir, "stolen")
	err = engine.MoveFile(ctx, allowedDir, dest)
	if err == nil {
		t.Fatal("MoveFile should reject allowed-path root as source, but it succeeded")
	}
	if _, statErr := os.Stat(allowedDir); os.IsNotExist(statErr) {
		t.Fatal("allowed-path root was moved despite expected protection")
	}

	// Test 4: deleting a FILE inside the root must still work
	err = engine.DeleteFile(ctx, testFile)
	if err != nil {
		t.Fatalf("DeleteFile should allow deleting files inside allowed-path root, got: %v", err)
	}
	if _, statErr := os.Stat(testFile); !os.IsNotExist(statErr) {
		t.Fatal("file inside allowed-path should have been deleted")
	}

	// Test 5: the root directory itself must still exist
	if _, statErr := os.Stat(allowedDir); os.IsNotExist(statErr) {
		t.Fatal("allowed-path root should still exist after deleting a file inside it")
	}
}

// TestDeleteAllowedPathRootVariations tests path variations that resolve
// to the allowed-path root (trailing slash, dot components, etc.)
func TestDeleteAllowedPathRootVariations(t *testing.T) {
	allowedDir := t.TempDir()

	cacheSystem, err := cache.NewIntelligentCache(10 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer cacheSystem.Close()

	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheSystem,
		ParallelOps:  2,
		AllowedPaths: []string{allowedDir},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()

	// All these variations should resolve to the same root and be blocked
	variations := []string{
		allowedDir,
		allowedDir + string(os.PathSeparator),
		allowedDir + string(os.PathSeparator) + ".",
		filepath.Join(allowedDir, "subdir", ".."),
	}

	for _, path := range variations {
		err := engine.DeleteFile(ctx, path)
		if err == nil {
			t.Errorf("DeleteFile(%q) should be blocked (resolves to allowed-path root), but succeeded", path)
		}
	}

	// Root must still exist
	if _, statErr := os.Stat(allowedDir); os.IsNotExist(statErr) {
		t.Fatal("allowed-path root was deleted via path variation")
	}
}

// TestSoftDeleteFallsBackWhenNoBackupDir verifies the legacy walk-up fallback
// is preserved when --backup-dir is not configured. The behavior change (issue
// #16) added BackupManager integration as the PREFERRED path; users without
// --backup-dir should still get the old behavior so they aren't broken.
func TestSoftDeleteFallsBackWhenNoBackupDir(t *testing.T) {
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "no-backup-dir.txt")
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
		// BackupDir intentionally empty → legacy walk-up
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

	// Legacy mode: SDID is empty (not discoverable via backup action)
	if info.SDID != "" {
		t.Errorf("legacy mode SDID = %q; want empty", info.SDID)
	}
	if info.Kind != "soft_delete_legacy" {
		t.Errorf("legacy mode Kind = %q; want \"soft_delete_legacy\"", info.Kind)
	}
	if info.DestPath == "" {
		t.Error("legacy mode DestPath is empty; expected a filesdelete/ path")
	}

	// File is gone from original
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatal("source file still exists after legacy soft-delete")
	}
	// File is in some filesdelete/ folder
	if _, err := os.Stat(info.DestPath); err != nil {
		t.Errorf("legacy dest file missing at %s: %v", info.DestPath, err)
	}
}
