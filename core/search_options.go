package core

import (
	"path/filepath"
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
	OutputFormat   string
}

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
