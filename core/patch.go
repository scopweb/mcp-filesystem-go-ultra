package core

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

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
	patch = strings.ReplaceAll(patch, "\r\n", "\n")
	lines := strings.Split(patch, "\n")
	p := &ParsedPatch{}
	i := 0
	for i < len(lines) {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "--- "):
			if p.OldFile != "" && len(p.Hunks) > 0 {
				return nil, fmt.Errorf("multi-file patch not supported; split into N apply_patch calls")
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
	if p.OldFile == "" || p.NewFile == "" || len(p.Hunks) == 0 {
		return nil, fmt.Errorf("not a unified diff (need --- / +++ / @@ hunks)")
	}
	return p, nil
}

func parseHunk(lines []string, i int) (PatchHunk, int, error) {
	header := lines[i]
	var h PatchHunk
	h.OldStart, h.OldCount, h.NewStart, h.NewCount = 1, 1, 1, 1
	rest := strings.TrimPrefix(header, "@@ ")
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return h, i, fmt.Errorf("invalid hunk header %q", header)
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

func ApplyUnifiedPatch(oldContent, patch string) (string, error) {
	parsed, err := ParseUnifiedDiff(patch)
	if err != nil {
		return "", err
	}
	eol := "\n"
	if strings.Contains(oldContent, "\r\n") {
		eol = "\r\n"
	}
	oldNorm := strings.ReplaceAll(oldContent, "\r\n", "\n")
	oldLines := splitKeepLast(oldNorm)
	var out []string
	pos := 0
	for hi, hunk := range parsed.Hunks {
		idx := hunk.OldStart - 1
		if idx < 0 {
			idx = 0
		}
		if idx > len(oldLines) {
			return "", fmt.Errorf("hunk %d: start line %d past end of file (%d lines)", hi+1, hunk.OldStart, len(oldLines))
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
				if pos >= len(oldLines) || oldLines[pos] != body {
					return "", fmt.Errorf("hunk %d: context mismatch at line %d", hi+1, pos+1)
				}
				out = append(out, body)
				pos++
			case '-':
				if pos >= len(oldLines) || oldLines[pos] != body {
					return "", fmt.Errorf("hunk %d: deletion mismatch at line %d", hi+1, pos+1)
				}
				pos++
			case '+':
				out = append(out, body)
			}
		}
	}
	out = append(out, oldLines[pos:]...)
	joined := strings.Join(out, "\n")
	if strings.HasSuffix(oldNorm, "\n") && !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	if eol == "\r\n" {
		joined = strings.ReplaceAll(joined, "\n", "\r\n")
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
