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
			// CharactersChanged = (100+100) = 200, content = 500 → 40% = MEDIUM
			name:        "MEDIUM risk - no block",
			content:     strings.Repeat("A", 200) + strings.Repeat("B", 100) + strings.Repeat("C", 200),
			oldText:     strings.Repeat("B", 100),
			newText:     strings.Repeat("D", 100),
			expectBlock: false,
			expectRisk:  "medium",
		},
		{
			// CharactersChanged = (200+200) = 400, content = 500 → 80% = HIGH
			name:        "HIGH risk - no block",
			content:     strings.Repeat("A", 150) + strings.Repeat("B", 200) + strings.Repeat("C", 150),
			oldText:     strings.Repeat("B", 200),
			newText:     strings.Repeat("D", 200),
			expectBlock: false,
			expectRisk:  "high",
		},
		{
			name:        "CRITICAL risk - BLOCKS",
			content:     strings.Repeat("A", 10),
			oldText:     strings.Repeat("A", 10),
			newText:     strings.Repeat("B", 10),
			expectBlock: true,
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

	// CharactersChanged = (100+100) = 200, content = 500 → 40% = MEDIUM
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

	// CharactersChanged = (200+200) = 400, content = 500 → 80% = HIGH
	content := strings.Repeat("A", 150) + strings.Repeat("B", 200) + strings.Repeat("C", 150)
	testFile := filepath.Join(tempDir, "high_risk.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := engine.EditFile(context.Background(), testFile, strings.Repeat("B", 200), strings.Repeat("D", 200), false, false)
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

// TestBug16_FormatRiskNotice verifies non-blocking warning format
func TestBug16_FormatRiskNotice(t *testing.T) {
	thresholds := core.DefaultRiskThresholds()

	// Create a MEDIUM risk impact: CharactersChanged = (100+100) = 200, content = 500 → 40%
	content := strings.Repeat("A", 200) + strings.Repeat("B", 100) + strings.Repeat("C", 200)
	oldText := strings.Repeat("B", 100)
	newText := strings.Repeat("D", 100)

	impact := core.CalculateChangeImpact(content, oldText, newText, thresholds)

	notice := impact.FormatRiskNotice("test-backup-123")
	if notice == "" {
		t.Fatal("FormatRiskNotice should return non-empty string for MEDIUM risk")
	}

	// Notice should contain risk level and percentage (UNDO is now in main response, not in notice)
	if !strings.Contains(notice, "RISK") {
		t.Error("Notice should contain RISK level")
	}
	if !strings.Contains(notice, "changed") {
		t.Error("Notice should contain change percentage")
	}
}
