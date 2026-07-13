package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectReplace_Basic verifies basic project_replace functionality
func TestProjectReplace_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	// Create test files
	testFiles := []string{
		filepath.Join(tmpDir, "file1.php"),
		filepath.Join(tmpDir, "file2.php"),
		filepath.Join(tmpDir, "file3.html"),
	}
	contents := []string{
		"<?php echo utf8_encode($name); ?>",
		"<?php utf8_encode($value); ?>",
		"<div>utf8_encode(</div>",
	}
	for i, f := range testFiles {
		if err := os.WriteFile(f, []byte(contents[i]), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Execute project_replace WITHOUT backup (to isolate the backup issue)
	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode(", "utf8e(", true, true, ".php,.html", nil, nil, false, false, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.FilesChanged != 3 {
		t.Errorf("Expected 3 files changed, got %d", result.FilesChanged)
	}
	if result.TotalReplaced != 3 {
		t.Errorf("Expected 3 total replacements, got %d", result.TotalReplaced)
	}

	// Verify files were modified
	for i, f := range testFiles {
		content, _ := os.ReadFile(f)
		if strings.Contains(string(content), "utf8_encode(") {
			t.Errorf("file%d: utf8_encode still present after replace", i+1)
		}
		if !strings.Contains(string(content), "utf8e(") {
			t.Errorf("file%d: utf8e( not found after replace", i+1)
		}
	}
}

// TestProjectReplace_DryRun verifies dry_run doesn't modify files
func TestProjectReplace_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "test.php")
	originalContent := "<?php utf8_encode($x); utf8_encode($y); ?>"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode(", "utf8e(", true, true, ".php", nil, nil, true, true, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if !result.DryRun {
		t.Error("Expected dry_run=true in result")
	}
	if result.TotalReplaced != 2 {
		t.Errorf("Expected 2 replacements counted, got %d", result.TotalReplaced)
	}

	// Verify file was NOT modified
	content, _ := os.ReadFile(testFile)
	if string(content) != originalContent {
		t.Error("File was modified despite dry_run=true")
	}
}

// TestProjectReplace_FileTypeFilter verifies file type filtering
func TestProjectReplace_FileTypeFilter(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	// Create .php and .txt files
	phpFile := filepath.Join(tmpDir, "test.php")
	txtFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(phpFile, []byte("utf8_encode"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(txtFile, []byte("utf8_encode"), 0644); err != nil {
		t.Fatal(err)
	}

	// Only .php files
	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode", "utf8e", true, true, ".php", nil, nil, false, false, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.FilesChanged != 1 {
		t.Errorf("Expected 1 file changed (.php only), got %d", result.FilesChanged)
	}

	// .txt should be unchanged
	content, _ := os.ReadFile(txtFile)
	if string(content) != "utf8_encode" {
		t.Error(".txt file was modified despite filter")
	}
	// .php should be changed
	content, _ = os.ReadFile(phpFile)
	if string(content) != "utf8e" {
		t.Error(".php file was not modified")
	}
}

// TestProjectReplace_ExcludePaths verifies exclude_paths
func TestProjectReplace_ExcludePaths(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	// Create files in main dir and subdir
	mainFile := filepath.Join(tmpDir, "main.php")
	subDir := filepath.Join(tmpDir, "jotajotape")
	os.MkdirAll(subDir, 0755)
	subFile := filepath.Join(subDir, "excluded.php")

	if err := os.WriteFile(mainFile, []byte("utf8_encode"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, []byte("utf8_encode"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode", "utf8e", true, true, ".php", nil, []string{"jotajotape/**"}, false, false, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.FilesChanged != 1 {
		t.Errorf("Expected 1 file changed (subdir excluded), got %d", result.FilesChanged)
	}

	// main.php should be changed
	content, _ := os.ReadFile(mainFile)
	if string(content) != "utf8e" {
		t.Error("main.php was not modified")
	}
	// subdir file should be unchanged
	content, _ = os.ReadFile(subFile)
	if string(content) != "utf8_encode" {
		t.Error("jotajotape/excluded.php was modified despite exclusion")
	}
}

// TestProjectReplace_Regex verifies regex mode
func TestProjectReplace_Regex(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "test.php")
	if err := os.WriteFile(testFile, []byte("item_1 item_2 item_3"), 0644); err != nil {
		t.Fatal(err)
	}

	// Replace item_\d+ with obj_$1
	result, err := engine.ProjectReplace(context.Background(), tmpDir, `item_(\d+)`, `obj_$1`, false, true, ".php", nil, nil, false, false, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace regex failed: %v", err)
	}

	if result.TotalReplaced != 3 {
		t.Errorf("Expected 3 replacements, got %d", result.TotalReplaced)
	}

	content, _ := os.ReadFile(testFile)
	if !strings.Contains(string(content), "obj_1 obj_2 obj_3") {
		t.Errorf("Expected 'obj_1 obj_2 obj_3', got: %s", content)
	}
}

// TestProjectReplace_CaseInsensitive verifies case-insensitive matching
func TestProjectReplace_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "test.php")
	if err := os.WriteFile(testFile, []byte("UTF8_ENCODE Utf8_Encode utf8_encode"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode", "utf8e", true, false, ".php", nil, nil, false, false, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.TotalReplaced != 3 {
		t.Errorf("Expected 3 replacements (case-insensitive), got %d", result.TotalReplaced)
	}

	content, _ := os.ReadFile(testFile)
	if strings.Contains(string(content), "UTF8_ENCODE") || strings.Contains(string(content), "Utf8_Encode") || strings.Contains(string(content), "utf8_encode") {
		t.Errorf("Some occurrences still present: %s", content)
	}
}

// TestProjectReplace_MaxFilesCap verifies max_files safety cap
func TestProjectReplace_MaxFilesCap(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	// Create 20 files
	for i := 0; i < 20; i++ {
		f := filepath.Join(tmpDir, "file20.go")
		if err := os.WriteFile(f, []byte("utf8_encode"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Set max_files=5
	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode", "utf8e", true, true, ".go", nil, nil, false, false, true, 5)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.FilesChanged > 5 {
		t.Errorf("Expected max 5 files changed (max_files cap), got %d", result.FilesChanged)
	}
}

// TestProjectReplace_NoMatches verifies behavior with no matches
func TestProjectReplace_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "test.php")
	if err := os.WriteFile(testFile, []byte("something else"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode", "utf8e", true, true, ".php", nil, nil, false, false, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.FilesChanged != 0 {
		t.Errorf("Expected 0 files changed, got %d", result.FilesChanged)
	}
	if result.TotalReplaced != 0 {
		t.Errorf("Expected 0 replacements, got %d", result.TotalReplaced)
	}
}

// TestProjectReplace_BackupCreation verifies backup is created when create_backup=true
func TestProjectReplace_BackupCreation(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "test.php")
	originalContent := "<?php utf8_encode(); ?>"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.ProjectReplace(context.Background(), tmpDir, "utf8_encode", "utf8e", true, true, ".php", nil, nil, false, true, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.BackupID == "" {
		t.Error("Expected backup ID when create_backup=true")
	}

	// Verify backup exists
	backupInfo, err := engine.GetBackupManager().GetBackupInfo(result.BackupID)
	if err != nil {
		t.Fatalf("GetBackupInfo failed: %v", err)
	}
	if len(backupInfo.Files) != 1 {
		t.Errorf("Expected 1 file in backup, got %d", len(backupInfo.Files))
	}
}

// TestProjectReplace_EmptyDirectory verifies handling of directory with no matching files
func TestProjectReplace_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	// Create empty directory
	emptyDir := filepath.Join(tmpDir, "empty")
	os.MkdirAll(emptyDir, 0755)

	result, err := engine.ProjectReplace(context.Background(), emptyDir, "utf8_encode", "utf8e", true, true, ".php", nil, nil, false, false, true, 1000)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if result.FilesChanged != 0 {
		t.Errorf("Expected 0 files changed in empty dir, got %d", result.FilesChanged)
	}
}

// TestProjectReplace_BackupRestoresOriginalContent is the regression test for
// the bug documented in docs/ISSUE-project-replace-backup-after-write.md.
// Before the fix, the batch backup was created AFTER the writes completed,
// so it captured the post-replace bytes; restoring it was a no-op. The fix
// moves the snapshot to BEFORE the first atomic write, which means
// `backup(action:"restore", backup_id:...)` now reverts the whole tree to
// its pre-replace state — exactly what an agent reaching for the rollback
// button expects.
func TestProjectReplace_BackupRestoresOriginalContent(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFiles := []string{
		filepath.Join(tmpDir, "a.go"),
		filepath.Join(tmpDir, "b.go"),
	}
	originals := []string{
		"package a\n\nfunc Alpha() {}\n",
		"package b\n\nfunc Alpha() {}\n",
	}
	for i, f := range testFiles {
		if err := os.WriteFile(f, []byte(originals[i]), 0644); err != nil {
			t.Fatalf("seed %s: %v", f, err)
		}
	}

	result, err := engine.ProjectReplace(context.Background(), tmpDir, "func Alpha", "func Omega", true, true, ".go", nil, nil, false, true, false, 100)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}
	if result.BackupID == "" {
		t.Fatal("expected backup_id in result when create_backup=true")
	}

	// Sanity-check that the writes actually happened — otherwise the
	// restore assertion below would be vacuous.
	for _, f := range testFiles {
		got, _ := os.ReadFile(f)
		if strings.Contains(string(got), "func Alpha") {
			t.Fatalf("file %s was not modified by project_replace (precondition for restore)", filepath.Base(f))
		}
	}

	// Restore from the project_replace backup. createBackup=true so a safety
	// pre-restore snapshot is taken automatically.
	restoredFiles, preRestoreID, err := engine.GetBackupManager().RestoreBackup(result.BackupID, "", true)
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}
	if len(restoredFiles) != len(testFiles) {
		t.Errorf("expected %d files restored, got %d (%v)", len(testFiles), len(restoredFiles), restoredFiles)
	}
	if preRestoreID == "" {
		t.Error("expected a pre-restore safety backup ID when createBackup=true")
	}

	// Critical assertion: every file must contain its ORIGINAL content,
	// not the post-replace bytes.
	for i, f := range testFiles {
		got, _ := os.ReadFile(f)
		if string(got) != originals[i] {
			t.Errorf("restore did not revert %s:\n  got:  %q\n  want: %q", filepath.Base(f), got, originals[i])
		}
	}
}

// TestProjectReplace_BackupRegistersUndoChain verifies that after a
// project_replace with create_backup=true, every touched file is registered
// in the per-file undo chain so `backup(action:"undo_last", file_path:...)`
// can step back through the project_replace layer (mirrors how edit_file
// registers its chain entry).
func TestProjectReplace_BackupRegistersUndoChain(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "chain.go")
	original := "package x\n\nfunc Alpha() {}\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := engine.ProjectReplace(context.Background(), tmpDir, "func Alpha", "func Omega", true, true, ".go", nil, nil, false, true, false, 100)
	if err != nil {
		t.Fatalf("ProjectReplace failed: %v", err)
	}

	if got := engine.GetCurrentBackupID(testFile); got != result.BackupID {
		t.Errorf("expected chain entry %q for %s, got %q", result.BackupID, testFile, got)
	}
}
