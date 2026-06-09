package core

import "strings"

// WhitespaceMatch represents a match of oldText in content under tolerant
// whitespace semantics. StartOrig and EndOrig are byte offsets in the
// ORIGINAL (unnormalized) content, with EndOrig exclusive (Go slice convention).
type WhitespaceMatch struct {
	StartOrig int
	EndOrig   int
}

// DefaultIndentSize is the number of spaces one tab expands to during
// tolerant matching. Matches the convention used by normalizeIndentation
// elsewhere in the package.
const DefaultIndentSize = 4

// normalizeForTolerantMatch normalizes s for whitespace-tolerant comparison:
//
//	\r\n         → \n
//	lone \r      → \n
//	\t           → `indentSize` spaces
//	other bytes  → unchanged
//
// Returns the normalized string and a byteMap where byteMap[i] is the byte
// offset in the ORIGINAL string that produced normalized byte i. For bytes
// produced by tab expansion, byteMap points to the original tab byte. For
// CRLF, byteMap points to the LAST byte consumed (the LF) so that
// map[end]+1 gives a correct exclusive end that spans the whole CRLF.
func normalizeForTolerantMatch(s string, indentSize int) (string, []int) {
	if indentSize <= 0 {
		indentSize = DefaultIndentSize
	}
	// Worst case: every byte becomes `indentSize` bytes (a tab). Avoid
	// pre-allocating that much for typical files; len(s) is a better default.
	normalized := make([]byte, 0, len(s))
	byteMap := make([]int, 0, len(s))

	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case '\r':
			// CRLF or lone CR — both normalize to LF.
			// Point byteMap at the LAST byte consumed so endOrig = map+1 spans
			// the whole line break (CRLF is 2 bytes, not 1).
			consumed := 1
			if i+1 < len(s) && s[i+1] == '\n' {
				consumed = 2
			}
			normalized = append(normalized, '\n')
			byteMap = append(byteMap, i+consumed-1)
			i += consumed
		case '\t':
			// Tab expands to N spaces. All spaces point to the same tab byte,
			// so map+1 also gives a correct exclusive end (single tab byte).
			for j := 0; j < indentSize; j++ {
				normalized = append(normalized, ' ')
				byteMap = append(byteMap, i)
			}
			i++
		default:
			normalized = append(normalized, c)
			byteMap = append(byteMap, i)
			i++
		}
	}
	return string(normalized), byteMap
}

// findAllTolerantMatches returns all non-overlapping occurrences of oldText
// in content under whitespace-tolerant semantics. Matches are byte ranges in
// the original (unnormalized) content.
//
// Returns nil if oldText is empty or no match is found.
func findAllTolerantMatches(content, oldText string) []WhitespaceMatch {
	if oldText == "" {
		return nil
	}
	normContent, contentMap := normalizeForTolerantMatch(content, DefaultIndentSize)
	normOld, _ := normalizeForTolerantMatch(oldText, DefaultIndentSize)
	if normOld == "" {
		return nil
	}

	var matches []WhitespaceMatch
	searchFrom := 0
	for {
		idx := strings.Index(normContent[searchFrom:], normOld)
		if idx < 0 {
			break
		}
		idx += searchFrom

		// Defensive bounds — should never trip if normalizeForTolerantMatch
		// and strings.Index are consistent.
		if idx >= len(contentMap) {
			break
		}
		startOrig := contentMap[idx]
		endNorm := idx + len(normOld) - 1
		if endNorm >= len(contentMap) {
			break
		}
		endOrig := contentMap[endNorm] + 1 // exclusive

		matches = append(matches, WhitespaceMatch{StartOrig: startOrig, EndOrig: endOrig})
		searchFrom = idx + len(normOld)
		if searchFrom > len(normContent) {
			break
		}
	}
	return matches
}

// applyTolerantMatches replaces all matched ranges in content with newText.
// Returns the modified content and the number of replacements applied.
//
// Matches must be non-overlapping. Overlapping matches are silently dropped
// (a defensive guard; the finder never produces them).
func applyTolerantMatches(content string, matches []WhitespaceMatch, newText string) (string, int) {
	if len(matches) == 0 {
		return content, 0
	}
	// Sort matches by start position. The finder already returns them sorted,
	// but a defensive sort keeps the helper correct in isolation.
	sorted := make([]WhitespaceMatch, len(matches))
	copy(sorted, matches)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].StartOrig < sorted[j-1].StartOrig; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	// Pre-size the builder: original length + N*(len(newText) - avg match size).
	// Use a conservative upper bound to avoid reallocations.
	var totalMatchLen int
	for _, m := range sorted {
		totalMatchLen += m.EndOrig - m.StartOrig
	}
	estimated := len(content) - totalMatchLen + len(newText)*len(sorted)
	if estimated < len(content) {
		estimated = len(content) // underflow guard
	}

	var sb strings.Builder
	sb.Grow(estimated)
	cursor := 0
	applied := 0
	for _, m := range sorted {
		if m.StartOrig < cursor {
			// Overlapping — skip defensively.
			continue
		}
		sb.WriteString(content[cursor:m.StartOrig])
		sb.WriteString(newText)
		cursor = m.EndOrig
		applied++
	}
	sb.WriteString(content[cursor:])
	return sb.String(), applied
}
