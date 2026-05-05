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

// setupBug16Engine creates a test engine and returns it with its allowed tempDir
func setupBug16Engine(t *testing.T) (*core.UltraFastEngine, string) {
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

// TestBug16_UpdatedThresholds verifies the new default risk thresholds
func TestBug16_UpdatedThresholds(t *testing.T) {
	thresholds := core.DefaultRiskThresholds()

	if thresholds.MediumPercentage != 20.0 {
		t.Errorf("MediumPercentage: got %.1f, want 20.0", thresholds.MediumPercentage)
	}
	if thresholds.HighPercentage != 75.0 {
		t.Errorf("HighPercentage: got %.1f, want 75.0", thresholds.HighPercentage)
	}
	if thresholds.MediumOccurrences != 50 {
		t.Errorf("MediumOccurrences: got %d, want 50", thresholds.MediumOccurrences)
	}
	if thresholds.HighOccurrences != 100 {
		t.Errorf("HighOccurrences: got %d, want 100", thresholds.HighOccurrences)
	}
}

// TestBug16_ShouldBlockOnlyCritical verifies ShouldBlockOperation only blocks CRITICAL
func TestBug16_ShouldBlockOnlyCritical(t *testing.T) {
	thresholds := core.DefaultRiskThresholds()

	tests := []struct {
		name        string
		content     string
		oldText     string
		newText     string
		expectBlock bool
		expectRisk  string
	}{
		{
			name:        "LOW risk - no block",
			content:     strings.Repeat("A", 200) + "REPLACE_ME" + strings.Repeat("B", 200),
			oldText:     "REPLACE_ME",
			newText:     "CHANGED_OK",
			expectBlock: false,
			expectRisk:  "low",
		},
		{
			// Honest metric: CharactersChanged = max(100, 100) * 1 = 100,
			// content = 500 → 20% = MEDIUM (threshold MediumPercentage=20.0).
			name:        "MEDIUM risk - no block",
			content:     strings.Repeat("A", 200) + strings.Repeat("B", 100) + strings.Repeat("C", 200),
			oldText:     strings.Repeat("B", 100),
			newText:     strings.Repeat("D", 100),
			expectBlock: false,
			expectRisk:  "medium",
		},
		{
			// Honest metric: CharactersChanged = max(400, 400) * 1 = 400,
			// content = 500 → 80% = HIGH (threshold HighPercentage=75.0).
			// Old (incorrect) formula counted (oldLen+newLen)*occurrences and
			// reached HIGH with only 200-byte oldText/newText; that double-
			// counted bytes that the edit never actually moved.
			name:        "HIGH risk - no block",
			content:     strings.Repeat("A", 50) + strings.Repeat("B", 400) + strings.Repeat("C", 50),
			oldText:     strings.Repeat("B", 400),
			newText:     strings.Repeat("D", 400),
			expectBlock: false,
			expectRisk:  "high",
		},
		{
			name:        "CRITICAL risk - no block (always warn, never block)",
			content:     strings.Repeat("A", 10),
			oldText:     strings.Repeat("A", 10),
			newText:     strings.Repeat("B", 10),
			expectBlock: false,
			expectRisk:  "critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impact := core.CalculateChangeImpact(tt.content, tt.oldText, tt.newText, thresholds)

			if impact.RiskLevel != tt.expectRisk {
				t.Errorf("RiskLevel: got %q, want %q (change=%.1f%%)", impact.RiskLevel, tt.expectRisk, impact.ChangePercentage)
			}

			blocked := impact.ShouldBlockOperation(false)
			if blocked != tt.expectBlock {
				t.Errorf("ShouldBlockOperation(false): got %v, want %v (risk=%s, change=%.1f%%)", blocked, tt.expectBlock, impact.RiskLevel, impact.ChangePercentage)
			}

			// force=true should NEVER block
			if impact.ShouldBlockOperation(true) {
				t.Error("ShouldBlockOperation(true) should never block")
			}
		})
	}
}

