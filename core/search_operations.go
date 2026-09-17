package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mcp/filesystem-ultra/mcp"
)

// errWalkStop is a sentinel returned from WalkDir callbacks to stop the walk
// early once max_results is reached (perf #2, v4.5.27). Never surfaces to callers.
var errWalkStop = errors.New("walk stopped: max_results reached")

// SmartSearch performs intelligent search with regex and filters
func (e *UltraFastEngine) SmartSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResponse, error) {
	if err := e.acquireOperation(ctx, "search"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("search", start)

	path := NormalizePath(request.Arguments["path"].(string))
	pattern := request.Arguments["pattern"].(string)
	includeContent, _ := request.Arguments["include_content"].(bool)
	noIgnore, _ := request.Arguments["no_ignore"].(bool)

	// Convert file types if provided
	fileTypes := []string{}
	if fileTypesParam, ok := request.Arguments["file_types"].([]interface{}); ok {
		for _, ft := range fileTypesParam {
			if str, ok := ft.(string); ok {
				fileTypes = append(fileTypes, str)
			}
		}
	}

	if path == "" || pattern == "" {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: "❌ Error: path and pattern are required"},
			},
		}, nil
	}

	validPath, err := e.validatePath(path)
	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: Path error: %v", err)},
			},
		}, nil
	}

	// Execute pre-search hook
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
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: pre-search hook denied: %v", err)},
			},
		}, nil
	}

	maxResults := e.config.MaxSearchResults
	if mr, ok := request.Arguments["max_results"].(float64); ok && mr > 0 {
		maxResults = int(mr)
	}
	out, err := e.performSmartSearchOutcome(ctx, validPath, pattern, includeContent, fileTypes, noIgnore, maxResults, 0)
	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: Search error: %v", err)},
			},
		}, nil
	}

	// Execute post-search hook (best-effort)
	hookCtx.Event = HookPostSearch
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostSearch, hookCtx)

	return &mcp.CallToolResponse{
		Content: []mcp.TextContent{
			{Text: out.Text},
		},
	}, nil
}

// AdvancedTextSearch performs advanced text search with context
func (e *UltraFastEngine) AdvancedTextSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResponse, error) {
	if err := e.acquireOperation(ctx, "search"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("search", start)

	path := NormalizePath(request.Arguments["path"].(string))
	pattern := request.Arguments["pattern"].(string)
	caseSensitive, _ := request.Arguments["case_sensitive"].(bool)
	wholeWord, _ := request.Arguments["whole_word"].(bool)
	includeContext, _ := request.Arguments["include_context"].(bool)
	outputFormat, _ := request.Arguments["output_format"].(string)
	// Default: "auto" — ripgrep-style 'path:line:content' for ≤5 matches,
	// verbose with emojis for more. Empty string from the handler means the
	// caller did not specify output_format (the common case).
	// Explicit "text" is preserved as backward-compatible verbose/compact.
	if outputFormat == "" {
		outputFormat = "auto"
	}

	contextLines := 3
	if cl, ok := request.Arguments["context_lines"].(float64); ok {
		contextLines = int(cl)
	}

	maxResults := e.config.MaxSearchResults
	if mr, ok := request.Arguments["max_results"].(float64); ok && mr > 0 {
		maxResults = int(mr)
	}
	noIgnore, _ := request.Arguments["no_ignore"].(bool)
	fileTypes := FileTypeFiltersFromArg(request.Arguments["file_types"])

	if path == "" || pattern == "" {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: "❌ Error: path and pattern are required"},
			},
		}, nil
	}

	validPath, err := e.validatePath(path)
	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: %v", err)},
			},
		}, nil
	}

	// Execute pre-search hook
	workingDir2, _ := os.Getwd()
	hookCtx2 := &HookContext{
		Event:      HookPreSearch,
		ToolName:   "search_files",
		FilePath:   validPath,
		Operation:  "advanced_search",
		Timestamp:  time.Now(),
		WorkingDir: workingDir2,
		Metadata:   map[string]interface{}{"pattern": pattern, "case_sensitive": caseSensitive, "whole_word": wholeWord},
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreSearch, hookCtx2); err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: pre-search hook denied: %v", err)},
			},
		}, nil
	}

	matches, _, err := e.performAdvancedTextSearch(ctx, validPath, pattern, caseSensitive, wholeWord, includeContext, contextLines, outputFormat, noIgnore, fileTypes)
	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: %v", err)},
			},
		}, nil
	}

	// Honor max_results on the content-search path (previously ignored —
	// the native worker AND the ripgrep path both returned unbounded
	// matches, and output_format:"json" dumped all of them).
	totalBeforeCap := len(matches)
	truncated := false
	if maxResults > 0 && len(matches) > maxResults {
		matches = matches[:maxResults]
		truncated = true
	}

	if len(matches) == 0 {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("🔍 No matches found for pattern '%s' in %s", pattern, path)},
			},
		}, nil
	}

	hookCtx2.Event = HookPostSearch
	hookCtx2.Metadata["match_count"] = len(matches)
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostSearch, hookCtx2)

	text := e.formatAdvancedSearchOutput(matches, pattern, path, outputFormat, includeContext, 0, totalBeforeCap, maxResults, truncated, 0, len(matches))
	return &mcp.CallToolResponse{
		Content: []mcp.TextContent{
			{Text: text},
		},
	}, nil
}

