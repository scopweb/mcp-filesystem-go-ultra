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

// Bug #23: CRLF/LF Mismatch in Edit Operations
// When a file uses CRLF (\r\n) and Claude Desktop sends old_text with LF (\n),
// the risk assessment should still find matches (not report 0 matches / CRITICAL rewrite).

func setupBug23Engine(t *testing.T) (*core.UltraFastEngine, string) {
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

func TestBug23_EditFile_CRLFMatchesLF(t *testing.T) {
	engine, tempDir := setupBug23Engine(t)

	// Create file with CRLF line endings
	filePath := filepath.Join(tempDir, "crlf_test.txt")
	content := "line1\r\nline2\r\nline3\r\n"
	os.WriteFile(filePath, []byte(content), 0644)

	// Edit with LF-only old_text (as Claude Desktop would send)
	result, err := engine.EditFile(context.Background(), filePath, "line2\n", "REPLACED\n", true, false)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}

	if result.ReplacementCount == 0 {
		t.Error("Expected at least 1 replacement, got 0 — CRLF/LF mismatch not handled")
	}

	// Verify file was actually modified
	modified, _ := os.ReadFile(filePath)
	if !strings.Contains(string(modified), "REPLACED") {
		t.Error("File was not modified — edit did not apply")
	}
}

func TestBug23_EditFile_CRLFRiskNotCritical(t *testing.T) {
	engine, tempDir := setupBug23Engine(t)

	// Create a larger CRLF file where a small edit should be LOW risk
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("// This is a comment line that is fairly long to create content\r\n")
	}
	sb.WriteString("TARGET_LINE_TO_REPLACE\r\n")
	for i := 0; i < 50; i++ {
		sb.WriteString("// More content lines for padding in the test file here\r\n")
	}

	filePath := filepath.Join(tempDir, "crlf_risk.txt")
	os.WriteFile(filePath, []byte(sb.String()), 0644)

	// Edit one line with LF old_text (no \r)
	result, err := engine.EditFile(context.Background(), filePath, "TARGET_LINE_TO_REPLACE\n", "REPLACED_SUCCESSFULLY\n", false, false)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}

	// Should NOT be critical risk (small change in large file)
	if result.RiskWarning != "" && strings.Contains(strings.ToUpper(result.RiskWarning), "CRITICAL") {
		t.Errorf("Small edit in CRLF file incorrectly flagged as CRITICAL: %s", result.RiskWarning)
	}

	if result.ReplacementCount == 0 {
		t.Error("Expected replacement to succeed")
	}
}

func TestBug23_MultiEdit_CRLFMatchesLF(t *testing.T) {
	engine, tempDir := setupBug23Engine(t)

	// Create CRLF file
	filePath := filepath.Join(tempDir, "crlf_multi.txt")
	content := "alpha\r\nbeta\r\ngamma\r\n"
	os.WriteFile(filePath, []byte(content), 0644)

	edits := []core.MultiEditOperation{
		{OldText: "alpha\n", NewText: "ALPHA\n"},
		{OldText: "gamma\n", NewText: "GAMMA\n"},
	}

	result, err := engine.MultiEdit(context.Background(), filePath, edits, true)
	if err != nil {
		t.Fatalf("MultiEdit failed: %v", err)
	}

	if result.SuccessfulEdits == 0 {
		t.Error("Expected successful edits, got 0 — CRLF/LF mismatch in multi_edit")
	}

	modified, _ := os.ReadFile(filePath)
	modStr := string(modified)
	if !strings.Contains(modStr, "ALPHA") || !strings.Contains(modStr, "GAMMA") {
		t.Errorf("Multi-edit did not apply correctly. Content: %q", modStr)
	}
}

func TestBug23_PureLF_StillWorks(t *testing.T) {
	engine, tempDir := setupBug23Engine(t)

	// Create file with LF line endings (Unix-style)
	filePath := filepath.Join(tempDir, "lf_test.txt")
	content := "line1\nline2\nline3\n"
	os.WriteFile(filePath, []byte(content), 0644)

	// Edit with LF old_text
	result, err := engine.EditFile(context.Background(), filePath, "line2\n", "REPLACED\n", false, false)
	if err != nil {
		t.Fatalf("EditFile failed on LF file: %v", err)
	}

	if result.ReplacementCount == 0 {
		t.Error("Expected replacement to succeed on LF file")
	}
}
