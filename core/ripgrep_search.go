package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/mcp/filesystem-ultra/embed/ripgrep"
)

// DetectRipgrep checks if ripgrep (rg) is available on the system.
// It first checks PATH, then falls back to embedded binary if embed_rg build tag is set.
// Returns availability status and version string.
func DetectRipgrep() (available bool, version string) {
	// First try: check if rg is in PATH.
	// NOTE: an exec.Cmd can only be started once — the previous implementation
	// called Run() and then Output() on the SAME Cmd, so Output always failed
	// with "exec: already started" and ripgrep was never detected from PATH.
	if output, err := exec.Command("rg", "--version").Output(); err == nil {
		parts := strings.Fields(string(output))
		if len(parts) >= 2 {
			return true, parts[1]
		}
	}

	// Second try: extract embedded binary (only with embed_rg tag)
	if ripgrep.IsEmbedded() {
		binPath, err := ripgrep.GetExtractedPath()
		if err == nil {
			// Try to get version from extracted binary
			cmd := exec.Command(binPath, "--version")
			if output, err := cmd.Output(); err == nil {
				parts := strings.Fields(string(output))
				if len(parts) >= 2 {
					return true, parts[1]
				}
			}
		}
	}

	return false, ""
}

// ripgrepMatch represents ripgrep's JSON output format for matches.
type ripgrepMatch struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Bytes      struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"bytes"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
		ContextLine *string `json:"context_line,omitempty"`
	} `json:"data"`
}

// RunRipgrepSearch executes ripgrep with --json output and returns SearchMatch results.
// Falls back to returning an error if ripgrep is not available or fails.
// The caller is responsible for passing a validated path that passes IsPathAllowed.
func (e *UltraFastEngine) RunRipgrepSearch(ctx context.Context, path, pattern string,
	caseSensitive, wholeWord, includeContext bool, contextLines int, noIgnore bool, fileTypes []string, includeGroups ...[]string) ([]SearchMatch, error) {

	args := []string{
		"--json",
		"--max-filesize=10M",
		"--hidden",
		"--encoding=none",
	}
	if noIgnore {
		args = append(args, "--no-ignore")
	} else {
		for _, f := range extraRipgrepIgnoreFiles(path) {
			args = append(args, "--ignore-file", f)
		}
	}

	if !caseSensitive {
		args = append(args, "--ignore-case")
	}
	if wholeWord {
		args = append(args, "-w")
	}
	if includeContext && contextLines > 0 {
		args = append(args, "-C", fmt.Sprintf("%d", contextLines))
	}

	// ripgrep unions positive globs. Use one group to prune traversal, then
	// enforce all groups with the shared matcher before pagination.
	prefilter := fileTypes
	if len(prefilter) == 0 && len(includeGroups) > 0 {
		prefilter = includeGroups[0]
	}
	for _, ft := range prefilter {
		for _, g := range FileTypeGlobsForRipgrep([]string{ft}) {
			if !isNameOrPathFilter(ft) {
				// Do not mix --iglob and --glob: rg gives --iglob priority
				// even over later exclusions. Use one ordered glob set.
				g = caseInsensitiveExtensionGlob(g)
			}
			args = append(args, "--glob", g)
		}
	}
	// Later globs win in ripgrep. Exclusions must follow positive filters so
	// file_types/include cannot re-enable noisy directories or minified files.
	for dir := range searchSkipDirs {
		args = append(args, "--glob", "!**/"+dir+"/**")
	}
	for _, pat := range ripgrepMinifiedExcludeGlobs() {
		args = append(args, "--glob", pat)
	}

	// `-e` forces the next argument to be parsed as the pattern even when it
	// starts with '-' — this prevents flag injection (e.g. a pattern of
	// "--pre=<cmd>" would otherwise make ripgrep execute an arbitrary
	// preprocessor command per file). `--` marks the end of flags so the
	// search path is always positional.
	root := path
	target := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		// Slash globs must be relative to the requested search root, not
		// the server's working directory or the absolute temp path.
		target = "."
	} else {
		root = filepath.Dir(path)
		target = filepath.Base(path)
	}
	args = append(args, "-e", pattern, "--", target)

	// Determine which ripgrep binary to use
	rgBin := "rg" // default: PATH
	if ripgrep.IsEmbedded() {
		if embeddedPath, err := ripgrep.GetExtractedPath(); err == nil {
			rgBin = embeddedPath
		}
	}

	cmd := exec.CommandContext(ctx, rgBin, args...)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		// ripgrep exit code 1 means "no matches found" — a valid result,
		// not a failure. Codes >= 2 are real errors.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []SearchMatch{}, nil
		}
		return nil, fmt.Errorf("ripgrep failed: %w", err)
	}

	matches, err := parseRipgrepOutput(output, includeContext, contextLines)
	if err != nil {
		return nil, err
	}
	filtered := matches[:0]
	for _, match := range matches {
		if !filepath.IsAbs(match.File) {
			match.File = filepath.Join(root, match.File)
		}
		if fileMatchesSearchFilters(match.File, root, fileTypes, includeGroups...) {
			filtered = append(filtered, match)
		}
	}
	return filtered, nil
}

