package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderDiff_Formats(t *testing.T) {
	old := "a\nb\nc\n"
	neu := "a\nX\nc\n"

	if got := RenderDiff(old, neu, "f.txt", "none"); got != "" {
		t.Errorf("none format should be empty, got %q", got)
	}
	if got := RenderDiff(old, neu, "f.txt", "stat"); got != "+1 -1" {
		t.Errorf("stat format = %q, want %q", got, "+1 -1")
	}
	full := RenderDiff(old, neu, "f.txt", "full")
	if !strings.Contains(full, "@@") || !strings.Contains(full, "+X") || !strings.Contains(full, "-b") {
		t.Errorf("full format missing expected hunk content: %q", full)
	}
	// No-change returns empty regardless of format.
	if got := RenderDiff(old, old, "f.txt", "full"); got != "" {
		t.Errorf("identical content should yield empty diff, got %q", got)
	}
}

// TestRenderDiff_AutoCollapsesLargeDiff verifies the token-saving default
// (point 1): a large block deletion collapses to a summary with an elision
// marker instead of dumping every removed line.
func TestRenderDiff_AutoCollapsesLargeDiff(t *testing.T) {
	var oldB strings.Builder
	for i := 0; i < 300; i++ {
		oldB.WriteString(fmt.Sprintf("line %d\n", i))
	}
	old := oldB.String()

	// Remove a big middle block (lines 25..275) → ~251 removed lines.
	var newB strings.Builder
	for i := 0; i < 300; i++ {
		if i >= 25 && i < 275 {
			continue
		}
		newB.WriteString(fmt.Sprintf("line %d\n", i))
	}
	neu := newB.String()

	auto := RenderDiff(old, neu, "big.txt", "")
	full := RenderDiff(old, neu, "big.txt", "full")

	if !strings.Contains(auto, "more line(s)") {
		t.Errorf("auto format should elide the middle of a large hunk; got:\n%s", auto)
	}
	if !strings.Contains(auto, "diff_format:\"full\"") {
		t.Error("auto format should hint how to see the full diff")
	}
	if len(auto) >= len(full) {
		t.Errorf("auto (summary) should be shorter than full: auto=%d full=%d", len(auto), len(full))
	}
	if strings.Count(full, "\n") <= diffAutoSummaryThreshold {
		t.Fatalf("test setup: full diff should exceed the auto threshold (%d), got %d lines",
			diffAutoSummaryThreshold, strings.Count(full, "\n"))
	}

	// Explicit summary should not carry the auto hint but should still elide.
	summary := RenderDiff(old, neu, "big.txt", "summary")
	if !strings.Contains(summary, "more line(s)") {
		t.Error("summary format should elide large hunks")
	}
}
