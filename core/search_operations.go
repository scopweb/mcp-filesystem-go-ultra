package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// performSmartSearch implements intelligent search
func (e *UltraFastEngine) performSmartSearch(path, pattern string, includeContent bool, fileTypes []string) (string, error) {
	var results []string
	var contentMatches []SearchMatch
	maxResults := e.config.MaxSearchResults

	// Compile regex pattern
	regexPattern, err := regexp.Compile(pattern)
	if err != nil {
		// If not valid regex, use literal search
		regexPattern = regexp.MustCompile(regexp.QuoteMeta(pattern))
	}

	err = filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue with other files
		}

		// Validate path
		if _, err := e.validatePath(currentPath); err != nil {
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

		// Search in filename
		if regexPattern.MatchString(info.Name()) {
			if len(results) < maxResults {
				results = append(results, fmt.Sprintf("📄 %s", currentPath))
			}
		}

		// Search in content if requested and it's a text file
		if includeContent && !info.IsDir() && info.Size() < 5*1024*1024 { // 5MB limit
			if e.isTextFile(currentPath) {
				content, err := os.ReadFile(currentPath)
				if err == nil {
					lines := strings.Split(string(content), "\n")
					for lineNum, line := range lines {
						if len(contentMatches) >= maxResults {
							break
						}
						if regexPattern.MatchString(line) {
							match := SearchMatch{
								File:       currentPath,
								LineNumber: lineNum + 1,
								Line:       strings.TrimSpace(line),
							}
							contentMatches = append(contentMatches, match)
						}
					}
				}
			}
		}

		return nil
	})

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

// performAdvancedTextSearch implements advanced text search
func (e *UltraFastEngine) performAdvancedTextSearch(path, pattern string, caseSensitive, wholeWord, includeContext bool, contextLines int) ([]SearchMatch, error) {
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
		return nil, fmt.Errorf("invalid regex pattern: %v", err)
	}

	err = filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Validate path
		if _, err := e.validatePath(currentPath); err != nil {
			return nil
		}

		// Only search in text files
		if !e.isTextFile(currentPath) || info.Size() > 5*1024*1024 { // 5MB limit
			return nil
		}

		content, err := os.ReadFile(currentPath)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			if regexPattern.MatchString(line) {
				match := SearchMatch{
					File:       currentPath,
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

				matches = append(matches, match)
			}
		}

		return nil
	})

	return matches, err
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

// Helper functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
