package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mcp/filesystem-ultra/mcp"
)

// SmartSearch performs intelligent search with regex and filters
func (e *UltraFastEngine) SmartSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResponse, error) {
	if err := e.acquireOperation(ctx, "search"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("search", start)

	path := request.Arguments["path"].(string)
	pattern := request.Arguments["pattern"].(string)
	includeContent, _ := request.Arguments["include_content"].(bool)

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

	results, err := e.performSmartSearch(validPath, pattern, includeContent, fileTypes)
	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: Search error: %v", err)},
			},
		}, nil
	}

	return &mcp.CallToolResponse{
		Content: []mcp.TextContent{
			{Text: results},
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

	path := request.Arguments["path"].(string)
	pattern := request.Arguments["pattern"].(string)
	caseSensitive, _ := request.Arguments["case_sensitive"].(bool)
	wholeWord, _ := request.Arguments["whole_word"].(bool)
	includeContext, _ := request.Arguments["include_context"].(bool)

	contextLines := 3
	if cl, ok := request.Arguments["context_lines"].(float64); ok {
		contextLines = int(cl)
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
				{Text: fmt.Sprintf("❌ Error: %v", err)},
			},
		}, nil
	}

	matches, err := e.performAdvancedTextSearch(validPath, pattern, caseSensitive, wholeWord, includeContext, contextLines)
	if err != nil {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("❌ Error: %v", err)},
			},
		}, nil
	}

	if len(matches) == 0 {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: fmt.Sprintf("🔍 No matches found for pattern '%s' in %s", pattern, path)},
			},
		}, nil
	}

	var result strings.Builder

	maxToShow := e.config.MaxSearchResults
	if maxToShow > len(matches) {
		maxToShow = len(matches)
	}

	if e.config.CompactMode {
		// Compact format: minimal output but with full paths
		result.WriteString(fmt.Sprintf("%d matches", len(matches)))
		if len(matches) > 20 {
			result.WriteString(" (first 20): ")
			maxToShow = 20
		} else {
			result.WriteString(": ")
		}

		for i := 0; i < maxToShow; i++ {
			if i > 0 {
				result.WriteString(", ")
			}
			match := matches[i]
			// Use full path instead of just basename
			result.WriteString(fmt.Sprintf("%s:%d", match.File, match.LineNumber))
		}

		if len(matches) > maxToShow {
			result.WriteString(fmt.Sprintf(" ... (%d more)", len(matches)-maxToShow))
		}
	} else {
		// Verbose format
		result.WriteString(fmt.Sprintf("🔍 Found %d matches for pattern '%s':\n\n", len(matches), pattern))

		for i := 0; i < maxToShow; i++ {
			match := matches[i]
			result.WriteString(fmt.Sprintf("📁 %s:%d\n", match.File, match.LineNumber))
			result.WriteString(fmt.Sprintf("   %s\n", match.Line))

			if includeContext && len(match.Context) > 0 {
				result.WriteString("   Context:\n")
				for _, contextLine := range match.Context {
					result.WriteString(fmt.Sprintf("   │ %s\n", contextLine))
				}
			}
			result.WriteString("\n")
		}

		if len(matches) > maxToShow {
			result.WriteString(fmt.Sprintf("⚠️ Showing %d of %d matches. Use more specific pattern.\n", maxToShow, len(matches)))
		}
	}

	return &mcp.CallToolResponse{
		Content: []mcp.TextContent{
			{Text: result.String()},
		},
	}, nil
}

