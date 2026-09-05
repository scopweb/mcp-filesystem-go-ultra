package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

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
	caseSensitive, wholeWord, includeContext bool, contextLines int, noIgnore bool) ([]SearchMatch, error) {

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

	// Exclude noisy directories via glob. The former `--ignore <dir>` form was
	// wrong: --ignore is a boolean flag (counterpart of --no-ignore), so every
	// dir name was parsed as a positional argument — the first one became the
	// search pattern and the user's real pattern/path became search paths,
	// making ripgrep error out (silent fallback to native search).
	for dir := range searchSkipDirs {
		args = append(args, "--glob", "!**/"+dir+"/**")
	}
	// v4.5.31: exclude generated/minified bundles from ripgrep searches too.
	// (*.min.* catches .min.js/.min.css and any future variant; *.map catches
	// source maps which are also single-line JSON blobs.)
	for _, pat := range searchMinifiedPatterns {
		args = append(args, "--glob", "!**/"+pat)
	}

	// `-e` forces the next argument to be parsed as the pattern even when it
	// starts with '-' — this prevents flag injection (e.g. a pattern of
	// "--pre=<cmd>" would otherwise make ripgrep execute an arbitrary
	// preprocessor command per file). `--` marks the end of flags so the
	// search path is always positional.
	args = append(args, "-e", pattern, "--", path)

	// Determine which ripgrep binary to use
	rgBin := "rg" // default: PATH
	if ripgrep.IsEmbedded() {
		if embeddedPath, err := ripgrep.GetExtractedPath(); err == nil {
			rgBin = embeddedPath
		}
	}

	cmd := exec.CommandContext(ctx, rgBin, args...)
	output, err := cmd.Output()
	if err != nil {
		// ripgrep exit code 1 means "no matches found" — a valid result,
		// not a failure. Codes >= 2 are real errors.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []SearchMatch{}, nil
		}
		return nil, fmt.Errorf("ripgrep failed: %w", err)
	}

	var matches []SearchMatch
	// pendingContext holds the trailing context lines rg emitted since the
	// last match record; they become the leading context of the next match.
	// Context records are also appended to the previous match (trailing
	// context), mirroring the native path's per-match window.
	var pendingContext []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
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
		case "begin":
			// New file: context lines from the previous file must not leak
			// into this one's matches.
			pendingContext = pendingContext[:0]
			continue
		case "context":
			ctxLine := truncateSearchLine(strings.TrimSpace(strings.TrimRight(rgMatch.Data.Lines.Text, "\r\n")), 0)
			if includeContext && contextLines > 0 {
				// Trailing context of the previous match (cap at contextLines
				// after the match line: leading + match + trailing ≈ 2N).
				if len(matches) > 0 && len(matches[len(matches)-1].Context) < 2*contextLines {
					matches[len(matches)-1].Context = append(matches[len(matches)-1].Context, ctxLine)
				}
				if len(pendingContext) < contextLines {
					pendingContext = append(pendingContext, ctxLine)
				}
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
		if includeContext && contextLines > 0 && len(pendingContext) > 0 {
			match.Context = append(match.Context, pendingContext...)
			pendingContext = pendingContext[:0]
		} else {
			pendingContext = pendingContext[:0]
		}

		matches = append(matches, match)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ripgrep output parsing error: %w", err)
	}

	return matches, nil
}
