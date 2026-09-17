package core

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SearchOptions is the typed contract for search_files. Filename search,
// content search (native and ripgrep) and count_only honor the same filters.
type SearchOptions struct {
	Path           string
	Pattern        string
	FileTypes      []string
	CaseSensitive  bool
	WholeWord      bool
	IncludeContent bool
	IncludeContext bool
	ContextLines   int
	CountOnly      bool
	ReturnLines    bool
	NoIgnore       bool
	MaxResults     int
	Offset         int
	OutputFormat   string
}

// Search pagination (FIABILIDAD-OPERATIVA E3):
//   Match unit: filename-only = one path; content = one matching line;
//   count_only is a total, not a page.
//   max_results is the page size in that unit. offset is 0-based into the
//   stable list (path, then line, then match_start). Offset rather than a
//   cursor: WalkDir order plus an explicit sort is stable for an unchanged
//   tree, and agents already continue with offset-style read_file ranges.
//   If the tree changes between pages, results may skip or duplicate; restart
//   at offset 0. hidden_count is filter exclusion, not pending results.
//   truncated means more matches exist or a byte budget cut the page.
//   match_count is an exact total only when the walk finished; otherwise it
//   is the count known so far and must not be treated as complete.

// ParseFileTypeFilters splits a comma-separated file_types or include value
// into trimmed tokens. Empty input yields nil.
func ParseFileTypeFilters(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FileTypeFiltersFromArg accepts the MCP file_types argument as []interface{}
// (handler) or []string (internal) and normalizes it.
func FileTypeFiltersFromArg(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return ParseFileTypeFilters(strings.Join(t, ","))
	case []interface{}:
		var parts []string
		for _, item := range t {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return ParseFileTypeFilters(strings.Join(parts, ","))
	case string:
		return ParseFileTypeFilters(t)
	default:
		return nil
	}
}

// FileMatchesTypeFilter reports whether path should be included given the
// caller's file_types/include tokens. Empty filters match everything.
// Tokens may be extensions (".go", "go") or globs ("*.go", "**/*.ts").
func FileMatchesTypeFilter(path string, fileTypes []string) bool {
	if len(fileTypes) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	base := filepath.Base(path)
	slashPath := filepath.ToSlash(path)
	for _, ft := range fileTypes {
		ft = strings.TrimSpace(ft)
		if ft == "" {
			continue
		}
		if strings.ContainsAny(ft, "*?[") {
			if globMatchFile(ft, base, slashPath) {
				return true
			}
			continue
		}
		want := ft
		if !strings.HasPrefix(want, ".") {
			want = "." + want
		}
		if strings.ToLower(want) == ext {
			return true
		}
	}
	return false
}

func globMatchFile(pattern, base, slashPath string) bool {
	pat := filepath.ToSlash(pattern)
	if ok, _ := filepath.Match(pat, base); ok {
		return true
	}
	if ok, _ := filepath.Match(pat, slashPath); ok {
		return true
	}
	if strings.HasPrefix(pat, "**/") {
		rest := pat[3:]
		if ok, _ := filepath.Match(rest, base); ok {
			return true
		}
		if ok, _ := filepath.Match(rest, slashPath); ok {
			return true
		}
	}
	return false
}

// FileTypeGlobsForRipgrep converts file_types/include tokens into ripgrep --glob
// values. Extensions become "*.ext"; existing globs are passed through.
func FileTypeGlobsForRipgrep(fileTypes []string) []string {
	if len(fileTypes) == 0 {
		return nil
	}
	var globs []string
	for _, ft := range fileTypes {
		ft = strings.TrimSpace(ft)
		if ft == "" {
			continue
		}
		if strings.ContainsAny(ft, "*?[") {
			globs = append(globs, filepath.ToSlash(ft))
			continue
		}
		if !strings.HasPrefix(ft, ".") {
			ft = "." + ft
		}
		globs = append(globs, "*"+strings.ToLower(ft))
	}
	return globs
}

func SortSearchMatches(m []SearchMatch) {
	sort.SliceStable(m, func(i, j int) bool {
		fi := strings.ToLower(filepath.ToSlash(m[i].File))
		fj := strings.ToLower(filepath.ToSlash(m[j].File))
		if fi != fj {
			return fi < fj
		}
		if m[i].LineNumber != m[j].LineNumber {
			return m[i].LineNumber < m[j].LineNumber
		}
		return m[i].MatchStart < m[j].MatchStart
	})
}

func SliceSearchPage(matches []SearchMatch, offset, pageSize int) (page []SearchMatch, truncated bool, next int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(matches) {
		offset = len(matches)
	}
	end := len(matches)
	if pageSize > 0 && offset+pageSize < end {
		end = offset + pageSize
		truncated = true
	}
	page = matches[offset:end]
	next = offset + len(page)
	if !truncated && next < len(matches) {
		truncated = true
	}
	return page, truncated, next
}

func SearchContinuationHint(offset, pageSize int, reason string) string {
	if pageSize <= 0 {
		pageSize = 50
	}
	hint := "search_files(..., offset:" + strconv.Itoa(offset) + ", max_results:" + strconv.Itoa(pageSize) + ")"
	if reason != "" {
		return reason + " Continue with " + hint
	}
	return "Continue with " + hint
}
