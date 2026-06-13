package main

// Regression tests for issue #21 — multi_edit risk notice displays
// "0 replacements" instead of the real edit count, and the displayed
// "% of file" can exceed 100% (e.g. "137% of file") when the honest-
// scope formula's numerator is larger than the file size.
//
// White-box diagnosis (see issue body for full trace):
//   - calculateMultiEditImpact never assigned impact.Occurrences, so
//     FormatRiskNotice printed the Go zero value ("0 replacements").
//   - The honest-scope formula Σ max(|oldText|,|newText|) / fileSize
//     is a correct upper bound but produces >100% on net insertions;
//     the notice formatter did not clamp it, so the user-visible text
//     read as "the file changed by more than 100%" which is wrong.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// setupOccurrencesEngine creates a test engine for the multi_edit
// occurrences-counter regression tests (issue #21).
func setupOccurrencesEngine(t *testing.T) (*core.UltraFastEngine, string) {
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
		t.Fatalf("Failed to create engine: %v", err)
	}
	return engine, tempDir
}

// TestIssue21_MultiEditNoticeReplacementsNotZero verifies that after a
// multi_edit that applied N edits, the RiskWarning's "replacements" count
// reflects N (not 0). Before the fix, calculateMultiEditImpact left
// aggregateImpact.Occurrences at its Go zero value, and FormatRiskNotice
// printed "0 replacements" — the same number whether the edits applied
// or not. This was misleading: a caller reading "0 replacements" could
// reasonably conclude the operation was a no-op, when in fact the file
// had been modified.
func TestIssue21_MultiEditNoticeReplacementsNotZero(t *testing.T) {
	engine, tempDir := setupOccurrencesEngine(t)
	ctx := context.Background()

	// Build a 212-byte file with 4 unique anchors separated by padding.
	// Each edit replaces a 7-byte anchor with a 40-byte replacement, so
	// scope per edit is 40 bytes → 4 × 40 = 160 bytes total scope.
	// ChangePercentage ≈ 160/212 ≈ 75% — well above the 20% medium
	// threshold, which forces FormatRiskNotice to emit the risk line.
	var b strings.Builder
	b.WriteString(strings.Repeat("X", 47))
	b.WriteString("ANCHOR1")
	b.WriteString(strings.Repeat("X", 47))
	b.WriteString("ANCHOR2")
	b.WriteString(strings.Repeat("X", 47))
	b.WriteString("ANCHOR3")
	b.WriteString(strings.Repeat("X", 47))
	b.WriteString("ANCHOR4")
	filePath := filepath.Join(tempDir, "occurrence_test.txt")
	if err := os.WriteFile(filePath, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	replacement := "ANCHOR_RENAMED_WITH_LONGER_PAYLOAD"
	edits := []core.MultiEditOperation{
		{OldText: "ANCHOR1", NewText: replacement},
		{OldText: "ANCHOR2", NewText: replacement},
		{OldText: "ANCHOR3", NewText: replacement},
		{OldText: "ANCHOR4", NewText: replacement},
	}

	result, err := engine.MultiEdit(ctx, filePath, edits, false, false, false)
	if err != nil {
		t.Fatalf("MultiEdit returned error: %v", err)
	}
	if result.SuccessfulEdits != 4 {
		t.Fatalf("fixture sanity: expected 4 successful edits, got %d (errors=%v)",
			result.SuccessfulEdits, result.Errors)
	}
	if result.RiskWarning == "" {
		t.Fatalf("fixture sanity: RiskWarning not emitted — adjust the seed so the change exceeds the medium threshold. Got %d edits, %d lines, %d total lines.",
			result.SuccessfulEdits, result.LinesAffected, result.TotalLines)
	}

	// Bug-fix assertion: the notice must NOT say "0 replacements" when 4
	// edits applied. The fix sets aggregateImpact.Occurrences from the real
	// per-edit ReplacementCount sum.
	if strings.Contains(result.RiskWarning, "0 replacements") {
		t.Errorf("RiskWarning still shows '0 replacements' despite %d successful edits:\n%s",
			result.SuccessfulEdits, result.RiskWarning)
	}
	if !strings.Contains(result.RiskWarning, "4 replacements") {
		t.Errorf("RiskWarning should mention '4 replacements' (one per applied edit), got:\n%s",
			result.RiskWarning)
	}

	// Sanity: every edit renamed exactly one occurrence (the token is
	// unique per line), so the per-edit ReplacementCount is 1, and the
	// sum is 4. The notice text confirms that.
	t.Logf("RiskWarning emitted:\n%s", result.RiskWarning)
}

// TestIssue21_RiskNoticeClampsPercentAt100 verifies that when the
// honest-scope formula yields a ChangePercentage > 100, the notice
// string is capped at "100% of file". The change_percentage field and
// the RiskLevel are not modified — only the displayed value.
//
// Honest-scope can exceed 100% on net insertions: replacing a 6-byte
// anchor with a 600-byte blob in a 200-byte file yields
// (6 + 600) / 200 × 100 = 303% (the formula uses max(old,new), so it's
// 600/200 = 300%). The internal RiskLevel and CharactersChanged remain
// the honest upper bound; only the user-visible "X% of file" text is
// clamped, so the magnitude word ("very large edit") still encodes
// severity.
func TestIssue21_RiskNoticeClampsPercentAt100(t *testing.T) {
	engine, tempDir := setupOccurrencesEngine(t)
	ctx := context.Background()

	// 200-byte file with a single, unique anchor.
	// Layout: 97 X's, "ANCHOR" (6 bytes), 97 X's → 200 bytes.
	var b strings.Builder
	b.WriteString(strings.Repeat("X", 97))
	b.WriteString("ANCHOR")
	b.WriteString(strings.Repeat("X", 97))
	filePath := filepath.Join(tempDir, "scope_overshoot.txt")
	if err := os.WriteFile(filePath, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	edits := []core.MultiEditOperation{
		{
			OldText: "ANCHOR",                 // 6 bytes
			NewText: strings.Repeat("Y", 600), // 600 bytes — net insertion
		},
	}

	result, err := engine.MultiEdit(ctx, filePath, edits, false, false, false)
	if err != nil {
		t.Fatalf("MultiEdit returned error: %v", err)
	}
	if result.SuccessfulEdits != 1 {
		t.Fatalf("fixture sanity: expected 1 successful edit, got %d (errors=%v)",
			result.SuccessfulEdits, result.Errors)
	}
	if result.RiskWarning == "" {
		t.Fatalf("fixture sanity: RiskWarning not emitted — adjust the seed so the change exceeds 10%% of the file")
	}

	// The honest-scope would be 300% here. The notice must NOT show the
	// raw uncapped value. Allowed forms: "100% of file".
	for _, bad := range []string{"300%", "299%", "298%"} {
		if strings.Contains(result.RiskWarning, bad) {
			t.Errorf("RiskWarning contains uncapped percentage %q (should be clamped to 100%%):\n%s",
				bad, result.RiskWarning)
		}
	}
	if !strings.Contains(result.RiskWarning, "100% of file") {
		t.Errorf("RiskWarning should contain '100%% of file' (the cap), got:\n%s",
			result.RiskWarning)
	}

	// Magnitude must still read as a high-severity edit (the cap is
	// display-only; the RiskLevel and magnitude are unchanged).
	if !strings.Contains(result.RiskWarning, "very large edit") &&
		!strings.Contains(result.RiskWarning, "large edit") {
		t.Errorf("RiskWarning should still convey severity via magnitude word, got:\n%s",
			result.RiskWarning)
	}

	t.Logf("RiskWarning emitted:\n%s", result.RiskWarning)
}