// performSmartSearch implements intelligent search with parallelization
func (e *UltraFastEngine) performSmartSearch(path, pattern string, includeContent bool, fileTypes []string) (string, error) {
	var resultsMu sync.Mutex
	var results []string
	var contentMatches []SearchMatch
	maxResults := e.config.MaxSearchResults

	// Compile regex pattern
	regexPattern, err := regexp.Compile(pattern)
	if err != nil {
		// If not valid regex, use literal search
		regexPattern = regexp.MustCompile(regexp.QuoteMeta(pattern))
	}

	// First pass: collect all files to search
	var filesToSearch []string
	err = filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue with other files
		}

		// Validate path
		if _, err := e.validatePath(currentPath); err != nil {
			return nil
		}

		// Skip directories for content search
		if info.IsDir() {
			return nil
		}

		// Filter by file types if specified
		if len(fileTypes) > 0 {
			ext := strings.ToLower(filepath.Ext(currentPath))
			found := false
			for _, ft := range fileTypes {
				if strings.ToLower(ft) == ext {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}

		// Check filename match
		if regexPattern.MatchString(info.Name()) {
			resultsMu.Lock()
			if len(results) < maxResults {
				results = append(results, fmt.Sprintf("📄 %s", currentPath))
			}
			resultsMu.Unlock()
		}

		// Add to content search list if applicable
		if includeContent && info.Size() < 10*1024*1024 { // Increased to 10MB limit
			if e.isTextFile(currentPath) {
				filesToSearch = append(filesToSearch, currentPath)
			}
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	// Second pass: parallel content search using worker pool
	if includeContent && len(filesToSearch) > 0 {
		var wg sync.WaitGroup

		for _, filePath := range filesToSearch {
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

				// Read file
				content, err := os.ReadFile(currentFile)
				if err != nil {
					return
				}

				lines := strings.Split(string(content), "\n")
				var localMatches []SearchMatch

				for lineNum, line := range lines {
					if regexPattern.MatchString(line) {
						match := SearchMatch{
							File:       currentFile,
							LineNumber: lineNum + 1,
							Line:       strings.TrimSpace(line),
						}
						localMatches = append(localMatches, match)
					}
				}

				// Append local matches to global list
				if len(localMatches) > 0 {
					resultsMu.Lock()
					for _, match := range localMatches {
						if len(contentMatches) < maxResults {
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

	err = nil

	if err != nil {
		return "", err
	}

	var resultBuilder strings.Builder

	totalResults := len(results) + len(contentMatches)

	if len(results) > 0 {
		if e.config.CompactMode {
			resultBuilder.WriteString(fmt.Sprintf("%d filename matches", len(results)))
			if len(results) > 10 {
				resultBuilder.WriteString(" (showing first 10): ")
				for i := 0; i < 10; i++ {
					if i > 0 {
						resultBuilder.WriteString(", ")
					}
					// Use full path instead of just basename
					resultBuilder.WriteString(strings.TrimPrefix(results[i], "📄 "))
				}
			} else {
				resultBuilder.WriteString(": ")
				for i, result := range results {
					if i > 0 {
						resultBuilder.WriteString(", ")
					}
					// Use full path instead of just basename
					resultBuilder.WriteString(strings.TrimPrefix(result, "📄 "))
				}
			}
			resultBuilder.WriteString("\n")
		} else {
			resultBuilder.WriteString(fmt.Sprintf("🔍 File name matches (%d):\n", len(results)))
			for _, result := range results {
				resultBuilder.WriteString(fmt.Sprintf("  %s\n", result))
			}
			resultBuilder.WriteString("\n")
		}
	}

	if len(contentMatches) > 0 {
		if e.config.CompactMode {
			resultBuilder.WriteString(fmt.Sprintf("%d content matches", len(contentMatches)))
			if len(contentMatches) > 10 {
				resultBuilder.WriteString(" (first 10): ")
				for i := 0; i < 10; i++ {
					if i > 0 {
						resultBuilder.WriteString(", ")
					}
					m := contentMatches[i]
					// Use full path instead of just basename
					resultBuilder.WriteString(fmt.Sprintf("%s:%d", m.File, m.LineNumber))
				}
			} else {
				resultBuilder.WriteString(": ")
				for i, match := range contentMatches {
					if i > 0 {
						resultBuilder.WriteString(", ")
					}
					// Use full path instead of just basename
					resultBuilder.WriteString(fmt.Sprintf("%s:%d", match.File, match.LineNumber))
				}
			}
		} else {
			resultBuilder.WriteString(fmt.Sprintf("📝 Content matches (%d):\n", len(contentMatches)))
			for _, match := range contentMatches {
				resultBuilder.WriteString(fmt.Sprintf("  📁 %s:%d - %s\n", match.File, match.LineNumber, match.Line))
			}
		}
	}

	if len(results) == 0 && len(contentMatches) == 0 {
		return fmt.Sprintf("🔍 No matches found for pattern '%s' in %s", pattern, path), nil
	}

	if totalResults >= e.config.MaxSearchResults {
		if e.config.CompactMode {
			resultBuilder.WriteString(fmt.Sprintf(" (limited to %d)", e.config.MaxSearchResults))
		} else {
			resultBuilder.WriteString(fmt.Sprintf("\n⚠️ Results limited to %d. Use more specific pattern.\n", e.config.MaxSearchResults))
		}
	}

	return resultBuilder.String(), nil
}

// performAdvancedTextSearch implements advanced text search with parallelization
func (e *UltraFastEngine) performAdvancedTextSearch(path, pattern string, caseSensitive, wholeWord, includeContext bool, contextLines int) ([]SearchMatch, error) {
	var matchesMu sync.Mutex
	var matches []SearchMatch

	// Prepare the pattern
	searchPattern := pattern
	if !caseSensitive {
		searchPattern = "(?i)" + searchPattern
	}
	if wholeWord {
		searchPattern = `\b` + searchPattern + `\b`
	}

	regexPattern, err := regexp.Compile(searchPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// First pass: collect all files to search
	var filesToSearch []string
	err = filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Validate path
		if _, err := e.validatePath(currentPath); err != nil {
			return nil
		}

		// Only search in text files with increased size limit
		if !e.isTextFile(currentPath) || info.Size() > 10*1024*1024 { // Increased to 10MB limit
			return nil
		}

		filesToSearch = append(filesToSearch, currentPath)
		return nil
	})

	if err != nil {
		return nil, err
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

			lines := strings.Split(string(content), "\n")
			var localMatches []SearchMatch

			for lineNum, line := range lines {
				if regexPattern.MatchString(line) {
					match := SearchMatch{
						File:       currentFile,
						LineNumber: lineNum + 1,
						Line:       strings.TrimSpace(line),
					}

					// Add context if requested
					if includeContext {
						var context []string
						start := max(0, lineNum-contextLines)
						end := min(len(lines), lineNum+contextLines+1)

						for i := start; i < end; i++ {
							if i != lineNum {
								context = append(context, strings.TrimSpace(lines[i]))
							}
						}
						match.Context = context
					}

					localMatches = append(localMatches, match)
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

	return matches, nil
}

// isTextFile determines if a file is likely a text file
func (e *UltraFastEngine) isTextFile(path string) bool {
	// Check by extension first (fast)
	ext := strings.ToLower(filepath.Ext(path))
	textExtensions := []string{
		".txt", ".md", ".go", ".js", ".ts", ".py", ".java", ".c", ".cpp", ".h", ".hpp",
		".css", ".html", ".htm", ".xml", ".json", ".yaml", ".yml", ".toml", ".ini",
		".sh", ".bat", ".ps1", ".sql", ".log", ".csv", ".tsv", ".conf", ".config",
		".dockerfile", ".gitignore", ".gitattributes", ".editorconfig", ".env",
	}

	for _, textExt := range textExtensions {
		if ext == textExt {
			return true
		}
	}

	// If no extension or unknown extension, check content (slower)
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	// Read first 512 bytes to check for binary content
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return false
	}

	// Check for null bytes (common in binary files)
	for i := 0; i < n; i++ {
		if buffer[i] == 0 {
			return false
		}
	}

	return true
}

// CountOccurrences counts occurrences of a pattern in a file and optionally returns line numbers
func (e *UltraFastEngine) CountOccurrences(ctx context.Context, path, pattern string, returnLines bool) (string, error) {
	if err := e.acquireOperation(ctx, "count"); err != nil {
		return "", err
	}
	start := time.Now()
	defer e.releaseOperation("count", start)

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

	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file: %s", validPath)
	}

	// Read file
	content, err := os.ReadFile(validPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// Try to compile as regex first, fallback to literal if fails
	var regexPattern *regexp.Regexp
	regexPattern, err = regexp.Compile(pattern)
	if err != nil {
		// Use literal pattern
		regexPattern = regexp.MustCompile(regexp.QuoteMeta(pattern))
	}

	// Count occurrences and track line numbers
	var matchedLines []int
	totalOccurrences := 0

	for lineNum, line := range lines {
		matches := regexPattern.FindAllString(line, -1)
		if len(matches) > 0 {
			totalOccurrences += len(matches)
			if returnLines {
				matchedLines = append(matchedLines, lineNum+1) // 1-indexed
			}
		}
	}

	// Build response
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
		result.WriteString(fmt.Sprintf("🔢 Pattern Occurrence Count\n"))
		result.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"))
		result.WriteString(fmt.Sprintf("📁 File: %s\n", validPath))
		result.WriteString(fmt.Sprintf("🔍 Pattern: '%s'\n", pattern))
		result.WriteString(fmt.Sprintf("📊 Total occurrences: %d\n", totalOccurrences))
		result.WriteString(fmt.Sprintf("📝 Lines with matches: %d\n", len(matchedLines)))

		if returnLines && len(matchedLines) > 0 {
			result.WriteString(fmt.Sprintf("\n📌 Line numbers:\n"))
			maxShow := 50
			for i := 0; i < len(matchedLines) && i < maxShow; i++ {
				result.WriteString(fmt.Sprintf("  Line %d\n", matchedLines[i]))
			}
			if len(matchedLines) > maxShow {
				result.WriteString(fmt.Sprintf("  ... and %d more lines\n", len(matchedLines)-maxShow))
			}
		}
		result.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"))
	}

	return result.String(), nil
}

// Helper functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