// caseInsensitiveExtensionGlob preserves the native extension-case contract
// without changing the case sensitivity or precedence of include/exclude globs.
func caseInsensitiveExtensionGlob(glob string) string {
	var b strings.Builder
	for _, r := range glob {
		lower, upper := unicode.ToLower(r), unicode.ToUpper(r)
		if lower != upper {
			fmt.Fprintf(&b, "[%c%c]", lower, upper)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Keep absolute line numbers: rg emits disjoint context groups and suppresses
// duplicate context records between nearby matches.
func parseRipgrepOutput(output []byte, includeContext bool, contextLines int) ([]SearchMatch, error) {
	var matches []SearchMatch
	lines := map[int]string{}
	first := 0
	flush := func() {
		if includeContext && contextLines > 0 {
			for i := first; i < len(matches); i++ {
				m := &matches[i]
				m.ContextSplit = true
				for n := max(1, m.LineNumber-contextLines); n <= m.LineNumber+contextLines; n++ {
					if n == m.LineNumber {
						continue
					}
					if text, ok := lines[n]; ok {
						m.Context = append(m.Context, text)
						m.ContextLineNumbers = append(m.ContextLineNumbers, n)
						if n < m.LineNumber {
							m.ContextBefore++
						}
					}
				}
			}
		}
		first = len(matches)
		clear(lines)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var rgMatch ripgrepMatch
		if err := json.Unmarshal([]byte(line), &rgMatch); err != nil {
			// Skip malformed lines
			slog.Debug("ripgrep: malformed JSON line", "error", err)
			continue
		}

		switch rgMatch.Type {
		case "begin", "end":
			flush()
			continue
		case "context":
			if includeContext && contextLines > 0 {
				lines[rgMatch.Data.LineNumber] = truncateSearchLine(strings.TrimRight(rgMatch.Data.Lines.Text, "\r\n"), 0)
			}
			continue
		case "match":
			// handled below
		default:
			continue
		}

		match := SearchMatch{
			File:       rgMatch.Data.Path.Text,
			LineNumber: rgMatch.Data.LineNumber,
			// rg's lines.text keeps the trailing newline; the native
			// bufio.Scanner path strips it. Trim for output parity and
			// truncate so a single minified line cannot dominate the response.
			Line: truncateSearchLine(strings.TrimRight(rgMatch.Data.Lines.Text, "\r\n"), 0),
		}
		// Byte offsets of the first submatch (native path reports the first
		// regex hit per line too, via FindStringIndex).
		if len(rgMatch.Data.Submatches) > 0 {
			match.MatchStart = rgMatch.Data.Submatches[0].Start
			match.MatchEnd = rgMatch.Data.Submatches[0].End
		}
		if includeContext {
			lines[match.LineNumber] = match.Line
		}
		matches = append(matches, match)
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ripgrep output parsing error: %w", err)
	}

	return matches, nil
}
