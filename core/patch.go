package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	PatchReasonMalformed       = "malformed"
	PatchReasonContextNotFound = "context_not_found"
	PatchReasonOverlap         = "overlap"
)

// PatchError is a fail-closed apply_patch failure. Reason is one of
// malformed, context_not_found, overlap.
type PatchError struct {
	HunkIndex int
	Reason    string
	LineHint  int
	Msg       string
}

func (e *PatchError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

func AsPatchError(err error) *PatchError {
	var pe *PatchError
	if errors.As(err, &pe) && pe != nil {
		return pe
	}
	if err == nil {
		return &PatchError{Reason: PatchReasonMalformed}
	}
	return &PatchError{Reason: PatchReasonMalformed, Msg: err.Error()}
}

type PatchHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string
}

type ParsedPatch struct {
	OldFile string
	NewFile string
	Hunks   []PatchHunk
}

func ParseUnifiedDiff(patch string) (*ParsedPatch, error) {
	files, err := parseUnifiedDiffFiles(patch)
	if err != nil {
		return nil, err
	}
	if len(files) > 1 {
		return nil, &PatchError{Reason: PatchReasonMalformed, Msg: "multi-file patch not supported; split into N apply_patch calls"}
	}
	if len(files) != 1 {
		return nil, &PatchError{Reason: PatchReasonMalformed, Msg: "not a unified diff (need --- / +++ / @@ hunks)"}
	}
	return &files[0], nil
}

func ParseUnifiedDiffs(patch string) ([]ParsedPatch, error) {
	files, err := parseUnifiedDiffFiles(patch)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, &PatchError{Reason: PatchReasonMalformed, Msg: "not a unified diff (need --- / +++ / @@ hunks)"}
	}
	return files, nil
}

func parseUnifiedDiffFiles(patch string) ([]ParsedPatch, error) {
	patch = strings.ReplaceAll(patch, "\r\n", "\n")
	lines := strings.Split(patch, "\n")
	var files []ParsedPatch
	p := ParsedPatch{}
	flush := func() error {
		if p.OldFile == "" && p.NewFile == "" && len(p.Hunks) == 0 {
			return nil
		}
		if p.OldFile == "" || p.NewFile == "" || len(p.Hunks) == 0 {
			return &PatchError{Reason: PatchReasonMalformed, Msg: "not a unified diff (need --- / +++ / @@ hunks)"}
		}
		files = append(files, p)
		p = ParsedPatch{}
		return nil
	}
	i := 0
	for i < len(lines) {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "--- "):
			if p.OldFile != "" && len(p.Hunks) > 0 {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			p.OldFile = strings.TrimSpace(strings.TrimPrefix(line, "--- "))
			i++
		case strings.HasPrefix(line, "+++ "):
			p.NewFile = strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			i++
		case strings.HasPrefix(line, "@@ "):
			h, next, err := parseHunk(lines, i)
			if err != nil {
				return nil, err
			}
			p.Hunks = append(p.Hunks, h)
			i = next
		default:
			i++
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return files, nil
}

func parseHunk(lines []string, i int) (PatchHunk, int, error) {
	header := lines[i]
	var h PatchHunk
	h.OldStart, h.OldCount, h.NewStart, h.NewCount = 1, 1, 1, 1
	rest := strings.TrimPrefix(header, "@@ ")
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return h, i, &PatchError{Reason: PatchReasonMalformed, Msg: fmt.Sprintf("invalid hunk header %q", header)}
	}
	oldSpec := strings.TrimPrefix(parts[0], "-")
	newSpec := strings.TrimPrefix(parts[1], "+")
	h.OldStart, h.OldCount = parseHunkRange(oldSpec)
	h.NewStart, h.NewCount = parseHunkRange(newSpec)
	i++
	for i < len(lines) {
		l := lines[i]
		if strings.HasPrefix(l, "@@ ") || strings.HasPrefix(l, "--- ") {
			break
		}
		if l == "\\ No newline at end of file" {
			i++
			continue
		}
		if l == "" && i == len(lines)-1 {
			break
		}
		if len(l) == 0 {
			h.Lines = append(h.Lines, " ")
			i++
			continue
		}
		switch l[0] {
		case ' ', '+', '-', '\\':
			h.Lines = append(h.Lines, l)
			i++
		default:
			i++
		}
	}
	return h, i, nil
}

