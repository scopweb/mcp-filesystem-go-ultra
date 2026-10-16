package core

import "strings"

const (
	symbolRangeExact       = "exact"
	symbolRangeUnavailable = "unavailable"
	symbolRangeDeclaration = "declaration"
)

func lineNumberAt(content string, idx int) int {
	if idx < 0 {
		idx = 0
	}
	if idx > len(content) {
		idx = len(content)
	}
	return strings.Count(content[:idx], "\n") + 1
}

func annotateCS(f *AnalyzeFinding, content string, matchStart, matchEnd int, kind string) {
	var start, end int
	var status string
	if kind == "type" {
		start, end, status = scanAfterName(content, matchStart, matchEnd)
	} else {
		start, end, status = scanFromParen(content, matchStart, matchEnd)
	}
	f.StartLine = start
	if end > 0 {
		f.EndLine = end
	}
	f.RangeStatus = status
	f.RangeKind = symbolRangeDeclaration
	f.Parser = "regex-brace"
	if matchEnd > matchStart && matchEnd <= len(content) {
		sig := strings.TrimSpace(content[matchStart:matchEnd])
		if len(sig) > 180 {
			sig = sig[:180]
		}
		f.Signature = sig
	}
	f.Container = nearestContainer(content, matchStart)
}

func annotateJS(f *AnalyzeFinding, content string, matchStart, matchEnd int) {
	start, end, status := scanAfterName(content, matchStart, matchEnd)
	f.StartLine = start
	if end > 0 {
		f.EndLine = end
	}
	f.RangeStatus = status
	f.RangeKind = symbolRangeDeclaration
	f.Parser = "regex-brace"
}

func scanFromParen(content string, matchStart, matchEnd int) (int, int, string) {
	start := lineNumberAt(content, matchStart)
	if matchEnd <= matchStart || matchEnd > len(content) || content[matchEnd-1] != '(' {
		return start, 0, symbolRangeUnavailable
	}
	end, ok := finishMember(content, matchEnd-1)
	if !ok {
		return start, 0, symbolRangeUnavailable
	}
	return start, lineNumberAt(content, end), symbolRangeExact
}

func scanAfterName(content string, matchStart, matchEnd int) (int, int, string) {
	start := lineNumberAt(content, matchStart)
	if matchEnd < matchStart || matchEnd > len(content) {
		return start, 0, symbolRangeUnavailable
	}
	i, kind, ok := skipToMemberStart(content, matchEnd)
	if !ok {
		return start, 0, symbolRangeUnavailable
	}
	var end int
	switch kind {
	case tokParen:
		end, ok = finishMember(content, i)
	case tokBrace:
		end, ok = matchBrace(content, i)
	case tokSemi:
		end, ok = i, true
	case tokArrow:
		end, ok = matchArrowBody(content, i)
	default:
		ok = false
	}
	if !ok {
		return start, 0, symbolRangeUnavailable
	}
	return start, lineNumberAt(content, end), symbolRangeExact
}

const (
	tokParen = iota
	tokBrace
	tokSemi
	tokArrow
)

// skipToMemberStart walks trivia, base lists and where-clauses until a
// parameter list, body, expression body or semicolon. Unexpected tokens abort.
func skipToMemberStart(content string, i int) (int, int, bool) {
	for i < len(content) {
		j, aborted := skipTrivia(content, i)
		if aborted || j >= len(content) {
			return 0, 0, false
		}
		i = j
		switch content[i] {
		case '(':
			return i, tokParen, true
		case '{':
			return i, tokBrace, true
		case ';':
			return i, tokSemi, true
		case '=':
			if i+1 < len(content) && content[i+1] == '>' {
				return i, tokArrow, true
			}
			return 0, 0, false
		default:
			if !allowedBetween(content[i]) {
				return 0, 0, false
			}
			i++
		}
	}
	return 0, 0, false
}

func allowedBetween(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
		b == '_' || b == ':' || b == ',' || b == '.' || b == '<' || b == '>' || b == '?' ||
		b == '[' || b == ']' || b == '&' || b == '*' || b == '@'
}

func finishMember(content string, openParen int) (int, bool) {
	closeParen, ok := matchDelim(content, openParen, '(', ')')
	if !ok {
		return 0, false
	}
	i, kind, ok := skipToMemberStart(content, closeParen+1)
	if !ok {
		return 0, false
	}
	switch kind {
	case tokBrace:
		return matchBrace(content, i)
	case tokSemi:
		return i, true
	case tokArrow:
		return matchArrowBody(content, i)
	default:
		return 0, false
	}
}

func matchArrowBody(content string, arrow int) (int, bool) {
	i := arrow + 2
	for i < len(content) {
		j, aborted := skipTrivia(content, i)
		if aborted {
			return 0, false
		}
		if j >= len(content) {
			return 0, false
		}
		if content[j] == ';' {
			return j, true
		}
		if content[j] == '{' {
			return matchBrace(content, j)
		}
		next, aborted := consumeAtom(content, j)
		if aborted || next == j {
			return 0, false
		}
		i = next
	}
	return 0, false
}

func matchBrace(content string, open int) (int, bool) {
	return matchDelim(content, open, '{', '}')
}

