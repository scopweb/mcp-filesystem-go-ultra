package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestAutoTruncateLargeFile_SmallFile(t *testing.T) {
	// File <= threshold — must be returned unchanged
	var b strings.Builder
	for i := 1; i <= autoTruncateLargeFileLines; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	content := b.String()
	result := autoTruncateLargeFile(content, "small.go")
	if result != content {
		t.Errorf("Small file should be returned unchanged, got different content")
	}
	if strings.Contains(result, "total lines") {
		t.Errorf("Small file should not have a footer, but got one")
	}
}

func TestAutoTruncateLargeFile_LargeFile(t *testing.T) {
	// File with autoTruncateLargeFileLines+100 lines — must be truncated
	total := autoTruncateLargeFileLines + 100
	var b strings.Builder
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	content := b.String()
	result := autoTruncateLargeFile(content, "big.cs")

	if !strings.Contains(result, fmt.Sprintf("of %d total lines", total)) {
		t.Errorf("Footer must state the real total (%d), got:\n%s", total, result[max(0, len(result)-200):])
	}
	if !strings.Contains(result, "big.cs") {
		t.Errorf("Footer must include the filename")
	}
	if !strings.Contains(result, "start_line") {
		t.Errorf("Footer must mention start_line param")
	}

	// Result must NOT contain lines beyond the threshold
	resultLines := strings.Split(result, "\n")
	// +2 for the blank separator line and the footer line itself
	if len(resultLines) > autoTruncateLargeFileLines+3 {
		t.Errorf("Result has too many lines: %d (threshold=%d)", len(resultLines), autoTruncateLargeFileLines)
	}

	// The original lines beyond the threshold must NOT appear in the result
	overThreshold := fmt.Sprintf("line %d", autoTruncateLargeFileLines+1)
	if strings.Contains(result, overThreshold) {
		t.Errorf("Result contains a line that should have been truncated: %q", overThreshold)
	}
}

func TestAutoTruncateLargeFile_FooterFormat(t *testing.T) {
	// Verify the footer mirrors ReadFileRange style: [Lines 1-N of TOTAL ...]
	total := autoTruncateLargeFileLines + 50
	var b strings.Builder
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&b, "x\n")
	}
	result := autoTruncateLargeFile(b.String(), "service.go")

	expected := fmt.Sprintf("[Lines 1-%d of %d total lines in service.go", autoTruncateLargeFileLines, total)
	if !strings.Contains(result, expected) {
		t.Errorf("Footer format mismatch.\nExpected substring: %q\nGot (tail):\n%s", expected, result[max(0, len(result)-300):])
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestTruncateLineWidths_NoOp(t *testing.T) {
	in := "short\nalso short"
	if got := truncateLineWidths(in, 0); got != in {
		t.Errorf("maxLen=0 must be no-op, got %q", got)
	}
	if got := truncateLineWidths(in, 100); got != in {
		t.Errorf("short lines must be unchanged, got %q", got)
	}
	if got := truncateLineWidths("", 10); got != "" {
		t.Errorf("empty must stay empty, got %q", got)
	}
}

func TestTruncateLineWidths_CutsAndFooters(t *testing.T) {
	long := strings.Repeat("x", 50)
	in := "ok\n" + long + "\nend"
	got := truncateLineWidths(in, 10)
	if !strings.Contains(got, "ok\n") {
		t.Errorf("short line must survive: %q", got)
	}
	if strings.Contains(got, long) {
		t.Errorf("long line must be cut: %q", got)
	}
	if !strings.Contains(got, "xxxxxxxxxx…") {
		t.Errorf("cut line must keep 10 runes plus ellipsis: %q", got)
	}
	if !strings.Contains(got, "[truncated 1 line(s) to max_line_length=10]") {
		t.Errorf("missing footer: %q", got)
	}
}
