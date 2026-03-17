package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// TestBug17_OverlappingEditsNotMisreported verifies that when Edit 1 subsumes
// Edit 2's change, Edit 2 is reported as "already_present" (not "failed").
// This is the core fix for Bug #17.
func TestBug17_OverlappingEditsNotMisreported(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "func hello() {\n    fmt.Println(\"hello\")\n}\n"
	testFile := filepath.Join(tempDir, "overlap.go")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{
			OldText: "func hello() {\n    fmt.Println(\"hello\")\n}",
			NewText: "func hello() {\n    fmt.Println(\"world\")\n    return\n}",
		},
		{
			OldText: "    fmt.Println(\"hello\")",
			NewText: "    fmt.Println(\"world\")",
		},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)
	if err != nil {
		t.Fatalf("MultiEdit should succeed, got error: %v", err)
	}

	// Bug #17: FailedEdits should be 0 — Edit 2 is "already_present", not "failed"
	if result.FailedEdits != 0 {
		t.Errorf("FailedEdits: got %d, want 0", result.FailedEdits)
	}

	if result.SuccessfulEdits != 1 {
		t.Errorf("SuccessfulEdits: got %d, want 1", result.SuccessfulEdits)
	}

	if result.SkippedEdits != 1 {
		t.Errorf("SkippedEdits: got %d, want 1", result.SkippedEdits)
	}

	// Verify EditDetails
	if len(result.EditDetails) != 2 {
		t.Fatalf("EditDetails length: got %d, want 2", len(result.EditDetails))
	}
	if result.EditDetails[0].Status != core.EditStatusApplied {
		t.Errorf("Edit 0 status: got %s, want applied", result.EditDetails[0].Status)
	}
	if result.EditDetails[1].Status != core.EditStatusAlreadyPresent {
		t.Errorf("Edit 1 status: got %s, want already_present", result.EditDetails[1].Status)
	}

	// Verify file content is correct
	finalContent, _ := os.ReadFile(testFile)
	if !strings.Contains(string(finalContent), "fmt.Println(\"world\")") {
		t.Error("File should contain the new Println")
	}
	if !strings.Contains(string(finalContent), "return") {
		t.Error("File should contain the return statement")
	}
}

