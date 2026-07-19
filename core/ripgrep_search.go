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
	// First try: check if rg is in PATH
	if cmd := exec.Command("rg", "--version"); runVersionCheck(cmd) {
		output, err := cmd.Output()
		if err == nil {
			parts := strings.Fields(string(output))
			if len(parts) >= 2 {
				return true, parts[1]
			}
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

// runVersionCheck attempts to run rg --version without capturing output.
// Returns true if successful (rg is available).
func runVersionCheck(cmd *exec.Cmd) bool {
	cmd.Run()
	return cmd.ProcessState != nil && cmd.ProcessState.Success()
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
		ContextLine *string `json:"context_line,omitempty"`
	} `json:"data"`
}

// RunRipgrepSearch executes ripgrep with --json output and returns SearchMatch results.
// Falls back to returning an error if ripgrep is not available or fails.
// The caller is responsible for passing a validated path that passes IsPathAllowed.
func (e *UltraFastEngine) RunRipgrepSearch(ctx context.Context, path, pattern string,
	caseSensitive, wholeWord, includeContext bool, contextLines int) ([]SearchMatch, error) {

	args := []string{
		"--json",
		"--max-filesize=10M",
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

		// Only process match type
		if rgMatch.Type != "match" {
			continue
		}

		match := SearchMatch{
			File:       rgMatch.Data.Path.Text,
			LineNumber: rgMatch.Data.LineNumber,
			Line:       rgMatch.Data.Lines.Text,
			MatchStart: rgMatch.Data.Bytes.Start,
			MatchEnd:   rgMatch.Data.Bytes.End,
		}

		// Add context lines if present (ripgrep provides them via separate "context" type lines)
		// For now, we capture the main match line; context is available via -C flag
		if includeContext && contextLines > 0 {
			// Ripgrep outputs context as separate "context" type lines before/after match lines
			// We collect these as context on the following match
		}

		matches = append(matches, match)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ripgrep output parsing error: %w", err)
	}

	return matches, nil
}
