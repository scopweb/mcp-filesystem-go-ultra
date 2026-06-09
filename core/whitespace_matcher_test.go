package core

import (
	"strings"
	"testing"
)

// TestWhitespaceMatcher_TabsVsSpaces verifies that a tab in the file
// matches 4 spaces in the pattern (and vice versa).
func TestWhitespaceMatcher_TabsVsSpaces(t *testing.T) {
	tests := []struct {
		name    string
		content string
		pattern string
		want    int // number of matches expected
	}{
		{
			name:    "tab in file, 4 spaces in pattern",
			content: "function foo() {\n\tbar();\n}",
			pattern: "    bar();",
			want:    1,
		},
		{
			name:    "4 spaces in file, tab in pattern",
			content: "function foo() {\n    bar();\n}",
			pattern: "\tbar();",
			want:    1,
		},
		{
			name:    "no whitespace difference, exact match",
			content: "function foo() {\n    bar();\n}",
			pattern: "    bar();",
			want:    1,
		},
		{
			name:    "multiple tabs in file, multiple spaces in pattern",
			content: "\t\tvar x = 1;",
			pattern: "        var x = 1;", // 8 spaces
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := findAllTolerantMatches(tt.content, tt.pattern)
			if len(matches) != tt.want {
				t.Errorf("got %d matches, want %d", len(matches), tt.want)
			}
		})
	}
}

// TestWhitespaceMatcher_CRLFvsLF verifies that CRLF in the file matches
// LF in the pattern (and vice versa).
func TestWhitespaceMatcher_CRLFvsLF(t *testing.T) {
	tests := []struct {
		name    string
		content string
		pattern string
		want    int
	}{
		{
			name:    "CRLF in file, LF in pattern",
			content: "line1\r\nline2\r\nline3",
			pattern: "line1\nline2",
			want:    1,
		},
		{
			name:    "LF in file, CRLF in pattern",
			content: "line1\nline2\nline3",
			pattern: "line1\r\nline2",
			want:    1,
		},
		{
			name:    "lone CR in file, LF in pattern",
			content: "line1\rline2",
			pattern: "line1\nline2",
			want:    1,
		},
		{
			name:    "mixed CRLF and LF in file",
			content: "line1\r\nline2\nline3\rline4",
			pattern: "line1\nline2\nline3\nline4",
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := findAllTolerantMatches(tt.content, tt.pattern)
			if len(matches) != tt.want {
				t.Errorf("got %d matches, want %d", len(matches), tt.want)
			}
		})
	}
}

// TestWhitespaceMatcher_MultipleMatches verifies that all non-overlapping
// matches are returned, not just the first.
func TestWhitespaceMatcher_MultipleMatches(t *testing.T) {
	content := "\tfoo();\n\tbar();\n\tbaz();\n"
	pattern := "    "
	matches := findAllTolerantMatches(content, pattern)
	if len(matches) != 3 {
		t.Errorf("got %d matches, want 3", len(matches))
	}
	// Verify each match points to a single tab byte (4 spaces → 1 tab).
	for i, m := range matches {
		if m.EndOrig-m.StartOrig != 1 {
			t.Errorf("match %d: span = %d, want 1 (single tab byte)", i, m.EndOrig-m.StartOrig)
		}
	}
}

// TestWhitespaceMatcher_PreservesByteRange verifies that EndOrig correctly
// points one past the last original byte consumed by the match.
func TestWhitespaceMatcher_PreservesByteRange(t *testing.T) {
	content := "a\r\nb"
	pattern := "a\nb"
	matches := findAllTolerantMatches(content, pattern)
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if matches[0].StartOrig != 0 || matches[0].EndOrig != 4 {
		t.Errorf("got range [%d, %d), want [0, 4)", matches[0].StartOrig, matches[0].EndOrig)
	}
	if content[matches[0].StartOrig:matches[0].EndOrig] != "a\r\nb" {
		t.Errorf("range slice = %q, want %q", content[matches[0].StartOrig:matches[0].EndOrig], "a\r\nb")
	}
}

// TestWhitespaceMatcher_UTF8Preserved verifies that multi-byte UTF-8
// characters in the file don't break byte indexing.
func TestWhitespaceMatcher_UTF8Preserved(t *testing.T) {
	content := "// café: 漢字\nfunction foo() {\n\tbar();\n}"
	pattern := "function foo() {\n    bar();\n}"
	matches := findAllTolerantMatches(content, pattern)
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	// The match starts at the "f" of "function" and ends at the "}"
	if !strings.HasPrefix(content[matches[0].StartOrig:], "function foo()") {
		t.Errorf("match slice should start with 'function foo()'")
	}
	if !strings.HasSuffix(content[:matches[0].EndOrig], "}") {
		t.Errorf("match slice should end with '}'")
	}
}

