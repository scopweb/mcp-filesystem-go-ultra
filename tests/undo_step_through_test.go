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

// setupUndoEngine creates a test engine with temp dir
func setupUndoEngine(t *testing.T) (*core.UltraFastEngine, string) {
	t.Helper()
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}

	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("Failed to create test engine: %v", err)
	}

	return engine, tempDir
}

// TestUndoChain_BasicStepThrough verifies that successive edits create a linked backup chain
func TestUndoChain_BasicStepThrough(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	testFile := filepath.Join(tempDir, "step_test.txt")
	original := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// First edit
	r1, err := engine.EditFile(context.Background(), testFile, "line1", "EDITED1", false, false)
	if err != nil {
		t.Fatalf("First edit failed: %v", err)
	}
	backup1 := r1.BackupID
	if backup1 == "" {
		t.Fatal("First edit should create backup")
	}

	// Verify backup1 is current in chain
	current := engine.GetCurrentBackupID(testFile)
	if current != backup1 {
		t.Errorf("GetCurrentBackupID: got %q, want %q", current, backup1)
	}

	// Second edit
	r2, err := engine.EditFile(context.Background(), testFile, "line2", "EDITED2", false, false)
	if err != nil {
		t.Fatalf("Second edit failed: %v", err)
	}
	backup2 := r2.BackupID
	if backup2 == "" {
		t.Fatal("Second edit should create backup")
	}

	// Chain should now point to backup2
	current = engine.GetCurrentBackupID(testFile)
	if current != backup2 {
		t.Errorf("GetCurrentBackupID after second edit: got %q, want %q", current, backup2)
	}

	// Verify backup2's parent is backup1
	info2, err := engine.GetBackupManager().GetBackupInfo(backup2)
	if err != nil {
		t.Fatalf("GetBackupInfo(backup2) failed: %v", err)
	}
	if info2.PreviousBackupID != backup1 {
		t.Errorf("backup2.PreviousBackupID: got %q, want %q", info2.PreviousBackupID, backup1)
	}
}

// TestUndoChain_RestorePreviousInChain verifies that RestorePreviousInChain follows the chain
func TestUndoChain_RestorePreviousInChain(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	testFile := filepath.Join(tempDir, "chain_restore.txt")
	original := "AAA\nBBB\nCCC\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// Edit 1: AAA -> AAA1
	r1, _ := engine.EditFile(context.Background(), testFile, "AAA", "AAA1", false, false)
	b1 := r1.BackupID

	// Verify file now has AAA1
	content1, _ := os.ReadFile(testFile)
	if !strings.Contains(string(content1), "AAA1") {
		t.Fatalf("Expected AAA1 in file, got: %s", content1)
	}

	// Edit 2: BBB -> BBB2
	r2, _ := engine.EditFile(context.Background(), testFile, "BBB", "BBB2", false, false)
	b2 := r2.BackupID

	// Verify file now has BBB2
	content2, _ := os.ReadFile(testFile)
	if !strings.Contains(string(content2), "BBB2") {
		t.Fatalf("Expected BBB2 in file, got: %s", content2)
	}

	// Chain: b2 -> b1
	// Undo step: restore b2 to b1
	restored, prevID, hasMore, err := engine.GetBackupManager().RestorePreviousInChain(b2, testFile)
	if err != nil {
		t.Fatalf("RestorePreviousInChain failed: %v", err)
	}
	if len(restored) == 0 {
		t.Error("Expected at least one restored file")
	}
	if prevID != b1 {
		t.Errorf("prevID: got %q, want %q", prevID, b1)
	}
	if !hasMore {
		t.Error("Expected hasMore=true (b1 still exists)")
	}

	// Simulate what tools_batch.go does after RestorePreviousInChain
	if prevID != "" {
		engine.SetCurrentBackupID(testFile, prevID)
	} else {
		engine.ClearBackupID(testFile)
	}

	// File should now have original content (from b1) - AAA without AAA1, and BBB without BBB2
	content3, _ := os.ReadFile(testFile)
	if strings.Contains(string(content3), "AAA1") {
		t.Errorf("Expected AAA1 NOT in file after undo, got: %s", content3)
	}
	if strings.Contains(string(content3), "BBB2") {
		t.Errorf("Expected BBB2 NOT in file after undo, got: %s", content3)
	}
	// b1 contains original state (before first edit)
	if !strings.Contains(string(content3), "AAA\nBBB\nCCC") {
		t.Errorf("Expected original content in file after undo, got: %s", content3)
	}

	// Chain should now point to b1
	current := engine.GetCurrentBackupID(testFile)
	if current != b1 {
		t.Errorf("GetCurrentBackupID: got %q, want %q", current, b1)
	}
}