// TestBug17_AllEditsApplied verifies normal case: independent edits all apply cleanly.
func TestBug17_AllEditsApplied(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Use larger content to avoid CRITICAL risk threshold on small files
	content := strings.Repeat("// padding\n", 20) + "line1_target\n" + strings.Repeat("// filler\n", 20) + "line3_target\n" + strings.Repeat("// end\n", 20)
	testFile := filepath.Join(tempDir, "all_apply.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "line1_target", NewText: "modified1"},
		{OldText: "line3_target", NewText: "modified3"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)
	if err != nil {
		t.Fatalf("MultiEdit should succeed: %v", err)
	}

	if result.SuccessfulEdits != 2 {
		t.Errorf("SuccessfulEdits: got %d, want 2", result.SuccessfulEdits)
	}
	if result.SkippedEdits != 0 {
		t.Errorf("SkippedEdits: got %d, want 0", result.SkippedEdits)
	}
	if result.FailedEdits != 0 {
		t.Errorf("FailedEdits: got %d, want 0", result.FailedEdits)
	}

	// Verify all EditDetails are "applied"
	for i, detail := range result.EditDetails {
		if detail.Status != core.EditStatusApplied {
			t.Errorf("Edit %d status: got %s, want applied", i, detail.Status)
		}
	}
}

// TestBug17_GenuineFailureStillReported verifies a truly missing oldText triggers atomic rollback.
// Bug #27: multi_edit is now atomic — if any edit fails, the file is NOT modified.
func TestBug17_GenuineFailureStillReported(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Use larger content so risk assessment doesn't trigger CRITICAL
	content := strings.Repeat("// padding\n", 20) + "line1_target\n" + strings.Repeat("// filler\n", 20) + "line3_target\n" + strings.Repeat("// end\n", 20)
	testFile := filepath.Join(tempDir, "genuine_fail.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "line1_target", NewText: "modified1"},
		{OldText: "NONEXISTENT_TEXT", NewText: "should_fail"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)

	// Bug #27: multi_edit is now atomic — should return error when any edit fails
	if err == nil {
		t.Fatal("MultiEdit should return atomic rollback error when any edit fails")
	}
	if !strings.Contains(err.Error(), "atomic rollback") {
		t.Errorf("Error should mention 'atomic rollback', got: %v", err)
	}

	// Result should still be returned with details
	if result == nil {
		t.Fatal("Result should be non-nil with edit details")
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("SuccessfulEdits: got %d, want 1 (rolled back)", result.SuccessfulEdits)
	}
	if result.FailedEdits != 1 {
		t.Errorf("FailedEdits: got %d, want 1", result.FailedEdits)
	}
	if result.SkippedEdits != 0 {
		t.Errorf("SkippedEdits: got %d, want 0", result.SkippedEdits)
	}

	// Verify the failed edit has proper detail
	if len(result.EditDetails) != 2 {
		t.Fatalf("EditDetails length: got %d, want 2", len(result.EditDetails))
	}
	if result.EditDetails[1].Status != core.EditStatusFailed {
		t.Errorf("Edit 1 status: got %s, want failed", result.EditDetails[1].Status)
	}
	if result.EditDetails[1].Error == "" {
		t.Error("Edit 1 should have an error message")
	}

	// Bug #27: File must NOT be modified (atomic rollback)
	actualContent, _ := os.ReadFile(testFile)
	if string(actualContent) != content {
		t.Error("File was modified despite atomic rollback — Bug #27 regression!")
	}
}

// TestBug17_RiskAssessmentCriticalProceeds verifies CRITICAL risk in MultiEdit
// auto-proceeds with backup and risk warning (Bug #22: multi_edit never blocks).
func TestBug17_RiskAssessmentCriticalProceeds(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Tiny file where edit rewrites nearly everything → CRITICAL
	content := "AB"
	testFile := filepath.Join(tempDir, "critical.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "AB", NewText: "CD"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)
	if err != nil {
		t.Fatalf("CRITICAL risk MultiEdit should proceed (Bug #22), got error: %v", err)
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("SuccessfulEdits: got %d, want 1", result.SuccessfulEdits)
	}
	// Should have a risk warning attached
	if result.RiskWarning == "" {
		t.Error("Expected risk warning for CRITICAL edit, got empty")
	}
	// Backup should be created
	if result.BackupID == "" {
		t.Error("Expected backup for CRITICAL edit, got empty")
	}
}

// TestBug17_RiskAssessmentCriticalForceProceeds verifies force=true bypasses CRITICAL block.
func TestBug17_RiskAssessmentCriticalForceProceeds(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "AB"
	testFile := filepath.Join(tempDir, "critical_force.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "AB", NewText: "CD"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, true)
	if err != nil {
		t.Fatalf("CRITICAL risk with force=true should succeed: %v", err)
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("SuccessfulEdits: got %d, want 1", result.SuccessfulEdits)
	}

	// Verify file was written
	finalContent, _ := os.ReadFile(testFile)
	if string(finalContent) != "CD" {
		t.Errorf("File content: got %q, want %q", string(finalContent), "CD")
	}
}

// TestBug17_EditDetailsPopulated verifies per-edit details are always populated.
// Bug #27: multi_edit is now atomic — any failure causes rollback.
func TestBug17_EditDetailsPopulated(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Use larger content so risk assessment doesn't trigger CRITICAL
	content := strings.Repeat("// padding\n", 20) + "aaa\nbbb\nccc\n" + strings.Repeat("// end\n", 20)
	testFile := filepath.Join(tempDir, "details.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "aaa", NewText: "xxx"},
		{OldText: "", NewText: "empty_old"}, // Should fail: empty old_text
		{OldText: "ccc", NewText: "zzz"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)

	// Bug #27: atomic rollback — any failed edit means file is NOT modified
	if err == nil {
		t.Fatal("Expected atomic rollback error due to empty old_text edit")
	}
	if result == nil {
		t.Fatal("Result should be non-nil with edit details")
	}

	if len(result.EditDetails) != 3 {
		t.Fatalf("EditDetails length: got %d, want 3", len(result.EditDetails))
	}

	if result.EditDetails[0].Status != core.EditStatusApplied {
		t.Errorf("Edit 0: got %s, want applied (rolled back)", result.EditDetails[0].Status)
	}
	if result.EditDetails[1].Status != core.EditStatusFailed {
		t.Errorf("Edit 1: got %s, want failed", result.EditDetails[1].Status)
	}
	// Edit 2 may be applied or not depending on execution order, but file is unchanged

	// Verify snippets are populated
	if result.EditDetails[0].OldTextSnippet == "" {
		t.Error("Edit 0 OldTextSnippet should be populated")
	}

	// File must NOT be modified (atomic rollback)
	actualContent, _ := os.ReadFile(testFile)
	if string(actualContent) != content {
		t.Error("File was modified despite atomic rollback — Bug #27 regression!")
	}
}

// TestBug17_BackwardCompatibility verifies original fields still work correctly.
func TestBug17_BackwardCompatibility(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Use larger content so risk assessment doesn't trigger CRITICAL
	content := strings.Repeat("// padding\n", 30) + "hello world\n" + strings.Repeat("// end\n", 30)
	testFile := filepath.Join(tempDir, "compat.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "hello", NewText: "goodbye"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)
	if err != nil {
		t.Fatalf("Should succeed: %v", err)
	}

	// All original fields still populated
	if result.TotalEdits != 1 {
		t.Errorf("TotalEdits: got %d, want 1", result.TotalEdits)
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("SuccessfulEdits: got %d, want 1", result.SuccessfulEdits)
	}
	if result.FailedEdits != 0 {
		t.Errorf("FailedEdits: got %d, want 0", result.FailedEdits)
	}
	if result.MatchConfidence == "" {
		t.Error("MatchConfidence should be populated")
	}
	if result.BackupID == "" {
		t.Error("BackupID should be populated")
	}
	// New fields should also be set
	if result.SkippedEdits != 0 {
		t.Errorf("SkippedEdits: got %d, want 0", result.SkippedEdits)
	}
	if len(result.EditDetails) != 1 {
		t.Fatalf("EditDetails length: got %d, want 1", len(result.EditDetails))
	}
}

// TestBug17_AllAlreadyPresent verifies that when all edits are already present,
// no write occurs and SkippedEdits is correct.
func TestBug17_AllAlreadyPresent(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// File already has the "after" state — oldText absent, newText present
	content := "modified1\nline2\nmodified3\n"
	testFile := filepath.Join(tempDir, "all_present.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "original1", NewText: "modified1"},
		{OldText: "original3", NewText: "modified3"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)
	if err != nil {
		t.Fatalf("All-already-present should succeed: %v", err)
	}

	if result.SkippedEdits != 2 {
		t.Errorf("SkippedEdits: got %d, want 2", result.SkippedEdits)
	}
	if result.SuccessfulEdits != 0 {
		t.Errorf("SuccessfulEdits: got %d, want 0 (nothing was applied)", result.SuccessfulEdits)
	}
	if result.FailedEdits != 0 {
		t.Errorf("FailedEdits: got %d, want 0", result.FailedEdits)
	}

	// File should be unchanged (no write occurred)
	finalContent, _ := os.ReadFile(testFile)
	if string(finalContent) != content {
		t.Error("File should not have been modified")
	}
}

// TestBug17_MixedOverlapAndIndependent verifies a batch with both overlapping
// and independent edits reports correctly.
func TestBug17_MixedOverlapAndIndependent(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "AAA\nBBB\nCCC\nDDD\n"
	testFile := filepath.Join(tempDir, "mixed.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		// Edit 1: replace AAA\nBBB block (includes BBB)
		{OldText: "AAA\nBBB", NewText: "XXX\nYYY"},
		// Edit 2: replace BBB alone — already subsumed by Edit 1
		{OldText: "BBB", NewText: "YYY"},
		// Edit 3: independent edit on DDD
		{OldText: "DDD", NewText: "ZZZ"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false)
	if err != nil {
		t.Fatalf("Should succeed: %v", err)
	}

	if result.SuccessfulEdits != 2 {
		t.Errorf("SuccessfulEdits: got %d, want 2", result.SuccessfulEdits)
	}
	if result.SkippedEdits != 1 {
		t.Errorf("SkippedEdits: got %d, want 1", result.SkippedEdits)
	}
	if result.FailedEdits != 0 {
		t.Errorf("FailedEdits: got %d, want 0", result.FailedEdits)
	}

	// Verify file content
	finalContent, _ := os.ReadFile(testFile)
	expected := "XXX\nYYY\nCCC\nZZZ\n"
	if string(finalContent) != expected {
		t.Errorf("File content: got %q, want %q", string(finalContent), expected)
	}
}