// TestWhitespaceMatcher_NoMatch verifies the finder returns nil when the
// pattern is absent.
func TestWhitespaceMatcher_NoMatch(t *testing.T) {
	matches := findAllTolerantMatches("hello world", "goodbye")
	if len(matches) != 0 {
		t.Errorf("got %d matches, want 0", len(matches))
	}
}

// TestWhitespaceMatcher_EmptyPattern verifies the finder refuses empty
// patterns (which would be an infinite match loop).
func TestWhitespaceMatcher_EmptyPattern(t *testing.T) {
	matches := findAllTolerantMatches("hello", "")
	if matches != nil {
		t.Errorf("got %v, want nil for empty pattern", matches)
	}
}

// TestApplyTolerantMatches_ReplacesAll verifies the helper correctly
// applies newText to all matches.
func TestApplyTolerantMatches_ReplacesAll(t *testing.T) {
	content := "\tfoo\n\tbar\n\tbaz\n"
	pattern := "    "
	newText := "  " // 2 spaces
	result, count := applyTolerantMatches(content, findAllTolerantMatches(content, pattern), newText)
	if count != 3 {
		t.Errorf("replacement count = %d, want 3", count)
	}
	expected := "  foo\n  bar\n  baz\n"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

// TestApplyTolerantMatches_PreservesUntouched verifies that bytes
// outside the matches are preserved exactly.
func TestApplyTolerantMatches_PreservesUntouched(t *testing.T) {
	content := "before\r\n\tafter"
	pattern := "    "
	result, count := applyTolerantMatches(content, findAllTolerantMatches(content, pattern), "XX")
	if count != 1 {
		t.Errorf("replacement count = %d, want 1", count)
	}
	if result != "before\r\nXXafter" {
		t.Errorf("got %q, want %q", result, "before\r\nXXafter")
	}
}

// TestPerformIntelligentEdit_TolerantMode is an end-to-end test that
// verifies the tolerant mode is plumbed through performIntelligentEdit
// and correctly finds a match that exact mode would miss — specifically
// when a tab appears in the MIDDLE of a line (not just leading). The
// existing OPTIMIZATION 7 only handles leading-whitespace differences
// (it normalizes only line-leading whitespace); tolerant mode handles
// tabs anywhere on the line.
func TestPerformIntelligentEdit_TolerantMode(t *testing.T) {
	engine, _ := newTestEngineForMinifier(t)
	// File uses a tab between the comma and the variable name. The
	// caller's pattern uses 4 spaces (typical of typed-from-IDE text).
	content := "call(a,\tb);"

	// Without tolerant: exact match must fail (tab ≠ 4 spaces). The
	// leading-only fallback in OPTIMIZATION 7 also fails because the
	// tab is not line-leading.
	_, err := engine.performIntelligentEdit(content, "call(a,    b);", "call(a,REPLACED);", false)
	if err == nil {
		t.Errorf("expected error in non-tolerant mode (tab in middle of line), got nil")
	}

	// With tolerant: should succeed and preserve the surrounding bytes.
	res, err := engine.performIntelligentEdit(content, "call(a,    b);", "call(a,REPLACED);", true)
	if err != nil {
		t.Fatalf("tolerant mode failed: %v", err)
	}
	if res.ReplacementCount != 1 {
		t.Errorf("got %d replacements, want 1", res.ReplacementCount)
	}
	expected := "call(a,REPLACED);"
	if res.ModifiedContent != expected {
		t.Errorf("got %q, want %q", res.ModifiedContent, expected)
	}
}

// newTestEngineForMinifier constructs a minimal engine for tests that
// only need the performIntelligentEdit path. It does not configure
// backup, audit, or cache.
func newTestEngineForMinifier(t *testing.T) (*UltraFastEngine, *TestStats) {
	t.Helper()
	// We don't need a fully-wired engine; performIntelligentEdit only
	// reads from its arguments and the engine's riskThresholds. Use a
	// zero-value engine for simplicity.
	engine := &UltraFastEngine{}
	// The riskThresholds field is required by CalculateChangeImpact;
	// the zero value works for our tests (no blocking).
	return engine, nil
}

// TestStats is a placeholder for any future test metrics. Currently
// unused; exists so newTestEngineForMinifier can return a second value
// for symmetry with other test helpers without an "unused return" lint.
type TestStats struct{}