func matchDelim(content string, open int, left, right byte) (int, bool) {
	depth := 1
	i := open + 1
	for i < len(content) {
		j, aborted := skipTrivia(content, i)
		if aborted {
			return 0, false
		}
		if j >= len(content) {
			return 0, false
		}
		if content[j] == left {
			depth++
			i = j + 1
			continue
		}
		if content[j] == right {
			depth--
			if depth == 0 {
				return j, true
			}
			i = j + 1
			continue
		}
		next, aborted := consumeAtom(content, j)
		if aborted || next == j {
			return 0, false
		}
		i = next
	}
	return 0, false
}

func skipTrivia(content string, i int) (int, bool) {
	for i < len(content) {
		switch content[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '/':
			if i+1 < len(content) && content[i+1] == '/' {
				i += 2
				for i < len(content) && content[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(content) && content[i+1] == '*' {
				end := strings.Index(content[i+2:], "*/")
				if end < 0 {
					return 0, true
				}
				i = i + 2 + end + 2
				continue
			}
			return i, false
		case '#':
			if !atLineStart(content, i) {
				return i, false
			}
			line := content[i:]
			if n := strings.IndexByte(line, '\n'); n >= 0 {
				line = line[:n]
			}
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "#if") || strings.HasPrefix(trim, "#else") || strings.HasPrefix(trim, "#elif") || strings.HasPrefix(trim, "#endif") {
				return 0, true
			}
			if n := strings.IndexByte(content[i:], '\n'); n >= 0 {
				i += n + 1
				continue
			}
			return len(content), false
		default:
			return i, false
		}
	}
	return i, false
}

func atLineStart(content string, i int) bool {
	for i > 0 {
		i--
		switch content[i] {
		case ' ', '\t', '\r':
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

func consumeAtom(content string, i int) (int, bool) {
	if i >= len(content) {
		return i, false
	}
	if aborted, next, ok := consumeQuoted(content, i); ok {
		return next, aborted
	}
	return i + 1, false
}

func consumeQuoted(content string, i int) (aborted bool, next int, ok bool) {
	if startsInterpolation(content, i) || content[i] == '`' || strings.HasPrefix(content[i:], `"""`) {
		return true, 0, true
	}
	if i+1 < len(content) && content[i] == '@' && content[i+1] == '"' {
		j := i + 2
		for j < len(content) {
			if content[j] == '"' {
				if j+1 < len(content) && content[j+1] == '"' {
					j += 2
					continue
				}
				return false, j + 1, true
			}
			j++
		}
		return true, 0, true
	}
	if content[i] != '"' && content[i] != '\'' {
		return false, 0, false
	}
	quote := content[i]
	j := i + 1
	for j < len(content) {
		if content[j] == '\\' && j+1 < len(content) {
			j += 2
			continue
		}
		if content[j] == quote {
			return false, j + 1, true
		}
		if content[j] == '\n' && quote == '\'' {
			return true, 0, true
		}
		j++
	}
	return true, 0, true
}

func startsInterpolation(content string, i int) bool {
	if strings.HasPrefix(content[i:], `$"`) || strings.HasPrefix(content[i:], `$@"`) || strings.HasPrefix(content[i:], `@$"`) {
		return true
	}
	return false
}

func nearestContainer(content string, idx int) string {
	if idx < 0 || idx > len(content) {
		return ""
	}
	prefix := content[:idx]
	keys := []string{"class ", "interface ", "struct ", "record ", "enum "}
	best := -1
	name := ""
	for _, key := range keys {
		at := strings.LastIndex(prefix, key)
		if at > best {
			rest := strings.TrimSpace(prefix[at+len(key):])
			n := containerName(rest)
			if n != "" {
				best = at
				name = n
			}
		}
	}
	return name
}

func containerName(s string) string {
	if s == "" {
		return ""
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || (i > 0 && c >= '0' && c <= '9') {
			i++
			continue
		}
		break
	}
	return s[:i]
}

type razorBlock struct {
	text  string
	start int
}

func razorCodeBlocks(content string) []razorBlock {
	var out []razorBlock
	i := 0
	for i < len(content) {
		if strings.HasPrefix(content[i:], "@@") {
			i += 2
			continue
		}
		if strings.HasPrefix(content[i:], "@*") {
			end := strings.Index(content[i+2:], "*@")
			if end < 0 {
				return out
			}
			i += 2 + end + 2
			continue
		}
		key := ""
		switch {
		case strings.HasPrefix(content[i:], "@code") && razorKeywordBoundary(content, i+5):
			key = "@code"
		case strings.HasPrefix(content[i:], "@functions") && razorKeywordBoundary(content, i+10):
			key = "@functions"
		}
		if key == "" {
			i++
			continue
		}
		j := i + len(key)
		for j < len(content) && (content[j] == ' ' || content[j] == '\t' || content[j] == '\r' || content[j] == '\n') {
			j++
		}
		if j >= len(content) || content[j] != '{' {
			i++
			continue
		}
		close, ok := matchBrace(content, j)
		if !ok {
			return out
		}
		innerStart := j + 1
		innerEnd := close
		out = append(out, razorBlock{text: content[innerStart:innerEnd], start: innerStart})
		i = close + 1
	}
	return out
}

func razorKeywordBoundary(content string, end int) bool {
	if end >= len(content) {
		return true
	}
	c := content[end]
	return !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || (c >= '0' && c <= '9'))
}
