package main

import (
	"testing"
)

// TestIssue202607_StructuredDiffCounts verifies editStructuredFromContents uses
// real diff counts for search_replace/regex/occurrence modes.
func TestIssue202607_StructuredDiffCounts(t *testing.T) {
	oldContent := "aaa\nbbb\nccc\n"
	newContent := "aaa\nBBB\nccc\n"
	m := editStructuredFromContents("/tmp/block.txt", oldContent, newContent, 1, 3, 3, "")

	if got := m["lines_added"]; got != 1 {
		t.Errorf("editStructuredFromContents lines_added = %v, want 1", got)
	}
	if got := m["lines_removed"]; got != 1 {
		t.Errorf("editStructuredFromContents lines_removed = %v, want 1", got)
	}
	if got := m["total_lines"]; got != 3 {
		t.Errorf("editStructuredFromContents total_lines = %v, want 3", got)
	}
}
