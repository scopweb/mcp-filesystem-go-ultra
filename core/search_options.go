package core

import (
	"fmt"
	"path"
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
//
//	Match unit: filename-only = one path; content = one matching line;
//	count_only is a total, not a page.
//	max_results is the page size in that unit. Omitted, the page is
//	DefaultSearchPage (40), never the ceiling. An explicit value is honored
//	up to MaxSearchResults (default 1000). offset is 0-based into the
//	stable list (path, then line, then match_start). Offset rather than a
//	cursor: WalkDir order plus an explicit sort is stable for an unchanged
//	tree, and agents already continue with offset-style read_file ranges.
//	If the tree changes between pages, results may skip or duplicate; restart
//	at offset 0. hidden_count is filter exclusion, not pending results.
//	truncated means more matches exist or a byte budget cut the page.
//	match_count is an exact total only when the walk finished; otherwise it
//	is the count known so far and must not be treated as complete.

const (
	maxIncludeExpansions = 64
	maxIncludeBraceDepth = 8
)

// ParseFileTypeFilters splits a comma-separated file_types value into trimmed
// tokens. It does not expand braces. Empty input yields nil.
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

// ExpandIncludeFilter splits include on commas outside braces and expands
// {a,b} alternatives. file_types must keep using ParseFileTypeFilters.
// Unbalanced braces, empty alternatives, nesting over 8, and more than 64
// expansions return an error. * does not cross /. ** matches any depth.
// A pattern containing / is relative to the search path.
func ExpandIncludeFilter(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts, err := splitIncludeCommas(raw)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range parts {
		expanded, err := expandBraceToken(p, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded...)
		if len(out) > maxIncludeExpansions {
			return nil, fmt.Errorf("include expands to more than %d alternatives", maxIncludeExpansions)
		}
	}
	return out, nil
}

func splitIncludeCommas(raw string) ([]string, error) {
	var parts []string
	var b strings.Builder
	depth := 0
	for _, r := range raw {
		switch r {
		case '{':
			depth++
			b.WriteRune(r)
		case '}':
			if depth == 0 {
				return nil, fmt.Errorf("include has an unbalanced '}'")
			}
			depth--
			b.WriteRune(r)
		case ',':
			if depth == 0 {
				if p := strings.TrimSpace(b.String()); p != "" {
					parts = append(parts, p)
				}
				b.Reset()
				continue
			}
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("include has an unbalanced '{'")
	}
	if p := strings.TrimSpace(b.String()); p != "" {
		parts = append(parts, p)
	}
	return parts, nil
}

func expandBraceToken(s string, depth int) ([]string, error) {
	if depth > maxIncludeBraceDepth {
		return nil, fmt.Errorf("include brace nesting exceeds %d", maxIncludeBraceDepth)
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		if strings.Contains(s, "}") {
			return nil, fmt.Errorf("include has an unbalanced '}'")
		}
		return []string{s}, nil
	}
	end, err := matchingBrace(s, start)
	if err != nil {
		return nil, err
	}
	alts, err := splitBraceAlts(s[start+1 : end])
	if err != nil {
		return nil, err
	}
	suffix, err := expandBraceToken(s[end+1:], depth+1)
	if err != nil {
		return nil, err
	}
	prefix := s[:start]
	var out []string
	for _, alt := range alts {
		altExp, err := expandBraceToken(alt, depth+1)
		if err != nil {
			return nil, err
		}
		for _, a := range altExp {
			for _, suf := range suffix {
				out = append(out, prefix+a+suf)
				if len(out) > maxIncludeExpansions {
					return nil, fmt.Errorf("include expands to more than %d alternatives", maxIncludeExpansions)
				}
			}
		}
	}
	return out, nil
}

func matchingBrace(s string, start int) (int, error) {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("include has an unbalanced '{'")
}

func splitBraceAlts(body string) ([]string, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("include has an empty brace alternative")
	}
	var alts []string
	var b strings.Builder
	depth := 0
	for _, r := range body {
		switch r {
		case '{':
			depth++
			b.WriteRune(r)
		case '}':
			if depth == 0 {
				return nil, fmt.Errorf("include has an unbalanced '}'")
			}
			depth--
			b.WriteRune(r)
		case ',':
			if depth == 0 {
				alt := strings.TrimSpace(b.String())
				if alt == "" {
					return nil, fmt.Errorf("include has an empty brace alternative")
				}
				alts = append(alts, alt)
				b.Reset()
				continue
			}
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("include has an unbalanced '{'")
	}
	alt := strings.TrimSpace(b.String())
	if alt == "" {
		return nil, fmt.Errorf("include has an empty brace alternative")
	}
	alts = append(alts, alt)
	return alts, nil
}

// FileTypeFiltersFromArg accepts the MCP file_types argument as []interface{}
// (handler) or []string (internal) and normalizes it. It does not expand braces.
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
// Tokens may be extensions (".go", "go"), globs ("*.go", "**/*.ts"), or
// exact names ("A.cs"). Patterns with / are matched against path itself
// when no search root is known.
func FileMatchesTypeFilter(path string, fileTypes []string) bool {
	return FileMatchesTypeFilterIn(path, "", fileTypes)
}

// FileMatchesTypeFilterIn is FileMatchesTypeFilter with the search root.
// Patterns containing / match the slash path relative to root. * does not
// cross /. ** matches any directory depth.
func FileMatchesTypeFilterIn(path, root string, fileTypes []string) bool {
	if len(fileTypes) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	base := filepath.Base(path)
	rel := filepath.ToSlash(path)
	if root != "" {
		if r, err := filepath.Rel(root, path); err == nil && r != "." && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
	}
	for _, ft := range fileTypes {
		ft = strings.TrimSpace(ft)
		if ft == "" {
			continue
		}
		if isNameOrPathFilter(ft) {
			if globMatchFile(ft, base, rel) {
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

func isNameOrPathFilter(ft string) bool {
	if strings.ContainsAny(ft, "*?[") || strings.ContainsAny(ft, `/\`) {
		return true
	}
	return strings.Contains(ft, ".") && !strings.HasPrefix(ft, ".")
}

func globMatchFile(pattern, base, rel string) bool {
	pat := filepath.ToSlash(strings.TrimSpace(pattern))
	if pat == "" {
		return false
	}
	if !strings.Contains(pat, "/") {
		ok, err := filepath.Match(pat, base)
		return err == nil && ok
	}
	if strings.HasPrefix(pat, "**/") {
		rest := pat[3:]
		if rest != "" && !strings.Contains(rest, "/") {
			if ok, err := filepath.Match(rest, base); err == nil && ok {
				return true
			}
		}
	}
	return matchSlashGlob(pat, rel)
}

func matchSlashGlob(pattern, rel string) bool {
	pattern = strings.Trim(pattern, "/")
	rel = strings.Trim(rel, "/")
	if pattern == "" {
		return rel == ""
	}
	if !strings.Contains(pattern, "**") {
		ok, err := path.Match(pattern, rel)
		return err == nil && ok
	}
	return matchSegs(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegs(pat, parts []string) bool {
	for {
		if len(pat) == 0 {
			return len(parts) == 0
		}
		if pat[0] == "**" {
			if matchSegs(pat[1:], parts) {
				return true
			}
			if len(parts) == 0 {
				return false
			}
			parts = parts[1:]
			continue
		}
		if len(parts) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], parts[0])
		if err != nil || !ok {
			return false
		}
		pat = pat[1:]
		parts = parts[1:]
	}
}

// FileTypeGlobsForRipgrep converts file_types/include tokens into ripgrep --glob
// values. Extensions become "*.ext". Globs and exact names are passed through.
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
		if isNameOrPathFilter(ft) {
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

// SearchPageSize resolves a search page. requested <= 0 means max_results
// was omitted. The result never exceeds ceiling; ceiling <= 0 uses
// MaxSearchResults.
func SearchPageSize(ceiling, requested int) int {
	if ceiling <= 0 {
		ceiling = MaxSearchResults
	}
	if requested <= 0 {
		requested = DefaultSearchPage
	}
	if requested > ceiling {
		return ceiling
	}
	return requested
}

func SearchContinuationHint(offset, pageSize int, reason string) string {
	if pageSize <= 0 {
		pageSize = DefaultSearchPage
	}
	hint := "search_files(..., offset:" + strconv.Itoa(offset) + ", max_results:" + strconv.Itoa(pageSize) + ")"
	if reason != "" {
		return reason + " Continue with " + hint
	}
	return "Continue with " + hint
}
