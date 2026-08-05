package core

import "strings"

// preserveBoundaryNewline guards the line boundary at the end of a splice
// match (feedback 2026-08-05, BUG 1). When the matched text ends with "\n"
// but the replacement does not, and there is content after the match, the
// next original line would be fused onto the last line of the replacement.
// This helper appends the missing "\n" so bytes outside the match keep their
// line structure. Callers must pass LF-normalized strings.
func preserveBoundaryNewline(content string, matchEnd int, matchedText, replacement string) string {
	if !strings.HasSuffix(matchedText, "\n") || strings.HasSuffix(replacement, "\n") {
		return replacement
	}
	if matchEnd >= len(content) {
		return replacement
	}
	return replacement + "\n"
}

// replaceAllPreserveBoundary is strings.ReplaceAll with boundary-newline
// preservation applied per match (see preserveBoundaryNewline).
func replaceAllPreserveBoundary(content, oldText, newText string) string {
	if !strings.HasSuffix(oldText, "\n") || strings.HasSuffix(newText, "\n") {
		return strings.ReplaceAll(content, oldText, newText)
	}
	var sb strings.Builder
	sb.Grow(len(content))
	last := 0
	for {
		idx := strings.Index(content[last:], oldText)
		if idx < 0 {
			break
		}
		matchStart := last + idx
		matchEnd := matchStart + len(oldText)
		sb.WriteString(content[last:matchStart])
		sb.WriteString(preserveBoundaryNewline(content, matchEnd, oldText, newText))
		last = matchEnd
	}
	sb.WriteString(content[last:])
	return sb.String()
}
