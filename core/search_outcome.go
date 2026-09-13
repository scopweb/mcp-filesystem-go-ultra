package core

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var searchCountRe = regexp.MustCompile(`(?i)(\d+)\s+matches?`)

// SearchOutcome is the typed search result. Handlers must publish
// structuredContent from these fields — never by scraping Text.
type SearchOutcome struct {
	Text         string
	Matches      []SearchMatch
	MatchCount   int
	Truncated    bool
	HiddenCount  int
	FilenameOnly bool
}

// SearchFiles runs filename, content, or count search and returns a typed outcome.
func (e *UltraFastEngine) SearchFiles(ctx context.Context, opts SearchOptions) (SearchOutcome, error) {
	if opts.CountOnly {
		return e.searchCountOutcome(ctx, opts)
	}
	if opts.IncludeContent || opts.WholeWord || opts.IncludeContext {
		return e.advancedSearchOutcomeFromOpts(ctx, opts)
	}
	return e.filenameSearchOutcome(ctx, opts)
}

func (e *UltraFastEngine) searchCountOutcome(ctx context.Context, opts SearchOptions) (SearchOutcome, error) {
	text, err := e.CountOccurrencesOpts(ctx, opts)
	if err != nil {
		return SearchOutcome{}, err
	}
	n := 0
	if m := searchCountRe.FindStringSubmatch(text); m != nil {
		fmt.Sscanf(m[1], "%d", &n)
	}
	return SearchOutcome{Text: text, MatchCount: n, Matches: []SearchMatch{}}, nil
}

func (e *UltraFastEngine) filenameSearchOutcome(ctx context.Context, opts SearchOptions) (SearchOutcome, error) {
	if err := e.acquireOperation(ctx, "search"); err != nil {
		return SearchOutcome{}, err
	}
	start := time.Now()
	defer e.releaseOperation("search", start)

	path := NormalizePath(opts.Path)
	pattern := opts.Pattern
	if path == "" || pattern == "" {
		return SearchOutcome{Text: "❌ Error: path and pattern are required", FilenameOnly: true}, nil
	}
	validPath, err := e.validatePath(path)
	if err != nil {
		return SearchOutcome{Text: fmt.Sprintf("❌ Error: Path error: %v", err), FilenameOnly: true}, nil
	}
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreSearch,
		ToolName:   "search_files",
		FilePath:   validPath,
		Operation:  "search",
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata:   map[string]interface{}{"pattern": pattern},
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreSearch, hookCtx); err != nil {
		return SearchOutcome{Text: fmt.Sprintf("❌ Error: pre-search hook denied: %v", err), FilenameOnly: true}, nil
	}
	out, err := e.performSmartSearchOutcome(ctx, validPath, pattern, false, opts.FileTypes, opts.NoIgnore, opts.MaxResults)
	if err != nil {
		return SearchOutcome{Text: fmt.Sprintf("❌ Error: Search error: %v", err), FilenameOnly: true}, nil
	}
	hookCtx.Event = HookPostSearch
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostSearch, hookCtx)
	out.FilenameOnly = true
	return out, nil
}

func (e *UltraFastEngine) advancedSearchOutcomeFromOpts(ctx context.Context, opts SearchOptions) (SearchOutcome, error) {
	if err := e.acquireOperation(ctx, "search"); err != nil {
		return SearchOutcome{}, err
	}
	start := time.Now()
	defer e.releaseOperation("search", start)

	path := NormalizePath(opts.Path)
	pattern := opts.Pattern
	outputFormat := opts.OutputFormat
	if outputFormat == "" {
		outputFormat = "auto"
	}
	contextLines := opts.ContextLines
	maxResults := e.config.MaxSearchResults
	if opts.MaxResults > 0 {
		maxResults = opts.MaxResults
	}

	if path == "" || pattern == "" {
		return SearchOutcome{Text: "❌ Error: path and pattern are required"}, nil
	}
	validPath, err := e.validatePath(path)
	if err != nil {
		return SearchOutcome{Text: fmt.Sprintf("❌ Error: %v", err)}, nil
	}
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreSearch,
		ToolName:   "search_files",
		FilePath:   validPath,
		Operation:  "advanced_search",
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata:   map[string]interface{}{"pattern": pattern, "case_sensitive": opts.CaseSensitive, "whole_word": opts.WholeWord},
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreSearch, hookCtx); err != nil {
		return SearchOutcome{Text: fmt.Sprintf("❌ Error: pre-search hook denied: %v", err)}, nil
	}

	matches, hidden, err := e.performAdvancedTextSearch(ctx, validPath, pattern, opts.CaseSensitive, opts.WholeWord, opts.IncludeContext, contextLines, outputFormat, opts.NoIgnore, opts.FileTypes)
	if err != nil {
		return SearchOutcome{Text: fmt.Sprintf("❌ Error: %v", err)}, nil
	}

	totalBeforeCap := len(matches)
	truncated := false
	if maxResults > 0 && len(matches) > maxResults {
		matches = matches[:maxResults]
		truncated = true
	}

	hookCtx.Event = HookPostSearch
	hookCtx.Metadata["match_count"] = len(matches)
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostSearch, hookCtx)

	if len(matches) == 0 {
		return SearchOutcome{
			Text:        fmt.Sprintf("🔍 No matches found for pattern '%s' in %s", pattern, path),
			Matches:     []SearchMatch{},
			MatchCount:  0,
			HiddenCount: hidden,
		}, nil
	}

	text := e.formatAdvancedSearchOutput(matches, pattern, path, outputFormat, opts.IncludeContext, totalBeforeCap, maxResults, truncated)
	return SearchOutcome{
		Text:        text,
		Matches:     matches,
		MatchCount:  totalBeforeCap,
		Truncated:   truncated,
		HiddenCount: hidden,
	}, nil
}

func hitsFromFilenameResults(results []string) []SearchMatch {
	out := make([]SearchMatch, 0, len(results))
	for _, r := range results {
		out = append(out, SearchMatch{File: strings.TrimPrefix(r, "📄 ")})
	}
	return out
}
