package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

	path := NormalizePath(request.Arguments["path"].(string))
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

	results, err := e.performSmartSearch(ctx, validPath, pattern, includeContent, fileTypes)
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

	path := NormalizePath(request.Arguments["path"].(string))
	pattern := request.Arguments["pattern"].(string)
	caseSensitive, _ := request.Arguments["case_sensitive"].(bool)
	wholeWord, _ := request.Arguments["whole_word"].(bool)
	includeContext, _ := request.Arguments["include_context"].(bool)
	outputFormat, _ := request.Arguments["output_format"].(string)
	if outputFormat == "" {
		outputFormat = "text"
	}

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
			result.WriteString(" (first 20)")
			maxToShow = 20
		}
		result.WriteString("\n")

		if includeContext {
			// Compact-with-context: one line per match + condensed context
			for i := 0; i < maxToShow; i++ {
				match := matches[i]
				result.WriteString(fmt.Sprintf("%s:%d[%d:%d] %s\n", match.File, match.LineNumber, match.MatchStart, match.MatchEnd, match.Line))
				if len(match.Context) > 0 {
					for _, ctxLine := range match.Context {
						result.WriteString(fmt.Sprintf("  | %s\n", ctxLine))
					}
				}
			}
		} else {
			// Compact without context: comma-separated positions
			for i := 0; i < maxToShow; i++ {
				if i > 0 {
					result.WriteString(", ")
				}
				match := matches[i]
				result.WriteString(fmt.Sprintf("%s:%d[%d:%d]", match.File, match.LineNumber, match.MatchStart, match.MatchEnd))
			}
		}

		if len(matches) > maxToShow {
			result.WriteString(fmt.Sprintf(" ... (%d more)", len(matches)-maxToShow))
		}
	} else {
		// Verbose format
		result.WriteString(fmt.Sprintf("🔍 Found %d matches for pattern '%s':\n\n", len(matches), pattern))

		for i := 0; i < maxToShow; i++ {
			match := matches[i]
			result.WriteString(fmt.Sprintf("📁 %s:%d [%d:%d]\n", match.File, match.LineNumber, match.MatchStart, match.MatchEnd))
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

	// Execute post-search hook (best-effort)
	hookCtx2.Event = HookPostSearch
	hookCtx2.Metadata["match_count"] = len(matches)
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostSearch, hookCtx2)

	// JSON output format for AI parsing
	if outputFormat == "json" {
		return &mcp.CallToolResponse{
			Content: []mcp.TextContent{
				{Text: formatSearchMatchesJSON(matches, pattern, path)},
			},
		}, nil
	}

	return &mcp.CallToolResponse{
		Content: []mcp.TextContent{
			{Text: result.String()},
		},
	}, nil
}

