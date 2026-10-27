package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUndoLast_PreviewAndSingleStep(t *testing.T) {
	testUndoLastPreviewAndSingleStep(t, t.TempDir())
}

func testUndoLastPreviewAndSingleStep(t *testing.T, dir string) {
	t.Helper()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "undo.txt")
	original := "alpha\r\nbeta\r\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	edit := callNamed(t, reg, ctx, "edit_file", map[string]interface{}{
		"path": path, "old_text": "beta", "new_text": "BETA",
	})
	if edit.IsError {
		t.Fatal(resultText(t, edit))
	}
	firstID := reg.engine.GetCurrentBackupID(path)
	patch := callNamed(t, reg, ctx, "apply_patch", map[string]interface{}{
		"path":  path,
		"patch": "--- a/undo.txt\n+++ b/undo.txt\n@@ -1,2 +1,2 @@\n-alpha\n+ALPHA\n BETA\n",
	})
	if patch.IsError {
		t.Fatal(resultText(t, patch))
	}
	secondID := reg.engine.GetCurrentBackupID(path)
	if firstID == "" || secondID == "" || firstID == secondID {
		t.Fatalf("expected two backup cursors: %q -> %q", firstID, secondID)
	}
	mustFile(t, path, "ALPHA\r\nBETA\r\n")
	// A fixed timestamp detects writes even on filesystems with coarse clocks.
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := reg.engine.GetBackupManager().ListBackups(100, "all", path, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"preview", "dry_run"} {
		t.Run(flag, func(t *testing.T) {
			res := callNamed(t, reg, ctx, "backup", map[string]interface{}{
				"action": "undo_last", "file_path": path, flag: true,
			})
			if res.IsError {
				t.Fatal(resultText(t, res))
			}
			mustFile(t, path, "ALPHA\r\nBETA\r\n")
			after, err := os.Stat(path)
			if err != nil || !after.ModTime().Equal(before.ModTime()) {
				t.Fatalf("preview changed mtime: %v", err)
			}
			if got := reg.engine.GetCurrentBackupID(path); got != secondID {
				t.Fatalf("preview advanced cursor: %q", got)
			}
			current, err := reg.engine.GetBackupManager().ListBackups(100, "all", path, 0)
			if err != nil || len(current) != len(backups) {
				t.Fatalf("preview changed backups: %v", err)
			}
		})
	}
	for _, step := range []struct{ want, cursor string }{
		{"alpha\r\nBETA\r\n", firstID},
		{original, ""},
	} {
		res := callNamed(t, reg, ctx, "backup", map[string]interface{}{
			"action": "undo_last", "file_path": path,
		})
		if res.IsError {
			t.Fatal(resultText(t, res))
		}
		mustFile(t, path, step.want)
		if got := reg.engine.GetCurrentBackupID(path); got != step.cursor {
			t.Fatalf("cursor=%q, want %q", got, step.cursor)
		}
	}
}

func TestUndoLast_BrokenCursorDoesNotRestoreLatestBackup(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "broken.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res := callNamed(t, reg, ctx, "edit_file", map[string]interface{}{
		"path": path, "old_text": "before", "new_text": "after",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	reg.engine.SetCurrentBackupID(path, "missing-backup")
	for _, preview := range []bool{true, false} {
		res = callNamed(t, reg, ctx, "backup", map[string]interface{}{
			"action": "undo_last", "file_path": path, "preview": preview,
		})
		if !res.IsError {
			t.Fatal("broken cursor must fail instead of restoring the latest global backup")
		}
		mustFile(t, path, "after\n")
	}
}