// formatSearchMatchesJSON formats search matches as structured JSON for AI parsing
func formatSearchMatchesJSON(matches []SearchMatch, pattern, path string, totalBeforeCap int, maxResults int) string {
	var buf strings.Builder
	buf.WriteString("{\n")
	buf.WriteString(fmt.Sprintf(`  "pattern": %s, `, jsonString(pattern)))
	buf.WriteString(fmt.Sprintf(`  "path": %s, `, jsonString(path)))
	buf.WriteString(fmt.Sprintf(`  "total_matches": %d, `, totalBeforeCap))
	buf.WriteString(fmt.Sprintf(`  "returned_matches": %d, `, len(matches)))
	if maxResults > 0 && totalBeforeCap > len(matches) {
		buf.WriteString(`  "truncated": true, `)
	}
	buf.WriteString(`  "matches": [`)

	for i, m := range matches {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(fmt.Sprintf(`{"file": %s, "line": %d, "line_number": %d, "match_start": %d, "match_end": %d, "line_content": %s`,
			jsonString(m.File), m.LineNumber, m.LineNumber, m.MatchStart, m.MatchEnd, jsonString(m.Line)))
		if len(m.Context) > 0 {
			buf.WriteString(`, "context": [`)
			for j, ctx := range m.Context {
				if j > 0 {
					buf.WriteString(", ")
				}
				buf.WriteString(jsonString(ctx))
			}
			buf.WriteString("]")
		}
		buf.WriteString("}")
	}

	buf.WriteString("],\n")
	buf.WriteString(`  "summary": `)
	buf.WriteString(jsonString(fmt.Sprintf("Found %d matches for pattern '%s' in %s", len(matches), pattern, path)))
	buf.WriteString("\n}")

	return buf.String()
}

func (e *UltraFastEngine) formatAdvancedSearchOutput(matches []SearchMatch, pattern, path, outputFormat string, includeContext bool, contextLines, totalBeforeCap, maxResults int, truncated bool, offset, nextOffset int) string {
	if outputFormat == "json" {
		body := formatSearchMatchesJSON(matches, pattern, path, totalBeforeCap, maxResults)
		if truncated {
			return body
		}
		return body
	}
	var result strings.Builder
	n := len(matches)
	const ripgrepThreshold = 5
	useRipgrep := outputFormat == "auto" && !includeContext && n <= ripgrepThreshold
	if useRipgrep {
		result.WriteString(formatSearchMatchesRipgrep(matches, n))
	} else if e.config.CompactMode && outputFormat != "text" {
		if totalBeforeCap > n {
			result.WriteString(fmt.Sprintf("%d matches (showing %d-%d)\n", totalBeforeCap, offset+1, offset+n))
		} else {
			result.WriteString(fmt.Sprintf("%d matches\n", n))
		}
		for i := 0; i < n; i++ {
			match := matches[i]
			result.WriteString(fmt.Sprintf("%s:%d[%d:%d] %s\n", match.File, match.LineNumber, match.MatchStart, match.MatchEnd, match.Line))
			if includeContext {
				result.WriteString(formatNumberedContext(match, contextLines, true))
			}
		}
	} else {
		if totalBeforeCap > n {
			result.WriteString(fmt.Sprintf("🔍 Found %d matches for pattern '%s' (showing %d-%d):\n\n", totalBeforeCap, pattern, offset+1, offset+n))
		} else {
			result.WriteString(fmt.Sprintf("🔍 Found %d matches for pattern '%s':\n\n", n, pattern))
		}
		for i := 0; i < n; i++ {
			match := matches[i]
			result.WriteString(fmt.Sprintf("📁 %s:%d [%d:%d]\n", match.File, match.LineNumber, match.MatchStart, match.MatchEnd))
			result.WriteString(fmt.Sprintf("   %s\n", match.Line))
			if includeContext && (len(match.Context) > 0 || contextLines > 0) {
				result.WriteString(formatNumberedContext(match, contextLines, false))
			}
			result.WriteString("\n")
		}
	}
	if truncated {
		reason := "page limit"
		result.WriteString(SearchContinuationHint(nextOffset, maxResults, reason+". "))
		result.WriteByte('\n')
	}
	return result.String()
}

func formatNumberedContext(match SearchMatch, contextLines int, compact bool) string {
	if len(match.Context) == 0 {
		return ""
	}
	nBefore := contextLines
	if nBefore <= 0 {
		nBefore = len(match.Context) / 2
	}
	if match.LineNumber-1 < nBefore {
		nBefore = match.LineNumber - 1
	}
	if nBefore > len(match.Context) {
		nBefore = len(match.Context)
	}
	before := match.Context[:nBefore]
	after := match.Context[nBefore:]
	var b strings.Builder
	indent := "  "
	if !compact {
		indent = "   "
		b.WriteString(indent + "Context:\n")
	}
	for i, line := range before {
		ln := match.LineNumber - len(before) + i
		fmt.Fprintf(&b, "%s%d | %s\n", indent, ln, line)
	}
	fmt.Fprintf(&b, "%s>%d | %s\n", indent, match.LineNumber, strings.TrimRight(match.Line, "\r\n"))
	for i, line := range after {
		fmt.Fprintf(&b, "%s%d | %s\n", indent, match.LineNumber+1+i, line)
	}
	return b.String()
}