// formatSearchMatchesJSON formats search matches as structured JSON for AI parsing
func formatSearchMatchesJSON(matches []SearchMatch, pattern, path string) string {
	var buf strings.Builder
	buf.WriteString("{\n")
	buf.WriteString(fmt.Sprintf(`  "pattern": %s, `, jsonString(pattern)))
	buf.WriteString(fmt.Sprintf(`  "path": %s, `, jsonString(path)))
	buf.WriteString(fmt.Sprintf(`  "total_matches": %d, `, len(matches)))
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

// jsonString escapes a string for JSON
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// performSmartSearch
func (e *UltraFastEngine) performSmartSearch(ctx context.Context, path, pattern string, includeContent bool, fileTypes []string) (string, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", &ContextError{Op: "search", Details: "operation cancelled before start"}
	}

	var resultsMu sync.Mutex
	var results []string
	var contentMatches []SearchMatch
	maxResults := e.config.MaxSearchResults

	// Compile regex pattern (uses engine cache to avoid repeated compilation)
	regexPattern, err := e.CompileRegex(pattern)
	if err != nil {
		// If not valid regex, use literal search
		regexPattern, _ = e.CompileRegex(regexp.QuoteMeta(pattern))
	}

	// First pass: collect all files to search
	var filesToSearch []string
	err = filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		// Check context in walk callback
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr // Stop walk if context is cancelled
		}

		if err != nil {
			return nil // Continue with other files
		}

		// Prune common large/irrelevant directories to avoid walking thousands of binaries
		if info.IsDir() {
			if searchSkipDirs[info.Name()] {
				return filepath.SkipDir
			}
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

				// Use bufio.Scanner for memory-efficient line processing
				// Avoids allocating all lines at once (30-40% memory savings)
				scanner := bufio.NewScanner(bytes.NewReader(content))
				var localMatches []SearchMatch
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
							Line:       line, // ✅ NO TrimSpace - mantener línea original
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
					resultBuilder.WriteString(fmt.Sprintf("%s:%d[%d:%d]", m.File, m.LineNumber, m.MatchStart, m.MatchEnd))
				}
			} else {
				resultBuilder.WriteString(": ")
				for i, match := range contentMatches {
					if i > 0 {
						resultBuilder.WriteString(", ")
					}
					// Use full path instead of just basename
					resultBuilder.WriteString(fmt.Sprintf("%s:%d[%d:%d]", match.File, match.LineNumber, match.MatchStart, match.MatchEnd))
				}
			}
		} else {
			resultBuilder.WriteString(fmt.Sprintf("📝 Content matches (%d):\n", len(contentMatches)))
			for _, match := range contentMatches {
				resultBuilder.WriteString(fmt.Sprintf("  📁 %s:%d [%d:%d] - %s\n", match.File, match.LineNumber, match.MatchStart, match.MatchEnd, match.Line))
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

	regexPattern, err := e.CompileRegex(searchPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// First pass: collect all files to search
	var filesToSearch []string
	err = filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Prune common large/irrelevant directories
		if info.IsDir() {
			if searchSkipDirs[info.Name()] {
				return filepath.SkipDir
			}
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

			var localMatches []SearchMatch

			// When context is not needed, use bufio.Scanner for memory efficiency
			// When context is needed, use strings.Split (need forward-looking capability)
			if !includeContext {
				// Memory-efficient path: 30-40% memory savings with bufio.Scanner
				scanner := bufio.NewScanner(bytes.NewReader(content))
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
							Line:       line, // ✅ NO TrimSpace - mantener línea original
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
							Line:       line, // ✅ NO TrimSpace - mantener línea original
							MatchStart: matchStart,
							MatchEnd:   matchEnd,
						}

						// Add context
						var context []string
						// Use built-in min/max (Go 1.21+) - no need for helper functions
						start := max(0, lineNum-contextLines)
						end := min(len(lines), lineNum+contextLines+1)

						for i := start; i < end; i++ {
							if i != lineNum {
								context = append(context, strings.TrimSpace(lines[i]))
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

	return matches, nil
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

// isTextFile determines if a file is likely a text file
func (e *UltraFastEngine) isTextFile(path string) bool {
	// Check by extension first (O(1) map lookup - very fast)
	ext := strings.ToLower(filepath.Ext(path))
	if textExtensionsMap[ext] {
		return true
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
func (e *UltraFastEngine) CountOccurrences(ctx context.Context, path, pattern string, returnLines bool, caseSensitive bool, wholeWord bool) (string, error) {
	if err := e.acquireOperation(ctx, "count"); err != nil {
		return "", err
	}
	start := time.Now()
	defer e.releaseOperation("count", start)

	// Normalize WSL/Windows paths
	path = NormalizePath(path)

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

	// Directory mode: count across all text files in directory
	if info.IsDir() {
		return e.countOccurrencesInDir(ctx, validPath, pattern, regexPattern, returnLines)
	}

	// Single file mode
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
func (e *UltraFastEngine) countOccurrencesInDir(ctx context.Context, dirPath, pattern string, regexPattern *regexp.Regexp, returnLines bool) (string, error) {
	type fileCount struct {
		path  string
		count int
		lines []int
	}

	var results []fileCount
	totalOccurrences := 0
	filesScanned := 0

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(info.Name(), ".") && path != dirPath {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.isTextFile(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
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
