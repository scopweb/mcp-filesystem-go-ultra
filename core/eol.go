package core

import "strings"

func countEOLStyles(content string) (crlf, lf, cr int) {
	crlf = strings.Count(content, "\r\n")
	lf = strings.Count(content, "\n") - crlf
	cr = strings.Count(content, "\r") - crlf
	return
}

func firstEOL(content string) string {
	for i := 0; i < len(content); i++ {
		if content[i] == '\r' {
			if i+1 < len(content) && content[i+1] == '\n' {
				return "\r\n"
			}
			return "\r"
		}
		if content[i] == '\n' {
			return "\n"
		}
	}
	return "\n"
}

func dominantEOL(content string) string {
	crlf, lf, cr := countEOLStyles(content)
	if crlf+lf+cr == 0 {
		return "\n"
	}
	max := crlf
	if lf > max {
		max = lf
	}
	if cr > max {
		max = cr
	}
	tied := 0
	if crlf == max {
		tied++
	}
	if lf == max {
		tied++
	}
	if cr == max {
		tied++
	}
	if tied == 1 {
		if crlf == max {
			return "\r\n"
		}
		if lf == max {
			return "\n"
		}
		return "\r"
	}
	return firstEOL(content)
}

func normalizeEOLMaps(s string) (norm string, from, to []int) {
	b := make([]byte, 0, len(s))
	from = make([]int, 0, len(s))
	to = make([]int, 0, len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\r' {
			end := i
			if i+1 < len(s) && s[i+1] == '\n' {
				end = i + 1
			}
			b = append(b, '\n')
			from = append(from, i)
			to = append(to, end)
			i = end + 1
			continue
		}
		b = append(b, s[i])
		from = append(from, i)
		to = append(to, i)
		i++
	}
	return string(b), from, to
}

func origRange(from, to []int, normStart, normEnd, origLen int) (int, int) {
	if normStart >= len(from) {
		return origLen, origLen
	}
	if normEnd <= normStart {
		return from[normStart], from[normStart]
	}
	if normEnd-1 >= len(to) {
		return from[normStart], origLen
	}
	return from[normStart], to[normEnd-1] + 1
}

func adaptInsertedEOL(s, eol string) string {
	return restoreEOL(normalizeLineEndings(s), eol)
}

func trailingEOL(s string) string {
	switch {
	case strings.HasSuffix(s, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(s, "\n"):
		return "\n"
	case strings.HasSuffix(s, "\r"):
		return "\r"
	default:
		return ""
	}
}

func preserveBoundaryOrig(content string, matchEnd int, matched, replacement string) string {
	nl := trailingEOL(matched)
	if nl == "" || trailingEOL(replacement) != "" {
		return replacement
	}
	if matchEnd >= len(content) {
		return replacement
	}
	return replacement + nl
}

func findEOLTolerantRanges(original, needle string) [][2]int {
	if needle == "" {
		return nil
	}
	norm, from, to := normalizeEOLMaps(original)
	oldN := normalizeLineEndings(needle)
	if oldN == "" {
		return nil
	}
	var ranges [][2]int
	search := 0
	for {
		idx := strings.Index(norm[search:], oldN)
		if idx < 0 {
			break
		}
		idx += search
		start, end := origRange(from, to, idx, idx+len(oldN), len(original))
		ranges = append(ranges, [2]int{start, end})
		search = idx + len(oldN)
		if search > len(norm) {
			break
		}
	}
	return ranges
}

func splicePreserve(original string, ranges [][2]int, replacement string) string {
	if len(ranges) == 0 {
		return original
	}
	var sb strings.Builder
	sb.Grow(len(original) + len(replacement)*len(ranges))
	cursor := 0
	for _, r := range ranges {
		start, end := r[0], r[1]
		if start < cursor || start > len(original) || end > len(original) || end < start {
			continue
		}
		sb.WriteString(original[cursor:start])
		sb.WriteString(preserveBoundaryOrig(original, end, original[start:end], replacement))
		cursor = end
	}
	sb.WriteString(original[cursor:])
	return sb.String()
}

func replaceEOLTolerant(original, oldText, newText string) (string, int) {
	ranges := findEOLTolerantRanges(original, oldText)
	if len(ranges) == 0 {
		return original, 0
	}
	repl := adaptInsertedEOL(newText, dominantEOL(original))
	return splicePreserve(original, ranges, repl), len(ranges)
}

func AppendPreservingEOL(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + adaptInsertedEOL(addition, dominantEOL(existing))
}

func ReplaceLiteralPreservingEOL(content, find, replace string) string {
	if find == "" {
		return content
	}
	out, n := replaceEOLTolerant(content, find, replace)
	if n > 0 {
		return out
	}
	return content
}

func lineNumberAtOrig(original string, byteIdx int) int {
	if byteIdx < 0 {
		byteIdx = 0
	}
	if byteIdx > len(original) {
		byteIdx = len(original)
	}
	return strings.Count(original[:byteIdx], "\n") + 1
}

func editResultFromRanges(original string, ranges [][2]int, newContent, method, confidence string) *EditResult {
	startLine, endLine, linesAffected := 1, 1, 0
	if len(ranges) > 0 {
		startLine = lineNumberAtOrig(original, ranges[0][0])
		endLine = lineNumberAtOrig(original, ranges[len(ranges)-1][1])
		for _, r := range ranges {
			if r[0] < 0 || r[1] > len(original) || r[1] < r[0] {
				continue
			}
			chunk := original[r[0]:r[1]]
			if strings.Contains(chunk, "\n") {
				linesAffected += strings.Count(chunk, "\n")
			} else {
				linesAffected++
			}
		}
	}
	return &EditResult{
		ModifiedContent:  newContent,
		ReplacementCount: len(ranges),
		MatchConfidence:  confidence,
		MatchMethod:      method,
		LinesAffected:    linesAffected,
		StartLine:        startLine,
		EndLine:          endLine,
	}
}