// jsonString escapes a string for JSON
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// formatSearchMatchesRipgrep emits matches in ripgrep's default content format:
//
//	path:line:content
//
// One row per match (ripgrep collapses same-line matches; we keep one-per-match
// to preserve order and make line numbers unambiguous for the model).
// Trailing CR/LF stripped from each line to keep output single-line per match
// (Windows-encoded files would otherwise emit \r that breaks downstream parsers).
func formatSearchMatchesRipgrep(matches []SearchMatch, maxToShow int) string {
	if maxToShow <= 0 || maxToShow > len(matches) {
		maxToShow = len(matches)
	}
	var b strings.Builder
	b.Grow(maxToShow * 80) // rough preallocation — most lines fit in 80 bytes
	for i := 0; i < maxToShow; i++ {
		m := matches[i]
		line := strings.TrimRight(m.Line, "\r\n")
		fmt.Fprintf(&b, "%s:%d:%s\n", m.File, m.LineNumber, line)
	}
	if len(matches) > maxToShow {
		fmt.Fprintf(&b, "... (%d more matches)\n", len(matches)-maxToShow)
	}
	return b.String()
}

// performSmartSearch
func (e *UltraFastEngine) performSmartSearchOutcome(ctx context.Context, path, pattern string, includeContent bool, fileTypes []string, noIgnore bool, maxResults int, offset int) (SearchOutcome, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return SearchOutcome{}, &ContextError{Op: "search", Details: "operation cancelled before start"}
	}

	var resultsMu sync.Mutex
	var results []string
	var contentMatches []SearchMatch
	if maxResults <= 0 {
		maxResults = e.config.MaxSearchResults
	}
	if offset < 0 {
		offset = 0
	}
	collectUntil := offset + maxResults + 1

	// Compile regex pattern (uses engine cache to avoid repeated compilation)
	// For glob patterns (*, ?), use filepath.Match instead to avoid regex misinterpretation
	isGlob := isGlobPattern(pattern)
	var regexPattern *regexp.Regexp
	var regexErr error
	if isGlob {
		// Glob pattern: match via filepath.Match (e.g., "Reports.*" → "Reports" + anything)
		// No regex compilation needed; we handle it inline in the walk callback
		regexPattern = nil
	} else {
		regexPattern, regexErr = e.CompileRegex(pattern)
		if regexErr != nil {
			// If not valid regex, use literal search
			regexPattern, _ = e.CompileRegex(regexp.QuoteMeta(pattern))
		}
	}

	// First pass: collect all files to search.
	// Perf (#1, v4.5.27): WalkDir instead of Walk — Walk lstats every entry,
	// WalkDir reuses the DirEntry from ReadDir (one syscall per dir, not per
	// file). On the 50k-entry trees behind the 5-45s searches in the proxy
	// log this is the dominant cost.
	var filesToSearch []string
	ign := NewIgnoreMatcher()
	hiddenCount := 0
	walkErr := filepath.WalkDir(path, func(currentPath string, d os.DirEntry, err error) error {
		// Check context in walk callback
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr // Stop walk if context is cancelled
		}

		if err != nil {
			return nil // Continue with other files
		}

		if skipWalkDir(d.Name(), currentPath, path, d.IsDir(), ign, noIgnore) {
			hiddenCount++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		// Perf (#2, v4.5.27): early exit — once max_results filename matches
		// are collected and no content search is pending, the rest of the
		// tree cannot change the response. Walk used to continue to the end.
		if !includeContent {
			resultsMu.Lock()
			full := len(results) >= collectUntil
			resultsMu.Unlock()
			if full {
				return errWalkStop
			}
		}

		if !FileMatchesTypeFilter(currentPath, fileTypes) {
			return nil
		}

		// Check filename match
		var matched bool
		if isGlob {
			// Glob pattern: use filepath.Match (e.g., "Reports.*" matches "Reports.dll")
			matched, _ = filepath.Match(pattern, d.Name())
		} else {
			matched = regexPattern.MatchString(d.Name())
		}
		if matched {
			resultsMu.Lock()
			if len(results) < collectUntil {
				results = append(results, fmt.Sprintf("📄 %s", currentPath))
			}
			resultsMu.Unlock()
		}

		// Add to content search list if applicable (stat only content candidates).
		// Perf (v4.5.30): isTextCandidate does zero I/O (extension map only);
		// the definitive binary check happens in the worker on the bytes it
		// already read (isTextContent).
		// v4.5.31: skip generated/minified bundles (*.min.*, *.map) by default —
		// their single-line content is not human-readable and blows up responses.
		if includeContent && isTextCandidate(currentPath) && !isMinifiedFile(currentPath) {
			if info, ierr := d.Info(); ierr == nil && info.Size() < 10*1024*1024 { // 10MB limit
				filesToSearch = append(filesToSearch, currentPath)
			}
		}

		return nil
	})
	walkCapped := walkErr == errWalkStop
	if walkErr != nil && walkErr != errWalkStop && !errors.Is(walkErr, context.Canceled) && !errors.Is(walkErr, context.DeadlineExceeded) {
		return SearchOutcome{}, walkErr
	}

	// Second pass: parallel content search using worker pool
	if includeContent && len(filesToSearch) > 0 {
		var wg sync.WaitGroup

		for _, filePath := range filesToSearch {
			// Check context in main loop
			if ctxErr := ctx.Err(); ctxErr != nil {
				break // Exit if context is cancelled
			}

			// Check if we've reached max results
			resultsMu.Lock()
			if len(contentMatches) >= maxResults {
				resultsMu.Unlock()
				break
			}
			resultsMu.Unlock()

			wg.Add(1)
			currentFile := filePath

			e.workerPool.Submit(func() {
				defer wg.Done()

				// Check context in worker
				if ctxErr := ctx.Err(); ctxErr != nil {
					return // Exit worker if context is cancelled
				}

				// Read file
				content, err := os.ReadFile(currentFile)
				if err != nil {
					return
				}

				// Definitive binary check on the bytes we already read
				// (BOM + null bytes). Replaces the old per-file open in the walk.
				if !isTextContent(content) {
					return
				}

				// Use bufio.Scanner for memory-efficient line processing
				// Avoids allocating all lines at once (30-40% memory savings)
				scanner := bufio.NewScanner(bytes.NewReader(content))
				// v4.5.31: allow very long lines (minified bundles can be a single
				// multi-KB line) so we can still process them if they somehow get
				// past the minified-file filter. Default 64KB token limit would fail.
				scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
				var localMatches []SearchMatch
				lineNum := 0

				for scanner.Scan() {
					lineNum++
					line := scanner.Text()
					if !isGlob && regexPattern.MatchString(line) {
						// Only do regex content search for non-glob patterns.
						// Glob patterns (e.g., "Reports.*") are for filename matching only.
						// Content search with glob patterns would misinterpret * as regex.
						// Calculate character offset of pattern match
						matchStart, matchEnd := calculateCharacterOffset(line, regexPattern)
						match := SearchMatch{
							File:       currentFile,
							LineNumber: lineNum,
							Line:       truncateSearchLine(line, 0), // v4.5.31: truncate minified lines
							MatchStart: matchStart,
							MatchEnd:   matchEnd,
						}
						localMatches = append(localMatches, match)
					}
				}

				// Append local matches to global list
				if len(localMatches) > 0 {
					resultsMu.Lock()
					for _, match := range localMatches {
						if len(contentMatches) < collectUntil {
							contentMatches = append(contentMatches, match)
						} else {
							break
						}
					}
					resultsMu.Unlock()
				}
			})
		}

		wg.Wait()
	}

	hits := hitsFromFilenameResults(results)
	hits = append(hits, contentMatches...)
	SortSearchMatches(hits)
	page, pageTrunc, next := SliceSearchPage(hits, offset, maxResults)
	truncated := walkCapped || pageTrunc
	exact := !walkCapped
	matchCount := len(hits)
	if !exact {
		matchCount = len(page)
	}

	if len(page) == 0 && offset == 0 {
		text := fmt.Sprintf("🔍 No matches found for pattern '%s' in %s", pattern, path)
		if !includeContent {
			text = fmt.Sprintf("🔍 No filename matches for pattern '%s' in %s (filename-only search — file contents were NOT searched; pass include_content:true to search inside files)", pattern, path)
		}
		return SearchOutcome{Text: text, Matches: []SearchMatch{}, FilenameOnly: !includeContent, Truncated: walkCapped, HiddenCount: hiddenCount, ExactTotal: exact}, nil
	}

	var resultBuilder strings.Builder
	if e.config.CompactMode {
		if exact && matchCount > len(page) {
			resultBuilder.WriteString(fmt.Sprintf("%d matches (showing %d-%d): ", matchCount, offset+1, offset+len(page)))
		} else {
			resultBuilder.WriteString(fmt.Sprintf("%d matches: ", len(page)))
		}
		for i, h := range page {
			if i > 0 {
				resultBuilder.WriteString(", ")
			}
			if h.LineNumber > 0 {
				resultBuilder.WriteString(fmt.Sprintf("%s:%d[%d:%d] %s", h.File, h.LineNumber, h.MatchStart, h.MatchEnd, h.Line))
			} else {
				resultBuilder.WriteString(h.File)
			}
		}
		resultBuilder.WriteString("\n")
	} else {
		label := "File name matches"
		if includeContent {
			label = "Content matches"
		}
		resultBuilder.WriteString(fmt.Sprintf("🔍 %s (%d):\n", label, len(page)))
		for _, h := range page {
			if h.LineNumber > 0 {
				resultBuilder.WriteString(fmt.Sprintf("  📁 %s:%d [%d:%d] - %s\n", h.File, h.LineNumber, h.MatchStart, h.MatchEnd, h.Line))
			} else {
				resultBuilder.WriteString(fmt.Sprintf("  📄 %s\n", h.File))
			}
		}
	}
	if truncated {
		reason := "page limit"
		if walkCapped && !exact {
			reason = "walk stopped at page limit"
		}
		resultBuilder.WriteString(SearchContinuationHint(next, maxResults, reason+". "))
		resultBuilder.WriteByte('\n')
	}

	return SearchOutcome{
		Text:         resultBuilder.String(),
		Matches:      page,
		MatchCount:   matchCount,
		Truncated:    truncated,
		HiddenCount:  hiddenCount,
		FilenameOnly: !includeContent,
		Offset:       offset,
		NextOffset:   next,
		TruncReason:  "",
		ExactTotal:   exact,
	}, nil
}