// TestBug16_MediumRiskAutoProceeds verifies MEDIUM risk edits succeed without force
func TestBug16_MediumRiskAutoProceeds(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Honest metric: CharactersChanged = max(100,100) * 1 = 100,
	// content = 500 → 20% = MEDIUM (threshold MediumPercentage=20.0).
	content := strings.Repeat("A", 200) + strings.Repeat("B", 100) + strings.Repeat("C", 200)
	testFile := filepath.Join(tempDir, "medium_risk.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Edit with force=false — should NOT block (Bug #16 fix)
	result, err := engine.EditFile(context.Background(), testFile, strings.Repeat("B", 100), strings.Repeat("D", 100), false, false)
	if err != nil {
		t.Fatalf("MEDIUM risk edit should NOT block, got error: %v", err)
	}

	if result.ReplacementCount != 1 {
		t.Errorf("ReplacementCount: got %d, want 1", result.ReplacementCount)
	}

	// Should have a backup
	if result.BackupID == "" {
		t.Error("BackupID should be non-empty for MEDIUM risk edit")
	}

	// Should have a risk warning
	if result.RiskWarning == "" {
		t.Error("RiskWarning should be non-empty for MEDIUM risk edit")
	}
}

// TestBug16_HighRiskAutoProceeds verifies HIGH risk edits succeed without force
func TestBug16_HighRiskAutoProceeds(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Honest metric: CharactersChanged = max(400,400) * 1 = 400,
	// content = 500 → 80% = HIGH (threshold HighPercentage=75.0).
	content := strings.Repeat("A", 50) + strings.Repeat("B", 400) + strings.Repeat("C", 50)
	testFile := filepath.Join(tempDir, "high_risk.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, strings.Repeat("B", 400), strings.Repeat("D", 400), false, false)
	if err != nil {
		t.Fatalf("HIGH risk edit should NOT block, got error: %v", err)
	}

	if result.BackupID == "" {
		t.Error("BackupID should be non-empty for HIGH risk edit")
	}

	if result.RiskWarning == "" {
		t.Error("RiskWarning should be non-empty for HIGH risk edit")
	}
}

// TestBug16_CriticalRiskProceeds verifies CRITICAL risk edits auto-proceed with
// backup and risk warning (Bug #22: never block, always backup).
func TestBug16_CriticalRiskProceeds(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// CRITICAL risk: replace the entire file content
	content := strings.Repeat("A", 10)
	testFile := filepath.Join(tempDir, "critical_risk.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, strings.Repeat("A", 10), strings.Repeat("B", 10), false, false)
	if err != nil {
		t.Fatalf("CRITICAL risk edit should proceed (Bug #22), got error: %v", err)
	}
	if result.ReplacementCount != 1 {
		t.Errorf("ReplacementCount: got %d, want 1", result.ReplacementCount)
	}
	if result.RiskWarning == "" {
		t.Error("Expected risk warning for CRITICAL edit, got empty")
	}
	if result.BackupID == "" {
		t.Error("Expected backup for CRITICAL edit, got empty")
	}
}

// TestBug16_CriticalRiskForceProceeds verifies CRITICAL risk proceeds with force=true
func TestBug16_CriticalRiskForceProceeds(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := strings.Repeat("A", 10)
	testFile := filepath.Join(tempDir, "critical_force.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, strings.Repeat("A", 10), strings.Repeat("B", 10), true, false)
	if err != nil {
		t.Fatalf("CRITICAL risk edit with force=true should succeed, got error: %v", err)
	}

	if result.ReplacementCount != 1 {
		t.Errorf("ReplacementCount: got %d, want 1", result.ReplacementCount)
	}
}

// TestBug16_LowRiskNoWarning verifies LOW risk edits have no warning
func TestBug16_LowRiskNoWarning(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Large file with small edit → LOW risk
	content := strings.Repeat("A", 500) + "HELLO" + strings.Repeat("B", 500)
	testFile := filepath.Join(tempDir, "low_risk.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, "HELLO", "WORLD", false, false)
	if err != nil {
		t.Fatalf("LOW risk edit should succeed, got error: %v", err)
	}

	if result.RiskWarning != "" {
		t.Errorf("RiskWarning should be empty for LOW risk, got: %s", result.RiskWarning)
	}
}

// TestBug16_BackupCreatedForCritical verifies backup exists for CRITICAL edits
// (Bug #22: no longer blocks, but backup is still created)
func TestBug16_BackupCreatedForCritical(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := strings.Repeat("A", 10)
	testFile := filepath.Join(tempDir, "backup_critical.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, strings.Repeat("A", 10), strings.Repeat("B", 10), false, false)
	if err != nil {
		t.Fatalf("CRITICAL edit should proceed (Bug #22): %v", err)
	}

	// Backup should be created
	if result.BackupID == "" {
		t.Error("Expected backup for CRITICAL edit, got empty")
	}
	// Risk warning should be attached
	if result.RiskWarning == "" {
		t.Error("Expected risk warning for CRITICAL edit, got empty")
	}
}

// TestBug16_MultiEditWithForce verifies MultiEdit accepts force param and creates backup
func TestBug16_MultiEditWithForce(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// Use larger content to avoid CRITICAL risk threshold on small files (Bug #17 added risk assessment)
	content := strings.Repeat("// padding\n", 20) + "line1\nline2\nline3\nline4\nline5\n" + strings.Repeat("// end\n", 20)
	testFile := filepath.Join(tempDir, "multi_edit.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	edits := []core.MultiEditOperation{
		{OldText: "line1", NewText: "modified1"},
		{OldText: "line3", NewText: "modified3"},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false, false)
	if err != nil {
		t.Fatalf("MultiEdit should succeed, got error: %v", err)
	}

	if result.SuccessfulEdits != 2 {
		t.Errorf("SuccessfulEdits: got %d, want 2", result.SuccessfulEdits)
	}

	// Verify backup ID is set (using BackupManager now)
	if result.BackupID == "" {
		t.Error("BackupID should be set for MultiEdit")
	}
}

// TestBug16_FormatRiskNotice verifies the informational notice format.
//
// History: this notice used to be an alarmist "⚠️ CRITICAL RISK (90%
// changed)" string emitted from a metric that double-counted bytes. It
// triggered emergency rollback behaviour against edits that were fine.
//
// The notice is now informational, lowercase, and uses an honest byte
// count. The verify hint is conditional ("if needed") and only at >=40%.
func TestBug16_FormatRiskNotice(t *testing.T) {
	thresholds := core.DefaultRiskThresholds()

	// MEDIUM risk: scope = max(200, 200) * 1 = 200, content = 600 → 33%.
	//
	// Constraints satisfied by these inputs:
	//   - TotalLines >= 10 → not a small file (escapes IsSmallFile branch)
	//   - CharactersChanged >= 200 → not a small file
	//   - 20% <= changePct < 40% → MEDIUM, but BELOW the verify-hint
	//     threshold (40%) so we can assert the hint is absent.
	//
	// Earlier draft used a content with no newlines, which made
	// TotalLines == 1 and triggered the small-file branch — assertions
	// for the normal branch did not match.
	content := strings.Repeat("A\n", 50) + strings.Repeat("B", 200) + strings.Repeat("\nC", 50) + strings.Repeat("D", 200)
	oldText := strings.Repeat("B", 200)
	newText := strings.Repeat("E", 200)

	impact := core.CalculateChangeImpact(content, oldText, newText, thresholds)

	notice := impact.FormatRiskNotice("test-backup-123")
	if notice == "" {
		t.Fatal("FormatRiskNotice should return non-empty string for MEDIUM risk")
	}

	// Tone: informational, not alarmist. Old format was "⚠️ MEDIUM RISK";
	// new format leads with lowercase "note:".
	if !strings.Contains(notice, "note:") {
		t.Errorf("notice should lead with informational \"note:\" prefix, got: %q", notice)
	}
	if strings.Contains(notice, "RISK") {
		t.Errorf("notice must not contain alarmist \"RISK\" word (regression of tone fix), got: %q", notice)
	}
	if strings.Contains(notice, "⚠") {
		t.Errorf("notice must not contain ⚠ emoji (regression of tone fix), got: %q", notice)
	}

	// The percentage must still be present so the reader can judge scope.
	if !strings.Contains(notice, "of file") {
		t.Errorf("notice should contain \"of file\" with the percentage, got: %q", notice)
	}

	// Backup ID must be referenced inline.
	if !strings.Contains(notice, "backup:test-backup-123") {
		t.Errorf("notice should reference backup id, got: %q", notice)
	}

	// MEDIUM (20%) is below the 40% verify-hint threshold — the hint must
	// NOT appear here.
	if strings.Contains(notice, "verify with") {
		t.Errorf("notice at MEDIUM (20%%) must not include verify hint, got: %q", notice)
	}
}

// TestBug16_FormatRiskNotice_HighRiskHasVerifyHint verifies that the verify
// hint appears at >=40% (the configured threshold) but with conditional
// phrasing rather than the previous imperative "VERIFY:" form.
func TestBug16_FormatRiskNotice_HighRiskHasVerifyHint(t *testing.T) {
	thresholds := core.DefaultRiskThresholds()

	// HIGH risk: max(400,400) * 1 = 400, content = 500 → 80%.
	content := strings.Repeat("A", 50) + strings.Repeat("B", 400) + strings.Repeat("C", 50)
	oldText := strings.Repeat("B", 400)
	newText := strings.Repeat("D", 400)

	impact := core.CalculateChangeImpact(content, oldText, newText, thresholds)

	notice := impact.FormatRiskNotice("test-backup-456", "/tmp/foo.txt")
	if notice == "" {
		t.Fatal("FormatRiskNotice should return non-empty string at 80% change")
	}

	if !strings.Contains(notice, "verify with") {
		t.Errorf("notice at 80%% must include verify hint, got: %q", notice)
	}
	// Conditional, not imperative.
	if !strings.Contains(notice, "if needed") {
		t.Errorf("verify hint must be conditional (\"if needed\"), got: %q", notice)
	}
	if strings.Contains(notice, "VERIFY:") {
		t.Errorf("notice must not use imperative \"VERIFY:\" wording, got: %q", notice)
	}
	// File path should be inlined into the hint when provided.
	if !strings.Contains(notice, "/tmp/foo.txt") {
		t.Errorf("verify hint should reference the file path when provided, got: %q", notice)
	}
}

// TestBug16_FormatRiskNotice_BelowMinIsSilent verifies that edits whose
// scope is below noticeMinPercent (10%%) emit no notice at all. The backup
// is still created — silence here means "not worth interrupting the user
// over", not "unprotected".
func TestBug16_FormatRiskNotice_BelowMinIsSilent(t *testing.T) {
	thresholds := core.DefaultRiskThresholds()

	// Build an impact at ~5% change: content=2000 bytes, edit touches 100.
	content := strings.Repeat("A", 1000) + strings.Repeat("B", 100) + strings.Repeat("C", 900)
	oldText := strings.Repeat("B", 100)
	newText := strings.Repeat("D", 100)

	impact := core.CalculateChangeImpact(content, oldText, newText, thresholds)

	notice := impact.FormatRiskNotice("test-backup-789")
	if notice != "" {
		t.Errorf("notice should be empty for edit at %.1f%% (below 10%% threshold), got: %q",
			impact.ChangePercentage, notice)
	}
}
