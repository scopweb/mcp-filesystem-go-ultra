package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// utf8SafeEnd returns the greatest index <= n that does not split a UTF-8 rune.
// A newline search can stay on bytes: 0x0A never appears inside a multibyte rune.
func utf8SafeEnd(s string, n int) int {
	if n > len(s) {
		n = len(s)
	}
	if n < 0 {
		return 0
	}
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// trimLineByteOffset drops the first byteOffset bytes of lines[startLine-1].
// byteOffset must be a rune boundary strictly inside that line.
func trimLineByteOffset(lines []string, startLine, byteOffset int) string {
	if byteOffset <= 0 {
		return ""
	}
	if startLine < 1 || startLine > len(lines) {
		return fmt.Sprintf("byte_offset: start_line %d is outside the file (%d lines)", startLine, len(lines))
	}
	line := lines[startLine-1]
	if byteOffset > len(line) || (byteOffset < len(line) && !utf8.RuneStart(line[byteOffset])) {
		return fmt.Sprintf("byte_offset %d is not a rune boundary in line %d (%d bytes)", byteOffset, startLine, len(line))
	}
	if byteOffset == len(line) {
		return fmt.Sprintf("byte_offset %d is the end of line %d; continue with start_line=%d and byte_offset=0", byteOffset, startLine, startLine+1)
	}
	lines[startLine-1] = line[byteOffset:]
	return ""
}

func stripProjectionFooter(text string) string {
	markers := []string{"\n\n[Lines ", "\n[Truncated:"}
	cut := -1
	for _, marker := range markers {
		i := strings.LastIndex(text, marker)
		if i >= 0 && i >= len(text)-500 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	if cut < 0 {
		return text
	}
	return text[:cut]
}

func clampFormattedSpillLimit(p readProjection, path string, limit int) readProjection {
	if limit <= 0 || limit > claudeSpillChars {
		limit = claudeSpillChars
	}
	if len(p.Text) <= limit {
		return p
	}
	body := stripProjectionFooter(p.Text)
	const reserve = 240
	budget := limit - reserve
	if budget < 4 {
		budget = 4
	}
	// The previous footer, not the file, pushed the response over the limit.
	if len(body) <= budget {
		footer := boundedSpillFooter(p, path, false)
		if len(body)+len(footer) <= limit {
			p.Text = body + footer
		} else {
			p.Text = body
		}
		return p
	}
	cutAt := budget
	midLine := false
	if i := strings.LastIndex(body[:budget], "\n"); i > 0 {
		cutAt = i
	} else {
		cutAt = utf8SafeEnd(body, budget)
		midLine = true
	}
	if cutAt == 0 && body != "" {
		_, size := utf8.DecodeRuneInString(body)
		if size > 0 && size <= budget {
			cutAt = size
			midLine = true
		}
	}
	kept := body[:cutAt]
	for attempt := 0; attempt < 8; attempt++ {
		p = assignSpillCursor(p, kept, midLine)
		footer := boundedSpillFooter(p, path, true)
		if len(kept)+len(footer) <= limit {
			p.Text = kept + footer
			p.Truncated = true
			p.Cut = mergeSpillCut(p.Cut)
			return p
		}
		if nl := strings.LastIndex(kept, "\n"); nl > 0 {
			kept = kept[:nl]
			midLine = false
			continue
		}
		next := utf8SafeEnd(kept, len(kept)-1)
		if next <= 0 || next == len(kept) {
			break
		}
		kept = kept[:next]
		midLine = true
	}
	p = assignSpillCursor(p, kept, midLine)
	p.Truncated = true
	p.Cut = mergeSpillCut(p.Cut)
	footer := boundedSpillFooter(p, path, true)
	switch {
	case len(kept)+len(footer) <= limit:
		p.Text = kept + footer
	case len(kept) <= limit:
		p.Text = kept
	default:
		p.Text = kept[:utf8SafeEnd(kept, limit)]
	}
	return p
}

func mergeSpillCut(cut string) string {
	if strings.Contains(cut, cutLineWidth) {
		return cutCharSpill + "+" + cutLineWidth
	}
	return cutCharSpill
}

func assignSpillCursor(p readProjection, kept string, midLine bool) readProjection {
	n := 1
	if kept != "" {
		n = strings.Count(kept, "\n") + 1
	}
	if p.StartLine < 1 {
		p.StartLine = 1
	}
	p.EndLine = p.StartLine + n - 1
	if p.TotalLines > 0 && p.EndLine > p.TotalLines {
		p.EndLine = p.TotalLines
	}
	if p.EndLine < p.StartLine {
		p.EndLine = p.StartLine
	}
	if midLine {
		partial := kept
		if i := strings.LastIndex(kept, "\n"); i >= 0 {
			partial = kept[i+1:]
		}
		p.ContinueAt = p.EndLine
		offset := len(partial)
		if n == 1 {
			offset += p.LineOffset
		}
		if offset < 1 {
			_, size := utf8.DecodeRuneInString(partial)
			if size < 1 {
				size = 1
			}
			offset = size
		}
		p.ResumeOffset = offset
		return p
	}
	p.ResumeOffset = 0
	p.ContinueAt = p.EndLine + 1
	return p
}

func boundedSpillFooter(p readProjection, path string, spilled bool) string {
	base := filepath.Base(path)
	if !spilled {
		if p.EndLine > 0 || p.TotalLines > 0 {
			return fmt.Sprintf("\n\n[Lines %d-%d of %d total lines in %s]", p.StartLine, p.EndLine, p.TotalLines, base)
		}
		return ""
	}
	if p.ResumeOffset > 0 {
		return fmt.Sprintf(
			"\n\n[Lines %d-%d of %d total lines in %s - line %d cut at byte %d to stay within %d bytes. Continue with start_line=%d, byte_offset=%d]",
			p.StartLine, p.EndLine, p.TotalLines, base, p.ContinueAt, p.ResumeOffset, claudeSpillChars, p.ContinueAt, p.ResumeOffset,
		)
	}
	return fmt.Sprintf(
		"\n\n[Lines %d-%d of %d total lines in %s - text cut to stay within %d bytes. Continue with start_line=%d]",
		p.StartLine, p.EndLine, p.TotalLines, base, claudeSpillChars, p.ContinueAt,
	)
}

func batchResumeContinuation() map[string]any {
	return map[string]any{
		"kind": "batch_resume",
		"hint": "aggregate text budget exhausted. Re-read from this path through the rest of the paths list; those bodies are not in this page.",
	}
}