// performAdvancedTextSearch implements advanced text search with parallelization
func (e *UltraFastEngine) performAdvancedTextSearch(ctx context.Context, path, pattern string, caseSensitive, wholeWord, includeContext bool, contextLines int, outputFormat string, noIgnore bool, fileTypes []string) ([]SearchMatch, int, error) {
	var matchesMu sync.Mutex
	var matches []SearchMatch

	if includeContext && contextLines <= 0 {
		includeContext = false
	}

	// Try ripgrep first when available (5-50× faster than the native walk on
	// large trees). Offsets (submatches), context lines (-C), and skip-dir
	// exclusions are parsed for full parity with the native path; on any
	// ripgrep failure we fall through to the native implementation.
	if e.ripgrepAvailable {
		rgMatches, rgErr := e.RunRipgrepSearch(ctx, path, pattern, caseSensitive, wholeWord, includeContext, contextLines, noIgnore, fileTypes)
		if rgErr == nil {
			return rgMatches, 0, nil
		}
		// Fall through to Go-native on error
		slog.Debug("Ripgrep fallback", "reason", rgErr)
	}

	// Prepare the pattern
	searchPattern := pattern
	if !caseSensitive {
		searchPattern = "(?i)" + searchPattern
	}
	if wholeWord {
		searchPattern = `\b` + searchPattern + `\b`
	}

	regexPattern, err := e.CompileRegex(searchPattern)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// First pass: collect all files to search (WalkDir: no per-entry lstat, v4.5.27)
	var filesToSearch []string
	ign := NewIgnoreMatcher()
	hiddenCount := 0
	err = filepath.WalkDir(path, func(currentPath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if skipWalkDir(d.Name(), currentPath, path, d.IsDir(), ign, noIgnore) {
			hiddenCount++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !FileMatchesTypeFilter(currentPath, fileTypes) {
			return nil
		}

		// Only search in text-file candidates (zero-I/O extension filter;
		// content validated in the worker) with increased size limit.
		// v4.5.31: skip generated/minified bundles (*.min.*, *.map) by default.
		if !isTextCandidate(currentPath) || isMinifiedFile(currentPath) {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil && info.Size() > 10*1024*1024 { // 10MB limit
			return nil
		}

		filesToSearch = append(filesToSearch, currentPath)
		return nil
	})

	if err != nil {
		return nil, hiddenCount, err
	}

	// Second pass: parallel search using worker pool
	var wg sync.WaitGroup

	for _, filePath := range filesToSearch {
		wg.Add(1)
		currentFile := filePath

		e.workerPool.Submit(func() {
			defer wg.Done()

			content, err := os.ReadFile(currentFile)
			if err != nil {
				return
			}

			// Definitive binary check on the bytes we already read
			// (BOM + null bytes). Replaces the old per-file open in the walk.
			if !isTextContent(content) {
				return
			}

			var localMatches []SearchMatch

			// When context is not needed, use bufio.Scanner for memory efficiency
			// When context is needed, use strings.Split (need forward-looking capability)
			if !includeContext {
				// Memory-efficient path: 30-40% memory savings with bufio.Scanner
				scanner := bufio.NewScanner(bytes.NewReader(content))
				// v4.5.31: allow very long lines so we don't silently stop scanning
				// if a file with minified-like lines slips through the filter.
				scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
				lineNum := 0

				for scanner.Scan() {
					lineNum++
					line := scanner.Text()

					if regexPattern.MatchString(line) {
						// Calculate character offset of pattern match
						matchStart, matchEnd := calculateCharacterOffset(line, regexPattern)
						match := SearchMatch{
							File:       currentFile,
							LineNumber: lineNum,
							Line:       truncateSearchLine(line, 0), // v4.5.31: keep output small
							MatchStart: matchStart,
							MatchEnd:   matchEnd,
						}
						localMatches = append(localMatches, match)
					}
				}
			} else {
				// Context path: needs full file in memory for forward-looking
				lines := strings.Split(string(content), "\n")

				for lineNum, line := range lines {
					if regexPattern.MatchString(line) {
						// Calculate character offset of pattern match
						matchStart, matchEnd := calculateCharacterOffset(line, regexPattern)
						match := SearchMatch{
							File:       currentFile,
							LineNumber: lineNum + 1,
							Line:       truncateSearchLine(line, 0), // v4.5.31: keep output small
							MatchStart: matchStart,
							MatchEnd:   matchEnd,
						}

						// Add context (truncated to avoid huge context lines)
						var context []string
						// Use built-in min/max (Go 1.21+) - no need for helper functions
						start := max(0, lineNum-contextLines)
						end := min(len(lines), lineNum+contextLines+1)

						for i := start; i < end; i++ {
							if i != lineNum {
								context = append(context, truncateSearchLine(strings.TrimSpace(lines[i]), 0))
							}
						}
						match.Context = context

						localMatches = append(localMatches, match)
					}
				}
			}

			// Append local matches to global list
			if len(localMatches) > 0 {
				matchesMu.Lock()
				matches = append(matches, localMatches...)
				matchesMu.Unlock()
			}
		})
	}

	wg.Wait()

	return matches, hiddenCount, nil
}

// searchSkipDirs are directories that should be skipped during search walks.
// These are typically build artifacts, dependency caches, or VCS internals
// that contain large numbers of files irrelevant to source-code searches.
var searchSkipDirs = map[string]bool{
	// Version control
	".git": true, ".svn": true, ".hg": true,
	// JS/Node
	"node_modules": true, ".next": true, ".nuxt": true, "dist": true,
	// .NET / Visual Studio
	"bin": true, "obj": true, ".vs": true, "packages": true, ".nuget": true,
	// Java / Maven / Gradle
	"target": true, ".gradle": true,
	// Python
	"__pycache__": true, ".venv": true, "venv": true, ".eggs": true,
	// General build/cache dirs
	"build": true, ".cache": true, ".tmp": true,
}

// isGlobPattern returns true if the pattern contains glob wildcards (*, ?, [)
// These should be matched via filepath.Match, not regex, to avoid misinterpretation.
// For example, "Reports.*" as regex means "Report" + zero+ 's' + literal dot, not "anything after Report".
func isGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// searchMinifiedPatterns lists filename fragments that identify generated/minified
// bundles. Files matching these are skipped by content search by default because
// their single-line output is not human-readable and wastes tokens.
var searchMinifiedPatterns = []string{
	".min.", // catches *.min.js, *.min.css, *.min.json, etc.
	".map",  // source maps
}

// isMinifiedFile reports whether a file is a generated/minified bundle that should
// be skipped by content search by default.
func isMinifiedFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	for _, p := range searchMinifiedPatterns {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

// truncateSearchLine truncates a matched line to maxLen runes, appending an ellipsis
// when truncation occurs. This prevents a single minified line from blowing up the
// response size.
func truncateSearchLine(line string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = MaxSearchLineLength
	}
	runes := []rune(line)
	if len(runes) <= maxLen {
		return line
	}
	return string(runes[:maxLen]) + " …"
}

// textExtensionsMap is a pre-computed map for O(1) text extension lookup
// Initialized once, used by isTextFile for fast extension checking
var textExtensionsMap = map[string]bool{
	// Documentation & Text
	".txt": true, ".md": true, ".rst": true, ".asciidoc": true,
	// Go
	".go": true, ".mod": true, ".sum": true,
	// JavaScript/TypeScript ecosystem
	".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".mjs": true, ".cjs": true,
	".vue": true, ".svelte": true, ".astro": true,
	// Python
	".py": true, ".pyi": true, ".pyw": true,
	// JVM languages
	".java": true, ".kt": true, ".kts": true, ".scala": true, ".groovy": true,
	// C/C++
	".c": true, ".cpp": true, ".cc": true, ".cxx": true, ".h": true, ".hpp": true, ".hxx": true,
	// Rust
	".rs": true,
	// Ruby
	".rb": true, ".erb": true, ".rake": true,
	// PHP
	".php": true, ".phtml": true,
	// Swift/Objective-C
	".swift": true, ".m": true, ".mm": true,
	// .NET
	".cs": true, ".fs": true, ".vb": true,
	".csproj": true, ".vbproj": true, ".fsproj": true, ".sln": true,
	".aspx": true, ".ascx": true, ".ashx": true, ".asmx": true, ".asax": true,
	".cshtml": true, ".vbhtml": true, ".razor": true,
	".resx": true, ".xaml": true, ".axaml": true,
	".targets": true, ".props": true, ".nuspec": true,
	// Web
	".css": true, ".scss": true, ".sass": true, ".less": true,
	".html": true, ".htm": true, ".xhtml": true,
	// Data formats
	".xml": true, ".json": true, ".jsonc": true, ".json5": true,
	".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	// Shell/Scripts
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".bat": true, ".cmd": true, ".ps1": true, ".psm1": true,
	// Database
	".sql": true, ".prisma": true,
	// Other
	".log": true, ".csv": true, ".tsv": true,
	".conf": true, ".config": true, ".cfg": true,
	".dockerfile": true, ".containerfile": true,
	".gitignore": true, ".gitattributes": true, ".gitmodules": true,
	".editorconfig": true, ".env": true, ".envrc": true,
	".makefile": true, ".cmake": true,
	".graphql": true, ".gql": true, ".proto": true,
	".tf": true, ".tfvars": true, // Terraform
	".hcl": true,                                            // HashiCorp
	".lua": true, ".vim": true, ".el": true, ".emacs": true, // Scripting
	".r": true, ".rmd": true, // R
	".dart": true, ".ex": true, ".exs": true, // Dart, Elixir
	".zig": true, ".nim": true, ".v": true, // Modern languages
	".pl":   true, // Perl
	".lock": true, // Lock files (package-lock.json, yarn.lock, etc.)
}

// isTextCandidate is the walk-safe, zero-I/O text/binary pre-filter.
// Perf (v4.5.30): the old isTextFile opened and read 512 bytes of EVERY file
// inside the serial WalkDir callback — 2 syscalls per candidate file before
// the parallel workers even started. This function answers from the filename
// alone: known binary extensions are rejected, everything else (known text
// extensions, unknown, or no extension) is deferred to the worker, which
// validates the content it already read via isTextContent.
func isTextCandidate(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return !binaryExtensionsMap[ext]
}

// CountOccurrences counts occurrences of a pattern in a file and optionally returns line numbers
func (e *UltraFastEngine) CountOccurrences(ctx context.Context, path, pattern string, returnLines bool, caseSensitive bool, wholeWord bool, noIgnore bool) (string, error) {
	return e.CountOccurrencesOpts(ctx, SearchOptions{
		Path: path, Pattern: pattern, ReturnLines: returnLines,
		CaseSensitive: caseSensitive, WholeWord: wholeWord, NoIgnore: noIgnore,
	})
}

// CountOccurrencesOpts is the typed entry point; FileTypes are applied to
// directory walks and to a single-file target (mismatch → zero matches).
func (e *UltraFastEngine) CountOccurrencesOpts(ctx context.Context, opts SearchOptions) (string, error) {
	if err := e.acquireOperation(ctx, "count"); err != nil {
		return "", err
	}
	start := time.Now()
	defer e.releaseOperation("count", start)

	path := NormalizePath(opts.Path)
	pattern := opts.Pattern
	returnLines := opts.ReturnLines
	caseSensitive := opts.CaseSensitive
	wholeWord := opts.WholeWord
	noIgnore := opts.NoIgnore
	fileTypes := opts.FileTypes

	validPath, err := e.validatePath(path)
	if err != nil {
		return "", fmt.Errorf("path validation error: %w", err)
	}

	// Check if file exists
	info, err := os.Stat(validPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", validPath)
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	// Try to compile as regex first, fallback to literal if fails (uses engine cache)
	// Bug #32: apply case_sensitive and whole_word flags to the regex pattern,
	// matching the same logic used in performAdvancedTextSearch.
	searchPattern := pattern
	if !caseSensitive {
		searchPattern = "(?i)" + searchPattern
	}
	if wholeWord {
		searchPattern = `\b` + searchPattern + `\b`
	}

	var regexPattern *regexp.Regexp
	regexPattern, err = e.CompileRegex(searchPattern)
	if err != nil {
		// Use literal pattern with flags
		litPattern := regexp.QuoteMeta(pattern)
		if !caseSensitive {
			litPattern = "(?i)" + litPattern
		}
		if wholeWord {
			litPattern = `\b` + litPattern + `\b`
		}
		regexPattern, _ = e.CompileRegex(litPattern)
	}

	if info.IsDir() {
		return e.countOccurrencesInDir(ctx, validPath, pattern, regexPattern, returnLines, noIgnore, fileTypes)
	}
	if !FileMatchesTypeFilter(validPath, fileTypes) {
		return "0 matches", nil
	}
	return e.countOccurrencesInFile(validPath, pattern, regexPattern, returnLines)
}

// countOccurrencesInFile counts occurrences in a single file
func (e *UltraFastEngine) countOccurrencesInFile(filePath, pattern string, regexPattern *regexp.Regexp, returnLines bool) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	var matchedLines []int
	totalOccurrences := 0

	for lineNum, line := range lines {
		matches := regexPattern.FindAllString(line, -1)
		if len(matches) > 0 {
			totalOccurrences += len(matches)
			if returnLines {
				matchedLines = append(matchedLines, lineNum+1)
			}
		}
	}

	var result strings.Builder

	if e.config.CompactMode {
		result.WriteString(fmt.Sprintf("%d matches", totalOccurrences))
		if returnLines && len(matchedLines) > 0 {
			result.WriteString(" at lines: ")
			maxShow := 20
			if len(matchedLines) > maxShow {
				for i := 0; i < maxShow; i++ {
					if i > 0 {
						result.WriteString(", ")
					}
					result.WriteString(fmt.Sprintf("%d", matchedLines[i]))
				}
				result.WriteString(fmt.Sprintf("... (+%d more)", len(matchedLines)-maxShow))
			} else {
				for i, lineNum := range matchedLines {
					if i > 0 {
						result.WriteString(", ")
					}
					result.WriteString(fmt.Sprintf("%d", lineNum))
				}
			}
		}
	} else {
		result.WriteString("🔢 Pattern Occurrence Count\n")
		result.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		result.WriteString(fmt.Sprintf("📁 File: %s\n", filePath))
		result.WriteString(fmt.Sprintf("🔍 Pattern: '%s'\n", pattern))
		result.WriteString(fmt.Sprintf("📊 Total occurrences: %d\n", totalOccurrences))
		result.WriteString(fmt.Sprintf("📝 Lines with matches: %d\n", len(matchedLines)))

		if returnLines && len(matchedLines) > 0 {
			result.WriteString("\n📌 Line numbers:\n")
			maxShow := 50
			for i := 0; i < len(matchedLines) && i < maxShow; i++ {
				result.WriteString(fmt.Sprintf("  Line %d\n", matchedLines[i]))
			}
			if len(matchedLines) > maxShow {
				result.WriteString(fmt.Sprintf("  ... and %d more lines\n", len(matchedLines)-maxShow))
			}
		}
		result.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	}

	return result.String(), nil
}

// countOccurrencesInDir counts occurrences across all text files in a directory
func (e *UltraFastEngine) countOccurrencesInDir(ctx context.Context, dirPath, pattern string, regexPattern *regexp.Regexp, returnLines bool, noIgnore bool, fileTypes []string) (string, error) {
	type fileCount struct {
		path  string
		count int
		lines []int
	}

	var results []fileCount
	totalOccurrences := 0
	filesScanned := 0

	ign := NewIgnoreMatcher()
	err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != dirPath {
				return filepath.SkipDir
			}
			if skipWalkDir(d.Name(), path, dirPath, true, ign, noIgnore) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipWalkDir(d.Name(), path, dirPath, false, ign, noIgnore) {
			return nil
		}
		if !FileMatchesTypeFilter(path, fileTypes) {
			return nil
		}
		if !isTextCandidate(path) || isMinifiedFile(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		if !isTextContent(content) {
			return nil // skip binary content (BOM / null bytes)
		}

		filesScanned++
		lines := strings.Split(string(content), "\n")
		fileOccurrences := 0
		var matchedLines []int

		for lineNum, line := range lines {
			matches := regexPattern.FindAllString(line, -1)
			if len(matches) > 0 {
				fileOccurrences += len(matches)
				if returnLines {
					matchedLines = append(matchedLines, lineNum+1)
				}
			}
		}

		if fileOccurrences > 0 {
			totalOccurrences += fileOccurrences
			relPath, _ := filepath.Rel(dirPath, path)
			if relPath == "" {
				relPath = path
			}
			results = append(results, fileCount{path: relPath, count: fileOccurrences, lines: matchedLines})
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %w", err)
	}

	var out strings.Builder

	if e.config.CompactMode {
		out.WriteString(fmt.Sprintf("%d matches in %d files", totalOccurrences, len(results)))
		if len(results) > 0 {
			out.WriteString(": ")
			maxFiles := 20
			for i, fc := range results {
				if i >= maxFiles {
					out.WriteString(fmt.Sprintf("... (+%d more files)", len(results)-maxFiles))
					break
				}
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(fmt.Sprintf("%s(%d)", fc.path, fc.count))
			}
		}
	} else {
		out.WriteString("🔢 Pattern Occurrence Count (Directory)\n")
		out.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		out.WriteString(fmt.Sprintf("📁 Directory: %s\n", dirPath))
		out.WriteString(fmt.Sprintf("🔍 Pattern: '%s'\n", pattern))
		out.WriteString(fmt.Sprintf("📊 Total occurrences: %d\n", totalOccurrences))
		out.WriteString(fmt.Sprintf("📄 Files with matches: %d / %d scanned\n", len(results), filesScanned))

		if len(results) > 0 {
			out.WriteString("\n📂 Per-file breakdown:\n")
			maxFiles := 50
			for i, fc := range results {
				if i >= maxFiles {
					out.WriteString(fmt.Sprintf("  ... and %d more files\n", len(results)-maxFiles))
					break
				}
				out.WriteString(fmt.Sprintf("  %s: %d occurrences\n", fc.path, fc.count))
				if returnLines && len(fc.lines) > 0 {
					out.WriteString("    Lines: ")
					maxShow := 10
					for j, ln := range fc.lines {
						if j >= maxShow {
							out.WriteString(fmt.Sprintf("... (+%d more)", len(fc.lines)-maxShow))
							break
						}
						if j > 0 {
							out.WriteString(", ")
						}
						out.WriteString(fmt.Sprintf("%d", ln))
					}
					out.WriteString("\n")
				}
			}
		}
		out.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	}

	return out.String(), nil
}

// calculateCharacterOffset - Determine exact character position of pattern match in line
// Returns: (startOffset, endOffset) within the line
// Note: Character offsets are 0-indexed relative to the start of the line
// Uses regex to find the actual match position (handles multiple occurrences correctly)
func calculateCharacterOffset(line string, regexPattern *regexp.Regexp) (int, int) {
	// Use regex to find the exact match position
	// This handles all cases: exact strings, case-insensitive, multiple occurrences
	loc := regexPattern.FindStringIndex(line)
	if loc != nil {
		return loc[0], loc[1]
	}

	// Fallback: pattern not found, return reasonable estimate
	return 0, 0
}
