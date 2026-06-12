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

// TestBackupRestoreTrash verifies the full soft-delete → restore-trash cycle:
// the file ends up back at the original path with identical content.
func TestBackupRestoreTrash(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "roundtrip.txt")
	originalContent := []byte("this must survive the round trip\n")
	if err := os.WriteFile(srcFile, originalContent, 0644); err != nil {
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

	// File gone from original, present in trash
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatal("source file still exists after soft-delete")
	}

	// Restore
	restoredPath, err := engine.GetBackupManager().RestoreTrash(info.SDID)
	if err != nil {
		t.Fatalf("RestoreTrash failed: %v", err)
	}
	if restoredPath != srcFile {
		t.Errorf("restored to %q, want %q", restoredPath, srcFile)
	}

	// File back at original with identical content
	got, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("restored file unreadable: %v", err)
	}
	if string(got) != string(originalContent) {
		t.Errorf("content mismatch: got %q, want %q", got, originalContent)
	}

	// Trash dir for this SD-ID should be gone
	trashDir := filepath.Join(backupDir, "filesdelete", info.SDID)
	if _, err := os.Stat(trashDir); !os.IsNotExist(err) {
		t.Errorf("trash subdir %q still exists after restore: %v", trashDir, err)
	}
}

// TestRestoreTrashRejectsPathTraversal verifies the SD-ID is sanitized
// against path traversal attacks. Without the sanitizeID check, an attacker
// could pass "../../etc/passwd" and escape the trash root.
func TestRestoreTrashRejectsPathTraversal(t *testing.T) {
	backupDir := t.TempDir()

	cacheSystem, err := cache.NewIntelligentCache(10 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer cacheSystem.Close()

	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheSystem,
		ParallelOps:  2,
		AllowedPaths: []string{t.TempDir()},
		BackupDir:    backupDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	maliciousIDs := []string{
		"../../etc/passwd",
		"..\\..\\windows\\system32",
		"foo/bar",
		"foo\\bar",
		"..",
		".",
		"", // empty
		"normal-but-with-;rm",
	}
	for _, id := range maliciousIDs {
		_, err := engine.GetBackupManager().RestoreTrash(id)
		if err == nil {
			t.Errorf("RestoreTrash(%q) should have failed but succeeded", id)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "invalid") &&
			!strings.Contains(strings.ToLower(err.Error()), "soft-delete id") {
			t.Errorf("RestoreTrash(%q) error %q should mention invalid ID", id, err)
		}
	}
}

// TestRestoreTrashRefusesIfOriginalPathExists verifies that RestoreTrash does
// NOT silently overwrite an existing file at the original path.
func TestRestoreTrashRefusesIfOriginalPathExists(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()
	srcFile := filepath.Join(allowedDir, "shared.txt")
	if err := os.WriteFile(srcFile, []byte("original"), 0644); err != nil {
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

	// User re-creates a file at the original location with DIFFERENT content
	if err := os.WriteFile(srcFile, []byte("NEW CONTENT, NOT TO BE OVERWRITTEN"), 0644); err != nil {
		t.Fatal(err)
	}

	// Restore should refuse
	_, err = engine.GetBackupManager().RestoreTrash(info.SDID)
	if err == nil {
		t.Fatal("RestoreTrash should have refused to overwrite the existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error %q should mention \"already exists\"", err)
	}

	// The user's new file must be intact
	got, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW CONTENT, NOT TO BE OVERWRITTEN" {
		t.Errorf("user's new file was modified: got %q", got)
	}
}