// TestUndoChain_ClearOnEnd verifies that ClearBackupID is called when chain ends
func TestUndoChain_ClearOnEnd(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	testFile := filepath.Join(tempDir, "chain_end.txt")
	original := "ORIGINAL\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// One edit
	r1, _ := engine.EditFile(context.Background(), testFile, "ORIGINAL", "MODIFIED", false, false)
	b1 := r1.BackupID

	// Verify chain points to b1
	if engine.GetCurrentBackupID(testFile) != b1 {
		t.Fatal("Chain should point to b1")
	}

	// Undo the only edit
	restored, prevID, hasMore, _ := engine.GetBackupManager().RestorePreviousInChain(b1, testFile)
	if len(restored) == 0 {
		t.Error("Expected restored files")
	}
	if prevID != "" {
		t.Errorf("prevID: got %q, want empty (b1 was first backup)", prevID)
	}
	if hasMore {
		t.Error("Expected hasMore=false (no more backups)")
	}

	// Manually clear chain (like tools_batch.go does when prevID is empty)
	engine.ClearBackupID(testFile)

	// Chain should be empty
	if engine.GetCurrentBackupID(testFile) != "" {
		t.Error("Chain should be empty after reaching end of chain")
	}
}

// TestUndoChain_SetAndClear verifies SetCurrentBackupID and ClearBackupID
func TestUndoChain_SetAndClear(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	testFile := filepath.Join(tempDir, "set_clear.txt")
	if err := os.WriteFile(testFile, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set backup ID
	engine.SetCurrentBackupID(testFile, "test-backup-123")
	if engine.GetCurrentBackupID(testFile) != "test-backup-123" {
		t.Error("SetCurrentBackupID / GetCurrentBackupID mismatch")
	}

	// Clear
	engine.ClearBackupID(testFile)
	if engine.GetCurrentBackupID(testFile) != "" {
		t.Error("ClearBackupID should result in empty")
	}
}

// TestUndoChain_MultiEditChain verifies multi_edit also creates linked backups
func TestUndoChain_MultiEditChain(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	testFile := filepath.Join(tempDir, "multi_chain.txt")
	original := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// First multi_edit
	edits1 := []core.MultiEditOperation{
		{OldText: "line1", NewText: "M1A"},
		{OldText: "line2", NewText: "M1B"},
	}
	r1, err := engine.MultiEdit(context.Background(), testFile, edits1, false, false)
	if err != nil {
		t.Fatalf("First MultiEdit failed: %v", err)
	}
	b1 := r1.BackupID

	// Second multi_edit
	edits2 := []core.MultiEditOperation{
		{OldText: "line3", NewText: "M2A"},
		{OldText: "line4", NewText: "M2B"},
	}
	r2, err := engine.MultiEdit(context.Background(), testFile, edits2, false, false)
	if err != nil {
		t.Fatalf("Second MultiEdit failed: %v", err)
	}
	b2 := r2.BackupID

	// Chain: b2 -> b1
	info2, _ := engine.GetBackupManager().GetBackupInfo(b2)
	if info2.PreviousBackupID != b1 {
		t.Errorf("b2.PreviousBackupID: got %q, want %q", info2.PreviousBackupID, b1)
	}
}

// TestVerifyIntegrity_HighRisk verifies that VerifyFileIntegrity is called for HIGH risk edits
func TestVerifyIntegrity_HighRisk(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	// HIGH risk: 80% change
	content := strings.Repeat("A", 150) + strings.Repeat("B", 200) + strings.Repeat("C", 150)
	testFile := filepath.Join(tempDir, "high_risk_verify.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, strings.Repeat("B", 200), strings.Repeat("D", 200), false, false)
	if err != nil {
		t.Fatalf("HIGH risk edit failed: %v", err)
	}

	// Should have integrity check
	if result.Integrity == nil {
		t.Fatal("HIGH risk edit should have Integrity result")
	}

	if result.Integrity.Verification != "OK" {
		t.Errorf("Integrity.Verification: got %q, want OK", result.Integrity.Verification)
	}
	if result.Integrity.Lines == 0 {
		t.Error("Integrity.Lines should be non-zero")
	}
	if result.Integrity.SizeBytes == 0 {
		t.Error("Integrity.SizeBytes should be non-zero")
	}
	if result.Integrity.Hash == "" {
		t.Error("Integrity.Hash should be non-empty")
	}
}

// TestVerifyIntegrity_CriticalRisk verifies that VerifyFileIntegrity is called for CRITICAL risk edits
func TestVerifyIntegrity_CriticalRisk(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	// CRITICAL risk: entire file replaced — file ends up tiny (35 bytes)
	// This triggers VerifyFileIntegrity's WARNING for suspiciously small file
	content := "ENTIRE_FILE_CONTENT"
	testFile := filepath.Join(tempDir, "critical_verify.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, content, "NEW_CONTENT_COMPLETELY_DIFFERENT", false, false)
	if err != nil {
		t.Fatalf("CRITICAL risk edit failed: %v", err)
	}

	// Should have integrity check (verify IS called for CRITICAL)
	if result.Integrity == nil {
		t.Fatal("CRITICAL risk edit should have Integrity result")
	}
	// File is 35 bytes after large change → WARNING (suspiciously small)
	if result.Integrity.Verification != "WARNING" {
		t.Errorf("Integrity.Verification: got %q, want WARNING (35 bytes is suspiciously small)", result.Integrity.Verification)
	}
	if result.Integrity.Warning == "" {
		t.Error("WARNING should have explanation")
	}
}

// TestVerifyIntegrity_LowRiskNoVerify verifies that LOW risk edits skip integrity check
func TestVerifyIntegrity_LowRiskNoVerify(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	// LOW risk: small change in large file
	content := strings.Repeat("A", 500) + "TARGET" + strings.Repeat("B", 500)
	testFile := filepath.Join(tempDir, "low_risk_no_verify.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, "TARGET", "CHANGED", false, false)
	if err != nil {
		t.Fatalf("LOW risk edit failed: %v", err)
	}

	// Should NOT have integrity check for LOW risk
	if result.Integrity != nil {
		t.Error("LOW risk edit should NOT have Integrity result")
	}
}

// TestVerifyIntegrity_ResultFields verifies FileIntegrityResult fields
func TestVerifyIntegrity_ResultFields(t *testing.T) {
	engine, tempDir := setupUndoEngine(t)

	testFile := filepath.Join(tempDir, "integrity_fields.txt")

	// Create a file and then do a HIGH risk edit (replace 200 B's with 200 D's = 80% change)
	content := strings.Repeat("A", 150) + strings.Repeat("B", 200) + strings.Repeat("C", 150)
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, _ := engine.EditFile(context.Background(), testFile, strings.Repeat("B", 200), strings.Repeat("D", 200), false, false)
	inv := result.Integrity

	if inv == nil {
		t.Fatal("HIGH risk edit should have Integrity result")
	}
	if inv.Readable != true {
		t.Error("Readable should be true")
	}
	if inv.Warning != "" {
		t.Errorf("Warning should be empty for OK status, got: %s", inv.Warning)
	}

	// Verify first 8 chars of hash are returned (as shown in output)
	if len(inv.Hash) < 8 {
		t.Errorf("Hash too short: %s", inv.Hash)
	}
}

// TestVerifyIntegrity_TruncatedFile warns when file is suspiciously small after high-change operation
func TestVerifyIntegrity_TruncatedFile(t *testing.T) {
	// This test verifies the logic by calling VerifyFileIntegrity directly
	// with a file that would be considered "too small"

	// Create a small temp file
	tempDir := t.TempDir()
	smallFile := filepath.Join(tempDir, "small.txt")
	if err := os.WriteFile(smallFile, []byte("X"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create engine to call VerifyFileIntegrity
	cacheInstance, _ := cache.NewIntelligentCache(1024 * 1024)
	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}
	engine, _ := core.NewUltraFastEngine(config)

	// Call directly with expectedChangePct > 50 (should trigger warning)
	result := engine.VerifyFileIntegrity(smallFile, 80.0)

	if result.Verification != "WARNING" {
		t.Errorf("Expected WARNING for small file after high change, got: %s", result.Verification)
	}
	if !strings.Contains(result.Warning, "only") {
		t.Errorf("Warning should mention file size, got: %s", result.Warning)
	}
}