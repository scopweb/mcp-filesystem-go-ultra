package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func TestSoftDeleteDirectoryRoundTrip(t *testing.T) {
	backupDir := t.TempDir()
	allowedDir := t.TempDir()
	dir := filepath.Join(allowedDir, "Pda2")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "Pda.cshtml")
	if err := os.WriteFile(nested, []byte("page"), 0644); err != nil {
		t.Fatal(err)
	}

	cacheSystem, err := cache.NewIntelligentCache(1024 * 1024)
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

	info, err := engine.SoftDeleteFile(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "soft_delete_dir" {
		t.Fatalf("kind %q", info.Kind)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("directory still present")
	}
	if _, err := os.Stat(filepath.Join(info.DestPath, "Pda.cshtml")); err != nil {
		t.Fatalf("trashed tree missing: %v", err)
	}

	restored, err := engine.GetBackupManager().RestoreTrash(info.SDID)
	if err != nil {
		t.Fatal(err)
	}
	if restored != dir {
		t.Fatalf("restored %s", restored)
	}
	b, err := os.ReadFile(nested)
	if err != nil || string(b) != "page" {
		t.Fatalf("restore content: %v %q", err, b)
	}
}