func parseHunkRange(spec string) (start, count int) {
	start, count = 1, 1
	if spec == "" {
		return
	}
	a, b, ok := strings.Cut(spec, ",")
	start, _ = strconv.Atoi(a)
	if start == 0 {
		start = 1
	}
	if ok {
		count, _ = strconv.Atoi(b)
	}
	return
}

type origLine struct {
	body string
	term string
}

func splitOrigLines(s string) []origLine {
	var out []origLine
	i := 0
	for i < len(s) {
		j := i
		for j < len(s) && s[j] != '\n' && s[j] != '\r' {
			j++
		}
		body := s[i:j]
		term := ""
		if j < len(s) {
			if s[j] == '\r' && j+1 < len(s) && s[j+1] == '\n' {
				term = "\r\n"
				j += 2
			} else if s[j] == '\r' {
				term = "\r"
				j++
			} else {
				term = "\n"
				j++
			}
		}
		out = append(out, origLine{body, term})
		i = j
	}
	return out
}

func joinOrigLines(lines []origLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.body)
		b.WriteString(l.term)
	}
	return b.String()
}

func ApplyUnifiedPatch(oldContent, patch string) (string, error) {
	parsed, err := ParseUnifiedDiff(patch)
	if err != nil {
		return "", err
	}
	return applyParsedPatch(oldContent, parsed)
}

func applyParsedPatch(oldContent string, parsed *ParsedPatch) (string, error) {
	if parsed == nil {
		return "", &PatchError{Reason: PatchReasonMalformed, Msg: "not a unified diff (need --- / +++ / @@ hunks)"}
	}
	eol := dominantEOL(oldContent)
	oldLines := splitOrigLines(oldContent)
	var out []origLine
	pos := 0
	for hi, hunk := range parsed.Hunks {
		idx := hunk.OldStart - 1
		if idx < 0 {
			idx = 0
		}
		if idx < pos {
			return "", &PatchError{HunkIndex: hi + 1, Reason: PatchReasonOverlap, LineHint: hunk.OldStart,
				Msg: fmt.Sprintf("hunk %d: overlap at line %d", hi+1, hunk.OldStart)}
		}
		if idx > len(oldLines) {
			return "", &PatchError{HunkIndex: hi + 1, Reason: PatchReasonOverlap, LineHint: hunk.OldStart,
				Msg: fmt.Sprintf("hunk %d: start line %d past end of file (%d lines)", hi+1, hunk.OldStart, len(oldLines))}
		}
		out = append(out, oldLines[pos:idx]...)
		pos = idx
		for _, raw := range hunk.Lines {
			if raw == "" {
				raw = " "
			}
			kind, body := raw[0], raw[1:]
			switch kind {
			case ' ':
				if pos >= len(oldLines) || oldLines[pos].body != body {
					return "", &PatchError{HunkIndex: hi + 1, Reason: PatchReasonContextNotFound, LineHint: pos + 1,
						Msg: fmt.Sprintf("hunk %d: context mismatch at line %d", hi+1, pos+1)}
				}
				out = append(out, oldLines[pos])
				pos++
			case '-':
				if pos >= len(oldLines) || oldLines[pos].body != body {
					return "", &PatchError{HunkIndex: hi + 1, Reason: PatchReasonContextNotFound, LineHint: pos + 1,
						Msg: fmt.Sprintf("hunk %d: deletion mismatch at line %d", hi+1, pos+1)}
				}
				pos++
			case '+':
				out = append(out, origLine{body, eol})
			}
		}
	}
	out = append(out, oldLines[pos:]...)
	joined := joinOrigLines(out)
	if len(oldLines) > 0 {
		lastHad := oldLines[len(oldLines)-1].term != ""
		if lastHad && len(out) > 0 && out[len(out)-1].term == "" {
			out[len(out)-1].term = eol
			joined = joinOrigLines(out)
		}
	}
	return joined, nil
}

func splitKeepLast(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		return parts[:len(parts)-1]
	}
	return parts
}

func PatchHeaderPath(header string) string {
	h := strings.TrimSpace(header)
	if i := strings.IndexAny(h, "\t "); i >= 0 {
		h = h[:i]
	}
	h = strings.TrimPrefix(h, "a/")
	h = strings.TrimPrefix(h, "b/")
	return h
}

func PatchHeaderMatches(header, destPath string) bool {
	h := PatchHeaderPath(header)
	if h == "/dev/null" || h == "dev/null" {
		return true
	}
	dest := filepath.ToSlash(destPath)
	h = filepath.ToSlash(h)
	if strings.EqualFold(filepath.Base(dest), filepath.Base(h)) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(dest), strings.ToLower(h))
}
