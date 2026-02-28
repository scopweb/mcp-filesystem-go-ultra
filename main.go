package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	localmcp "github.com/mcp/filesystem-ultra/mcp"
)

// Configuration holds all server configuration
type Configuration struct {
	CacheSize        int64    // Cache size in bytes
	ParallelOps      int      // Max concurrent operations
	BinaryThreshold  int64    // File size threshold for binary protocol
	VSCodeAPIEnabled bool     // Enable VSCode API integration when available
	DebugMode        bool     // Enable debug logging
	LogLevel         string   // Log level (info, debug, error)
	AllowedPaths     []string // List of allowed base paths for access control
	CompactMode      bool     // Enable compact responses (minimal tokens)
	MaxResponseSize  int64    // Max response size in bytes
	MaxSearchResults int      // Max search results to return
	MaxListItems     int      // Max items in directory listings
}

// DefaultConfiguration returns optimized defaults based on system
func DefaultConfiguration() *Configuration {
	// Auto-detect optimal settings based on system resources
	cpuCount := runtime.NumCPU()
	parallelOps := cpuCount * 2 // 2x CPU cores for I/O bound operations
	if parallelOps > 16 {
		parallelOps = 16 // Cap at 16 to avoid overhead
	}

	return &Configuration{
		CacheSize:        100 * 1024 * 1024, // 100MB default
		ParallelOps:      parallelOps,
		BinaryThreshold:  1024 * 1024, // 1MB threshold
		VSCodeAPIEnabled: true,
		DebugMode:        false,
		LogLevel:         "info",
		AllowedPaths:     []string{},       // No restrictions by default
		CompactMode:      false,            // Verbose by default
		MaxResponseSize:  10 * 1024 * 1024, // 10MB default
		MaxSearchResults: 1000,             // 1000 results default
		MaxListItems:     500,              // 500 items default
	}
}

func main() {
	config := DefaultConfiguration()

	// Parse command line arguments
	var (
		cacheSize        = flag.String("cache-size", "100MB", "Memory cache limit (e.g., 50MB, 1GB)")
		parallelOps      = flag.Int("parallel-ops", config.ParallelOps, "Max concurrent operations")
		binaryThreshold  = flag.String("binary-threshold", "1MB", "File size threshold for binary protocol")
		vsCodeAPI        = flag.Bool("vscode-api", true, "Enable VSCode API integration when available")
		debugMode        = flag.Bool("debug", false, "Enable debug mode")
		logLevel         = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		allowedPaths     = flag.String("allowed-paths", "", "Comma-separated list of allowed base paths for access control (alternative: pass paths as individual arguments)")
		compactMode      = flag.Bool("compact-mode", false, "Enable compact responses (minimal tokens for Claude Desktop)")
		maxResponseSize  = flag.String("max-response-size", "10MB", "Maximum response size")
		maxSearchResults = flag.Int("max-search-results", 1000, "Maximum search results to return")
		maxListItems     = flag.Int("max-list-items", 500, "Maximum items in directory listings")
		hooksEnabled     = flag.Bool("hooks-enabled", false, "Enable hooks system for pre/post operation validation and formatting")
		hooksConfig      = flag.String("hooks-config", "", "Path to hooks configuration JSON file (e.g., hooks.json)")
		version          = flag.Bool("version", false, "Show version information")
		benchmark        = flag.Bool("bench", false, "Run performance benchmark")

		// Backup configuration
		backupDir      = flag.String("backup-dir", "", "Directory for backup storage (default: temp/mcp-batch-backups)")
		backupMaxAge   = flag.Int("backup-max-age", 7, "Max age of backups in days")
		backupMaxCount = flag.Int("backup-max-count", 100, "Max number of backups to keep")

		// Risk thresholds
		riskThresholdMedium   = flag.Float64("risk-threshold-medium", 30.0, "Percentage change threshold for medium risk")
		riskThresholdHigh     = flag.Float64("risk-threshold-high", 50.0, "Percentage change threshold for high risk")
		riskOccurrencesMedium = flag.Int("risk-occurrences-medium", 50, "Number of occurrences threshold for medium risk")
		riskOccurrencesHigh   = flag.Int("risk-occurrences-high", 100, "Number of occurrences threshold for high risk")
	)
	flag.Parse()

	if *version {
		fmt.Printf("MCP Filesystem Server Ultra-Fast v3.14.0\n")
		fmt.Printf("Protocol: MCP 2025-11-25\n")
		fmt.Printf("Build: %s\n", time.Now().Format("2006-01-02"))
		fmt.Printf("Go: %s\n", runtime.Version())
		fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	// Parse cache size
	if size, err := parseSize(*cacheSize); err != nil {
		log.Fatalf("Invalid cache size: %v", err)
	} else {
		config.CacheSize = size
	}

	// Parse binary threshold
	if threshold, err := parseSize(*binaryThreshold); err != nil {
		log.Fatalf("Invalid binary threshold: %v", err)
	} else {
		config.BinaryThreshold = threshold
	}

	config.ParallelOps = *parallelOps
	config.VSCodeAPIEnabled = *vsCodeAPI
	config.DebugMode = *debugMode
	config.LogLevel = *logLevel
	config.CompactMode = *compactMode
	config.MaxSearchResults = *maxSearchResults
	config.MaxListItems = *maxListItems

	// Parse max response size
	if size, err := parseSize(*maxResponseSize); err != nil {
		log.Fatalf("Invalid max response size: %v", err)
	} else {
		config.MaxResponseSize = size
	}

	// Parse allowed paths - support both formats:
	// 1. Single --allowed-paths flag with comma-separated values
	// 2. Multiple individual path arguments after all flags
	if *allowedPaths != "" {
		// Format 1: comma-separated string
		config.AllowedPaths = strings.Split(*allowedPaths, ",")
		for i, path := range config.AllowedPaths {
			config.AllowedPaths[i] = strings.TrimSpace(path)
		}
	} else {
		// Format 2: check for additional arguments as individual paths
		additionalArgs := flag.Args()
		if len(additionalArgs) > 0 {
			config.AllowedPaths = additionalArgs
		}
	}

	// Setup logging
	setupLogging(config)

	log.Printf("🚀 Starting MCP Filesystem Server Ultra-Fast")
	log.Printf("📊 Config: Cache=%s, Parallel=%d, Binary=%s, VSCode=%v, Compact=%v",
		formatSize(config.CacheSize), config.ParallelOps,
		formatSize(config.BinaryThreshold), config.VSCodeAPIEnabled, config.CompactMode)

	if *benchmark {
		runBenchmark(config)
		return
	}

	// Initialize components
	ctx := context.Background()

	// Initialize cache system
	cacheSystem, err := cache.NewIntelligentCache(config.CacheSize)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}
	defer cacheSystem.Close()

	// Initialize core engine
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:            cacheSystem,
		ParallelOps:      config.ParallelOps,
		VSCodeAPIEnabled: config.VSCodeAPIEnabled,
		DebugMode:        config.DebugMode,
		AllowedPaths:     config.AllowedPaths,
		BinaryThreshold:  config.BinaryThreshold,
		CompactMode:      config.CompactMode,
		MaxResponseSize:  config.MaxResponseSize,
		MaxSearchResults: config.MaxSearchResults,
		MaxListItems:     config.MaxListItems,
		HooksEnabled:     *hooksEnabled,
		HooksConfigPath:  *hooksConfig,

		// Backup configuration
		BackupDir:      *backupDir,
		BackupMaxAge:   *backupMaxAge,
		BackupMaxCount: *backupMaxCount,

		// Risk thresholds
		RiskThresholdMedium:   *riskThresholdMedium,
		RiskThresholdHigh:     *riskThresholdHigh,
		RiskOccurrencesMedium: *riskOccurrencesMedium,
		RiskOccurrencesHigh:   *riskOccurrencesHigh,
	})
	if err != nil {
		log.Fatalf("Failed to initialize engine: %v", err)
	}
	defer engine.Close()

	// Create MCP server using mark3labs SDK
	s := server.NewMCPServer(
		"filesystem-ultra",
		"3.13.2",
		server.WithToolCapabilities(true), // listChanged=true enables tools/list_changed notifications
	)

	// Register tools
	if err := registerTools(s, engine); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	// Register large file processing tools (modular - new in v3.12.0)
	if err := registerLargeFileTools(s, engine); err != nil {
		log.Fatalf("Failed to register large file tools: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start performance monitoring
	go engine.StartMonitoring(ctx)

	log.Printf("✅ Server ready - Waiting for connections...")

	// Start the stdio server using new API
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// registerTools registers all optimized filesystem tools
func registerTools(s *server.MCPServer, engine *core.UltraFastEngine) error {
	// Read file tool
	readTool := mcp.NewTool("read_file",
		mcp.WithDescription("Read file (cached, fast)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file")),
		mcp.WithNumber("max_lines", mcp.Description("Max lines (optional, 0=all)")),
		mcp.WithString("mode", mcp.Description("Mode: all, head, tail")),
	)
	s.AddTool(readTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Get optional parameters from Arguments map
		maxLines := 0
		mode := "all"

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if ml, ok := args["max_lines"].(float64); ok {
				maxLines = int(ml)
			}
			if m, ok := args["mode"].(string); ok && m != "" {
				mode = m
			}
		}

		content, err := engine.ReadFileContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		// Apply truncation if requested
		if maxLines > 0 || mode != "all" {
			content = truncateContent(content, maxLines, mode)
		}

		return mcp.NewToolResultText(content), nil
	})

	// Write file tool
	writeTool := mcp.NewTool("write_file",
		mcp.WithDescription("Write file (atomic)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write the file")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write to the file")),
	)
	s.AddTool(writeTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
		}

		err = engine.WriteFileContent(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s written", formatSize(int64(len(content))))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)), nil
	})

	// Create file tool (alias for write_file for compatibility with Claude Desktop)
	createFileTool := mcp.NewTool("create_file",
		mcp.WithDescription("Create/write file (atomic) - alias for write_file. Do NOT use for backups or copying files; use copy_file instead."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to create/write the file")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write to the file")),
	)
	s.AddTool(createFileTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
		}

		err = engine.WriteFileContent(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s written", formatSize(int64(len(content))))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully created %d bytes in %s", len(content), path)), nil
	})

	// Write base64 tool - for binary files from container/sandbox environments
	writeBase64Tool := mcp.NewTool("write_base64",
		mcp.WithDescription("Write binary file from base64 content. Use this to copy files from Linux container to Windows: first base64-encode the file in the container, then pass the encoded content here."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Destination path (Windows format, e.g., C:\\Users\\...)")),
		mcp.WithString("content_base64", mcp.Required(), mcp.Description("File content encoded in base64")),
	)
	s.AddTool(writeBase64Tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		contentBase64, err := request.RequireString("content_base64")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content_base64: %v", err)), nil
		}

		// Validate base64 before passing to engine (fast fail)
		if _, decodeErr := base64.StdEncoding.DecodeString(contentBase64); decodeErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid base64: %v", decodeErr)), nil
		}

		bytesWritten, err := engine.WriteBase64(ctx, path, contentBase64)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s written", formatSize(int64(bytesWritten)))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes (from base64) to %s", bytesWritten, path)), nil
	})

	// Read base64 tool - for binary files to container/sandbox environments
	readBase64Tool := mcp.NewTool("read_base64",
		mcp.WithDescription("Read file as base64. Use this to copy binary files from Windows to Linux container: read the file as base64, then decode it in the container."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to read (Windows format)")),
	)
	s.AddTool(readBase64Tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		encoded, originalSize, err := engine.ReadBase64(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(encoded), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("# File: %s (%d bytes)\n# Base64 encoded:\n%s", path, originalSize, encoded)), nil
	})

	// List directory tool
	listTool := mcp.NewTool("list_directory",
		mcp.WithDescription("List directory (cached)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the directory to list")),
	)
	s.AddTool(listTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		listing, err := engine.ListDirectoryContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(listing), nil
	})

	// Edit file tool
	editTool := mcp.NewTool("edit_file",
		mcp.WithDescription("Edit file (smart, backup, risk validation)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if HIGH/CRITICAL risk (default: false)")),
	)
	s.AddTool(editTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		oldText, err := request.RequireString("old_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid old_text: %v", err)), nil
		}

		newText, err := request.RequireString("new_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid new_text: %v", err)), nil
		}

		// Extract force parameter
		force := false
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if f, ok := args["force"].(bool); ok {
				force = f
			}
		}

		result, err := engine.EditFile(path, oldText, newText, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %d changes", result.ReplacementCount)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully edited %s\n📊 Changes: %d replacement(s)\n🎯 Match confidence: %s\n📝 Lines affected: %d",
			path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)), nil
	})

	// Performance stats tool
	statsTool := mcp.NewTool("performance_stats",
		mcp.WithDescription("Get performance stats"),
	)
	s.AddTool(statsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		stats := engine.GetPerformanceStats()
		return mcp.NewToolResultText(stats), nil
	})

	// Capture last artifact tool
	captureLastTool := mcp.NewTool("capture_last_artifact",
		mcp.WithDescription("Store artifact in memory"),
		mcp.WithString("content", mcp.Required(), mcp.Description("Artifact code content")),
	)
	s.AddTool(captureLastTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
		}

		err = engine.CaptureLastArtifact(ctx, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		lines := strings.Count(content, "\n") + 1
		return mcp.NewToolResultText(fmt.Sprintf("Captured artifact: %d bytes, %d lines", len(content), lines)), nil
	})

	// Write last artifact tool
	writeLastTool := mcp.NewTool("write_last_artifact",
		mcp.WithDescription("Write artifact to file"),
		mcp.WithString("path", mcp.Required(), mcp.Description("FULL file path including directory and filename (e.g., C:\\temp\\script.py)")),
	)
	s.AddTool(writeLastTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		err = engine.WriteLastArtifact(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Wrote last artifact to: %s", path)), nil
	})

	// Artifact info tool
	artifactInfoTool := mcp.NewTool("artifact_info",
		mcp.WithDescription("Get artifact info"),
	)
	s.AddTool(artifactInfoTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		info := engine.GetLastArtifactInfo()
		return mcp.NewToolResultText(info), nil
	})

	// Search & replace tool
	searchReplaceTool := mcp.NewTool("search_and_replace",
		mcp.WithDescription("Recursive search & replace"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Base file or directory path")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal to search")),
		mcp.WithString("replacement", mcp.Required(), mcp.Description("Replacement text")),
	)
	s.AddTool(searchReplaceTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		replacement, err := request.RequireString("replacement")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := engine.SearchAndReplace(path, pattern, replacement, false)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Content) > 0 {
			return mcp.NewToolResultText(resp.Content[0].Text), nil
		}
		return mcp.NewToolResultText("No output"), nil
	})

	// Smart search tool
	smartSearchTool := mcp.NewTool("smart_search",
		mcp.WithDescription("Search files by name/content"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Base directory or file")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
		mcp.WithBoolean("include_content", mcp.Description("Include file content search (default: false)")),
		mcp.WithString("file_types", mcp.Description("Comma-separated file extensions (e.g., '.go,.txt')")),
	)
	s.AddTool(smartSearchTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Extract optional parameters
		includeContent := false
		fileTypes := []interface{}{}

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if ic, ok := args["include_content"].(bool); ok {
				includeContent = ic
			}
			if ft, ok := args["file_types"].(string); ok && ft != "" {
				// Parse comma-separated extensions
				parts := strings.Split(ft, ",")
				for _, part := range parts {
					fileTypes = append(fileTypes, strings.TrimSpace(part))
				}
			}
		}

		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{"path": path, "pattern": pattern, "include_content": includeContent, "file_types": fileTypes}}
		resp, err := engine.SmartSearch(ctx, engineReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Content) > 0 {
			return mcp.NewToolResultText(resp.Content[0].Text), nil
		}
		return mcp.NewToolResultText("No matches"), nil
	})

	// Advanced text search tool
	advancedTextSearchTool := mcp.NewTool("advanced_text_search",
		mcp.WithDescription("Advanced text search with context"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory or file")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive search (default: false)")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false)")),
		mcp.WithBoolean("include_context", mcp.Description("Include context lines (default: false)")),
		mcp.WithNumber("context_lines", mcp.Description("Number of context lines (default: 3)")),
	)
	s.AddTool(advancedTextSearchTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Extract optional parameters
		caseSensitive := false
		wholeWord := false
		includeContext := false
		contextLines := 3

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if cs, ok := args["case_sensitive"].(bool); ok {
				caseSensitive = cs
			}
			if ww, ok := args["whole_word"].(bool); ok {
				wholeWord = ww
			}
			if ic, ok := args["include_context"].(bool); ok {
				includeContext = ic
			}
			if cl, ok := args["context_lines"].(float64); ok {
				contextLines = int(cl)
			}
		}

		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{"path": path, "pattern": pattern, "case_sensitive": caseSensitive, "whole_word": wholeWord, "include_context": includeContext, "context_lines": contextLines}}
		resp, err := engine.AdvancedTextSearch(ctx, engineReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Content) > 0 {
			return mcp.NewToolResultText(resp.Content[0].Text), nil
		}
		return mcp.NewToolResultText("No matches"), nil
	})
	// Rename file tool
	renameTool := mcp.NewTool("rename_file",
		mcp.WithDescription("Rename file/dir"),
		mcp.WithString("old_path", mcp.Required(), mcp.Description("Current path of the file/directory")),
		mcp.WithString("new_path", mcp.Required(), mcp.Description("New path for the file/directory")),
	)
	s.AddTool(renameTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		oldPath, err := request.RequireString("old_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid old_path: %v", err)), nil
		}

		newPath, err := request.RequireString("new_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid new_path: %v", err)), nil
		}

		err = engine.RenameFile(ctx, oldPath, newPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully renamed '%s' to '%s'", oldPath, newPath)), nil
	})

	// Soft delete file tool
	softDeleteTool := mcp.NewTool("soft_delete_file",
		mcp.WithDescription("Safe delete (to trash)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file/directory to delete")),
	)
	s.AddTool(softDeleteTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		err = engine.SoftDeleteFile(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully moved '%s' to filesdelete folder", path)), nil
	})

	// Streaming write file tool (for large files)
	streamingWriteTool := mcp.NewTool("streaming_write_file",
		mcp.WithDescription("Write large files (chunked)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write the file")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write to the file")),
	)
	s.AddTool(streamingWriteTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
		}

		err = engine.StreamingWriteFile(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully wrote %s using intelligent streaming", formatSize(int64(len(content))))), nil
	})

	// Chunked read file tool (for large files)
	chunkedReadTool := mcp.NewTool("chunked_read_file",
		mcp.WithDescription("Read large files (chunked)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to read")),
		mcp.WithNumber("max_chunk_size", mcp.Description("Maximum chunk size in bytes (default: 32768)")),
	)
	s.AddTool(chunkedReadTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		maxChunkSize := mcp.ParseInt(request, "max_chunk_size", 32*1024)

		content, err := engine.ChunkedReadFile(ctx, path, maxChunkSize)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(content), nil
	})

	// Smart edit file tool (handles large files)
	smartEditTool := mcp.NewTool("smart_edit_file",
		mcp.WithDescription("Edit large files (smart)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
		mcp.WithNumber("max_file_size", mcp.Description("Max file size for regular edit (default: 1MB)")),
	)
	s.AddTool(smartEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		oldText, err := request.RequireString("old_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid old_text: %v", err)), nil
		}

		newText, err := request.RequireString("new_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid new_text: %v", err)), nil
		}

		maxFileSize := int64(mcp.ParseInt(request, "max_file_size", 1024*1024))

		result, err := engine.SmartEditFile(ctx, path, oldText, newText, maxFileSize)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Smart edit completed on %s\n📊 Changes: %d replacement(s)\n🎯 Match confidence: %s\n📝 Lines affected: %d",
			path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)), nil
	})

	// File analysis tool
	fileAnalysisTool := mcp.NewTool("analyze_file",
		mcp.WithDescription("Analyze file and recommend optimal operation strategy"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to analyze")),
	)
	s.AddTool(fileAnalysisTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		analysis, err := engine.GetFileAnalysis(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(analysis), nil
	})

	// Intelligent operations (automatic optimization for Claude Desktop)
	intelligentWriteTool := mcp.NewTool("intelligent_write",
		mcp.WithDescription("Automatically optimized write for Claude Desktop (chooses direct or streaming)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write the file")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write to the file")),
	)
	s.AddTool(intelligentWriteTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
		}

		err = engine.IntelligentWrite(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Intelligently wrote %s to %s", formatSize(int64(len(content))), path)), nil
	})

	intelligentReadTool := mcp.NewTool("intelligent_read",
		mcp.WithDescription("Automatically optimized read for Claude Desktop (chooses direct or chunked)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to read")),
	)
	s.AddTool(intelligentReadTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		content, err := engine.IntelligentRead(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(content), nil
	})

	intelligentEditTool := mcp.NewTool("intelligent_edit",
		mcp.WithDescription("Automatically optimized edit for Claude Desktop (chooses direct or smart edit). Blocks HIGH/CRITICAL risk."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if HIGH/CRITICAL risk (default: false)")),
	)
	s.AddTool(intelligentEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		oldText, err := request.RequireString("old_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid old_text: %v", err)), nil
		}

		newText, err := request.RequireString("new_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid new_text: %v", err)), nil
		}

		// Extract force parameter
		force := false
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if f, ok := args["force"].(bool); ok {
				force = f
			}
		}

		result, err := engine.IntelligentEdit(ctx, path, oldText, newText, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Intelligent edit completed on %s\n📊 Changes: %d replacement(s)\n🎯 Match confidence: %s\n📝 Lines affected: %d",
			path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)), nil
	})

	// DEPRECATED: Advanced recovery edit. Redirects to intelligent_edit for stability.
	recoveryEditTool := mcp.NewTool("recovery_edit",
		mcp.WithDescription("[DEPRECATED] Edit with automatic error recovery. Redirects to intelligent_edit. Blocks HIGH/CRITICAL risk."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if HIGH/CRITICAL risk (default: false)")),
	)
	s.AddTool(recoveryEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		oldText, err := request.RequireString("old_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid old_text: %v", err)), nil
		}

		newText, err := request.RequireString("new_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid new_text: %v", err)), nil
		}

		// Extract force parameter
		force := false
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if f, ok := args["force"].(bool); ok {
				force = f
			}
		}

		result, err := engine.AutoRecoveryEdit(ctx, path, oldText, newText, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Recovery edit completed on %s\n📊 Changes: %d replacement(s)\n🎯 Match confidence: %s\n📝 Lines affected: %d",
			path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)), nil
	})

	// Optimization suggestion tool
	optimizationSuggestionTool := mcp.NewTool("get_optimization_suggestion",
		mcp.WithDescription("Get Claude Desktop optimization suggestions for a file"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to analyze")),
	)
	s.AddTool(optimizationSuggestionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		suggestion, err := engine.GetOptimizationSuggestion(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(suggestion), nil
	})

	// Create directory tool
	createDirTool := mcp.NewTool("create_directory",
		mcp.WithDescription("Create a new directory (and parent directories if needed)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the directory to create")),
	)
	s.AddTool(createDirTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		err = engine.CreateDirectory(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s created", path)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully created directory: %s", path)), nil
	})

	// Delete file tool
	deleteTool := mcp.NewTool("delete_file",
		mcp.WithDescription("Permanently delete a file or directory (use soft_delete_file for safer deletion)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file or directory to delete")),
	)
	s.AddTool(deleteTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		err = engine.DeleteFile(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s deleted", path)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully deleted: %s", path)), nil
	})

	// Move file tool
	moveTool := mcp.NewTool("move_file",
		mcp.WithDescription("Move a file or directory to a new location"),
		mcp.WithString("source_path", mcp.Required(), mcp.Description("Current path of the file/directory")),
		mcp.WithString("dest_path", mcp.Required(), mcp.Description("New path for the file/directory")),
	)
	s.AddTool(moveTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourcePath, err := request.RequireString("source_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid source_path: %v", err)), nil
		}

		destPath, err := request.RequireString("dest_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid dest_path: %v", err)), nil
		}

		err = engine.MoveFile(ctx, sourcePath, destPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: moved to %s", destPath)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully moved '%s' to '%s'", sourcePath, destPath)), nil
	})

	// Copy file tool
	copyTool := mcp.NewTool("copy_file",
		mcp.WithDescription("Copy a file or directory to a new location. Use this for backups or duplication instead of reading and writing."),
		mcp.WithString("source_path", mcp.Required(), mcp.Description("Path of the file/directory to copy")),
		mcp.WithString("dest_path", mcp.Required(), mcp.Description("Destination path for the copy")),
	)
	s.AddTool(copyTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourcePath, err := request.RequireString("source_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid source_path: %v", err)), nil
		}

		destPath, err := request.RequireString("dest_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid dest_path: %v", err)), nil
		}

		err = engine.CopyFile(ctx, sourcePath, destPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: copied to %s", destPath)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully copied '%s' to '%s'", sourcePath, destPath)), nil
	})

	// Get file info tool
	fileInfoTool := mcp.NewTool("get_file_info",
		mcp.WithDescription("Get detailed information about a file or directory"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file or directory")),
	)
	s.AddTool(fileInfoTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		info, err := engine.GetFileInfo(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(info), nil
	})

	// Plan Mode: Analyze write change
	analyzeWriteTool := mcp.NewTool("analyze_write",
		mcp.WithDescription("Analyze a write operation without executing (Plan Mode / dry-run)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content that would be written")),
	)
	s.AddTool(analyzeWriteTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
		}

		analysis, err := engine.AnalyzeWriteChange(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		// Format analysis as text
		result := formatChangeAnalysis(analysis)
		return mcp.NewToolResultText(result), nil
	})

	// Plan Mode: Analyze edit change
	analyzeEditTool := mcp.NewTool("analyze_edit",
		mcp.WithDescription("Analyze an edit operation without executing (Plan Mode / dry-run)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("Replacement text")),
	)
	s.AddTool(analyzeEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		oldText, err := request.RequireString("old_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid old_text: %v", err)), nil
		}

		newText, err := request.RequireString("new_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid new_text: %v", err)), nil
		}

		analysis, err := engine.AnalyzeEditChange(ctx, path, oldText, newText)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		result := formatChangeAnalysis(analysis)
		return mcp.NewToolResultText(result), nil
	})

	// Plan Mode: Analyze delete change
	analyzeDeleteTool := mcp.NewTool("analyze_delete",
		mcp.WithDescription("Analyze a delete operation without executing (Plan Mode / dry-run)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file or directory")),
	)
	s.AddTool(analyzeDeleteTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		analysis, err := engine.AnalyzeDeleteChange(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		result := formatChangeAnalysis(analysis)
		return mcp.NewToolResultText(result), nil
	})

	// Batch operations tool
	batchOpsTool := mcp.NewTool("batch_operations",
		mcp.WithDescription("Execute multiple file operations atomically. Supports: write, edit, copy, move, delete, create_dir. Example: {\"operations\":[{\"type\":\"copy\",\"source\":\"file.txt\",\"destination\":\"backup.txt\"}],\"atomic\":true}"),
		mcp.WithString("request_json", mcp.Required(), mcp.Description("JSON with operations array and options. Fields: operations (array), atomic (bool), create_backup (bool), validate_only (bool)")),
	)
	s.AddTool(batchOpsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestJSON, err := request.RequireString("request_json")
		if err != nil {
			return mcp.NewToolResultError("request_json parameter is required"), nil
		}

		// Parse full request from JSON
		var batchReq core.BatchRequest
		if err := json.Unmarshal([]byte(requestJSON), &batchReq); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid request JSON: %v", err)), nil
		}

		// Set defaults if not provided
		if batchReq.Operations == nil || len(batchReq.Operations) == 0 {
			return mcp.NewToolResultError("operations array is required and cannot be empty"), nil
		}

		// Execute batch using batch manager
		batchManager := core.NewBatchOperationManager("", 10)
		result := batchManager.ExecuteBatch(batchReq)

		// Format result
		resultText := formatBatchResult(result)

		if !result.Success {
			return mcp.NewToolResultError(resultText), nil
		}

		return mcp.NewToolResultText(resultText), nil
	})

	// Read file range tool
	readRangeTool := mcp.NewTool("read_file_range",
		mcp.WithDescription("Read specific line range from file (efficient for large files)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to read")),
		mcp.WithNumber("start_line", mcp.Required(), mcp.Description("Starting line number (1-indexed)")),
		mcp.WithNumber("end_line", mcp.Required(), mcp.Description("Ending line number (inclusive)")),
	)
	s.AddTool(readRangeTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		startLine := mcp.ParseInt(request, "start_line", 1)
		endLine := mcp.ParseInt(request, "end_line", 100)

		content, err := engine.ReadFileRange(ctx, path, startLine, endLine)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(content), nil
	})

	// Count occurrences tool
	countOccurrencesTool := mcp.NewTool("count_occurrences",
		mcp.WithDescription("Count pattern occurrences in file and optionally return line numbers"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to search")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Pattern to search for (regex or literal)")),
		mcp.WithString("return_lines", mcp.Description("Return line numbers of matches (true/false, default: false)")),
	)
	s.AddTool(countOccurrencesTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid pattern: %v", err)), nil
		}

		returnLines := false
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if rl, ok := args["return_lines"].(string); ok {
				returnLines = (rl == "true" || rl == "True" || rl == "TRUE")
			} else if rl, ok := args["return_lines"].(bool); ok {
				returnLines = rl
			}
		}

		result, err := engine.CountOccurrences(ctx, path, pattern, returnLines)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(result), nil
	})

	// Replace nth occurrence tool
	replaceNthTool := mcp.NewTool("replace_nth_occurrence",
		mcp.WithDescription("Replace specific occurrence of pattern (first, last, or N-th)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Pattern to search for (regex or literal)")),
		mcp.WithString("replacement", mcp.Required(), mcp.Description("Replacement text")),
		mcp.WithNumber("occurrence", mcp.Required(), mcp.Description("Which occurrence to replace (-1=last, 1=first, 2=second, etc.)")),
		mcp.WithString("whole_word", mcp.Description("Match whole words only (true/false, default: false)")),
	)
	s.AddTool(replaceNthTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid pattern: %v", err)), nil
		}

		replacement, err := request.RequireString("replacement")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid replacement: %v", err)), nil
		}

		occurrence := mcp.ParseInt(request, "occurrence", -1)

		wholeWord := false
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if ww, ok := args["whole_word"].(string); ok {
				wholeWord = (ww == "true" || ww == "True" || ww == "TRUE")
			} else if ww, ok := args["whole_word"].(bool); ok {
				wholeWord = ww
			}
		}

		result, err := engine.ReplaceNthOccurrence(ctx, path, pattern, replacement, occurrence, wholeWord)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: replaced occurrence #%d", occurrence)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully replaced occurrence #%d\n📊 Line affected: %d\n🎯 Confidence: %s",
			occurrence, result.LinesAffected, result.MatchConfidence)), nil
	})

	// Telemetry tool - Monitor edit patterns
	telemetryTool := mcp.NewTool("get_edit_telemetry",
		mcp.WithDescription("Get telemetry data about edit operations (helps identify full rewrites vs targeted edits)"),
	)
	s.AddTool(telemetryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		summary := engine.GetEditTelemetrySummary()

		// Format as readable JSON
		data, _ := json.MarshalIndent(summary, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("📊 Edit Telemetry Summary:\n\n%s", string(data))), nil
	})

	// Multi-edit tool - ULTRA-FAST multiple edits in single file
	multiEditTool := mcp.NewTool("multi_edit",
		mcp.WithDescription("Apply multiple edits to a single file atomically. MUCH faster than calling edit_file multiple times. File is read once, all edits applied in memory, then written once."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("edits_json", mcp.Required(), mcp.Description("JSON array of edits: [{\"old_text\": \"...\", \"new_text\": \"...\"}, ...]")),
	)
	s.AddTool(multiEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		editsJSON, err := request.RequireString("edits_json")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid edits_json: %v", err)), nil
		}

		// Parse edits from JSON
		var edits []core.MultiEditOperation
		if err := json.Unmarshal([]byte(editsJSON), &edits); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid edits JSON: %v", err)), nil
		}

		if len(edits) == 0 {
			return mcp.NewToolResultError("edits array cannot be empty"), nil
		}

		// Execute multi-edit
		result, err := engine.MultiEdit(ctx, path, edits)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Multi-edit error: %v", err)), nil
		}

		// Format result
		if engine.IsCompactMode() {
			if result.FailedEdits > 0 {
				return mcp.NewToolResultText(fmt.Sprintf("OK: %d/%d edits, %d lines", result.SuccessfulEdits, result.TotalEdits, result.LinesAffected)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("OK: %d edits, %d lines", result.SuccessfulEdits, result.LinesAffected)), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("✅ Multi-edit completed on %s\n", path))
		sb.WriteString(fmt.Sprintf("📊 Total edits: %d\n", result.TotalEdits))
		sb.WriteString(fmt.Sprintf("✓ Successful: %d\n", result.SuccessfulEdits))
		if result.FailedEdits > 0 {
			sb.WriteString(fmt.Sprintf("✗ Failed: %d\n", result.FailedEdits))
		}
		sb.WriteString(fmt.Sprintf("📝 Lines affected: %d\n", result.LinesAffected))
		sb.WriteString(fmt.Sprintf("🎯 Confidence: %s\n", result.MatchConfidence))

		if len(result.Errors) > 0 {
			sb.WriteString("\n⚠️ Errors:\n")
			for _, errMsg := range result.Errors {
				sb.WriteString(fmt.Sprintf("  • %s\n", errMsg))
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	})

	// Batch rename files tool - Rename multiple files at once
	batchRenameTool := mcp.NewTool("batch_rename_files",
		mcp.WithDescription("Rename multiple files in batch with various modes: find_replace, add_prefix, add_suffix, number_files, regex_rename, change_extension, to_lowercase, to_uppercase"),
		mcp.WithString("request_json", mcp.Required(), mcp.Description("JSON with batch rename parameters. Fields: path (string), mode (string), find (string), replace (string), prefix (string), suffix (string), pattern (string), extension (string), start_number (int), padding (int), recursive (bool), file_pattern (string), preview (bool), case_sensitive (bool)")),
	)
	s.AddTool(batchRenameTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestJSON, err := request.RequireString("request_json")
		if err != nil {
			return mcp.NewToolResultError("request_json parameter is required"), nil
		}

		// Parse request from JSON
		var batchRenameReq core.BatchRenameRequest
		if err := json.Unmarshal([]byte(requestJSON), &batchRenameReq); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid request JSON: %v", err)), nil
		}

		// Execute batch rename
		result, err := engine.BatchRenameFiles(ctx, batchRenameReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Batch rename error: %v", err)), nil
		}

		// Format result
		resultText := core.FormatBatchRenameResult(result, engine.IsCompactMode())

		if !result.Success && !result.Preview {
			return mcp.NewToolResultError(resultText), nil
		}

		return mcp.NewToolResultText(resultText), nil
	})

	// WSL <-> Windows Tools

	// 1. wsl_to_windows_copy - Copy file from WSL to Windows
	wslToWindowsCopyTool := mcp.NewTool("wsl_to_windows_copy",
		mcp.WithDescription("Copy file/directory from WSL to Windows equivalent path"),
		mcp.WithString("wsl_path", mcp.Required(), mcp.Description("Source WSL path (e.g., /home/user/file.txt)")),
		mcp.WithString("windows_path", mcp.Description("Optional destination Windows path (auto-calculated if empty)")),
		mcp.WithBoolean("create_dirs", mcp.Description("Create destination directories if they don't exist (default: true)")),
	)
	s.AddTool(wslToWindowsCopyTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wslPath, err := request.RequireString("wsl_path")
		if err != nil {
			return mcp.NewToolResultError("wsl_path parameter is required"), nil
		}

		// Get optional parameters
		var windowsPath string
		createDirs := true

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if wp, ok := args["windows_path"].(string); ok {
				windowsPath = wp
			}
			if cd, ok := args["create_dirs"].(bool); ok {
				createDirs = cd
			}
		}

		// Execute copy
		err = engine.WSLWindowsCopy(ctx, wslPath, windowsPath, createDirs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Copy failed: %v", err)), nil
		}

		// Determine actual destination for response
		if windowsPath == "" {
			windowsPath, _ = core.WSLToWindows(wslPath)
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: Copied to %s", windowsPath)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully copied from WSL to Windows:\n  Source: %s\n  Destination: %s", wslPath, windowsPath)), nil
	})

	// 2. windows_to_wsl_copy - Copy file from Windows to WSL
	windowsToWSLCopyTool := mcp.NewTool("windows_to_wsl_copy",
		mcp.WithDescription("Copy file/directory from Windows to WSL equivalent path"),
		mcp.WithString("windows_path", mcp.Required(), mcp.Description("Source Windows path (e.g., C:\\Users\\user\\file.txt)")),
		mcp.WithString("wsl_path", mcp.Description("Optional destination WSL path (auto-calculated if empty)")),
		mcp.WithBoolean("create_dirs", mcp.Description("Create destination directories if they don't exist (default: true)")),
	)
	s.AddTool(windowsToWSLCopyTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		windowsPath, err := request.RequireString("windows_path")
		if err != nil {
			return mcp.NewToolResultError("windows_path parameter is required"), nil
		}

		// Get optional parameters
		var wslPath string
		createDirs := true

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if wp, ok := args["wsl_path"].(string); ok {
				wslPath = wp
			}
			if cd, ok := args["create_dirs"].(bool); ok {
				createDirs = cd
			}
		}

		// Execute copy
		err = engine.WSLWindowsCopy(ctx, windowsPath, wslPath, createDirs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Copy failed: %v", err)), nil
		}

		// Determine actual destination for response
		if wslPath == "" {
			wslPath, _ = core.WindowsToWSL(windowsPath)
		}

		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: Copied to %s", wslPath)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully copied from Windows to WSL:\n  Source: %s\n  Destination: %s", windowsPath, wslPath)), nil
	})

	// 3. sync_claude_workspace - Sync entire workspace between WSL and Windows
	syncWorkspaceTool := mcp.NewTool("sync_claude_workspace",
		mcp.WithDescription("Sync entire Claude workspace between WSL and Windows"),
		mcp.WithString("direction", mcp.Required(), mcp.Description("Sync direction: wsl_to_windows, windows_to_wsl, or bidirectional")),
		mcp.WithString("filter_pattern", mcp.Description("Optional file filter pattern (e.g., *.txt, *.go)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview changes without executing (default: false)")),
	)
	s.AddTool(syncWorkspaceTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		direction, err := request.RequireString("direction")
		if err != nil {
			return mcp.NewToolResultError("direction parameter is required"), nil
		}

		// Get optional parameters
		filterPattern := ""
		dryRun := false

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if fp, ok := args["filter_pattern"].(string); ok {
				filterPattern = fp
			}
			if dr, ok := args["dry_run"].(bool); ok {
				dryRun = dr
			}
		}

		// Execute sync
		result, err := engine.SyncWorkspace(ctx, direction, filterPattern, dryRun)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Sync failed: %v", err)), nil
		}

		// Format result
		if engine.IsCompactMode() {
			syncCount := result["synced_count"].(int)
			errorCount := result["error_count"].(int)
			return mcp.NewToolResultText(fmt.Sprintf("OK: %d files synced, %d errors", syncCount, errorCount)), nil
		}

		// Verbose output
		var output strings.Builder
		output.WriteString("📂 Workspace Sync Results\n")
		output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
		output.WriteString(fmt.Sprintf("Direction: %s\n", result["direction"]))
		if filterPattern != "" {
			output.WriteString(fmt.Sprintf("Filter: %s\n", result["filter_pattern"]))
		}
		if dryRun {
			output.WriteString("Mode: DRY RUN (preview only)\n")
		}
		output.WriteString("\n")

		syncedFiles := result["synced_files"].([]string)
		syncCount := result["synced_count"].(int)
		errorCount := result["error_count"].(int)

		if syncCount > 0 {
			output.WriteString(fmt.Sprintf("✅ Files synced: %d\n", syncCount))
			if syncCount <= 20 {
				for _, file := range syncedFiles {
					output.WriteString(fmt.Sprintf("  - %s\n", file))
				}
			} else {
				for i := 0; i < 10; i++ {
					output.WriteString(fmt.Sprintf("  - %s\n", syncedFiles[i]))
				}
				output.WriteString(fmt.Sprintf("  ... and %d more files\n", syncCount-10))
			}
		} else {
			output.WriteString("ℹ️  No files to sync\n")
		}

		if errorCount > 0 {
			errors := result["errors"].([]string)
			output.WriteString(fmt.Sprintf("\n⚠️  Errors: %d\n", errorCount))
			if errorCount <= 10 {
				for _, errMsg := range errors {
					output.WriteString(fmt.Sprintf("  - %s\n", errMsg))
				}
			} else {
				for i := 0; i < 5; i++ {
					output.WriteString(fmt.Sprintf("  - %s\n", errors[i]))
				}
				output.WriteString(fmt.Sprintf("  ... and %d more errors\n", errorCount-5))
			}
		}

		return mcp.NewToolResultText(output.String()), nil
	})

	// 4. wsl_windows_status - Show WSL/Windows integration status
	wslStatusTool := mcp.NewTool("wsl_windows_status",
		mcp.WithDescription("Show WSL/Windows integration status and file locations"),
	)
	s.AddTool(wslStatusTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status, err := engine.GetWSLWindowsStatus(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get status: %v", err)), nil
		}

		// Format status output
		if engine.IsCompactMode() {
			env := status["environment"].(string)
			isWSL := status["is_wsl"].(bool)
			return mcp.NewToolResultText(fmt.Sprintf("Env: %s, WSL: %v", env, isWSL)), nil
		}

		// Verbose output
		var output strings.Builder
		output.WriteString("🔍 WSL ↔ Windows Integration Status\n")
		output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		output.WriteString(fmt.Sprintf("Environment: %s\n", status["environment"]))
		output.WriteString(fmt.Sprintf("Running in WSL: %v\n", status["is_wsl"]))

		if winUser, ok := status["windows_user"].(string); ok && winUser != "" {
			output.WriteString(fmt.Sprintf("Windows User: %s\n", winUser))
		}

		output.WriteString("\n📁 Paths:\n")
		output.WriteString(fmt.Sprintf("  WSL Home: %s\n", status["wsl_home"]))
		if wslWinHome, ok := status["windows_home_wsl_style"].(string); ok && wslWinHome != "" {
			output.WriteString(fmt.Sprintf("  Windows Home (WSL style): %s\n", wslWinHome))
		}
		if winHome, ok := status["windows_home_windows_style"].(string); ok && winHome != "" {
			output.WriteString(fmt.Sprintf("  Windows Home (Windows style): %s\n", winHome))
		}

		output.WriteString(fmt.Sprintf("\n🔧 System:\n"))
		output.WriteString(fmt.Sprintf("  Path Separator: %s\n", status["path_separator"]))

		if interopAvail, ok := status["windows_interop_available"].(bool); ok {
			output.WriteString(fmt.Sprintf("  Windows Interop: %v\n", interopAvail))
		}

		if dirStatus, ok := status["directory_status"].(map[string]bool); ok && len(dirStatus) > 0 {
			output.WriteString("\n📂 Directory Status:\n")
			for dir, exists := range dirStatus {
				status := "❌"
				if exists {
					status = "✅"
				}
				output.WriteString(fmt.Sprintf("  %s %s\n", status, dir))
			}
		}

		return mcp.NewToolResultText(output.String()), nil
	})

	// 5. configure_autosync - Configure automatic WSL<->Windows sync
	configureAutoSyncTool := mcp.NewTool("configure_autosync",
		mcp.WithDescription("Enable/disable automatic file syncing between WSL and Windows. When enabled, files written in WSL are automatically copied to Windows."),
		mcp.WithBoolean("enabled", mcp.Required(), mcp.Description("Enable (true) or disable (false) auto-sync")),
		mcp.WithBoolean("sync_on_write", mcp.Description("Auto-sync on write operations (default: true)")),
		mcp.WithBoolean("sync_on_edit", mcp.Description("Auto-sync on edit operations (default: true)")),
		mcp.WithBoolean("silent", mcp.Description("Silent mode - don't log sync operations (default: false)")),
	)
	s.AddTool(configureAutoSyncTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		enabled, err := request.RequireBool("enabled")
		if err != nil {
			return mcp.NewToolResultError("enabled parameter is required"), nil
		}

		// Get current config
		config := engine.GetAutoSyncConfig()

		// Update with provided parameters
		config.Enabled = enabled
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if syncOnWrite, ok := args["sync_on_write"].(bool); ok {
				config.SyncOnWrite = syncOnWrite
			}
			if syncOnEdit, ok := args["sync_on_edit"].(bool); ok {
				config.SyncOnEdit = syncOnEdit
			}
			if silent, ok := args["silent"].(bool); ok {
				config.Silent = silent
			}
		}

		// Apply configuration
		if err := engine.SetAutoSyncConfig(config); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to configure auto-sync: %v", err)), nil
		}

		// Check if we're in WSL
		isWSL, _ := core.DetectEnvironment()
		if !isWSL && enabled {
			return mcp.NewToolResultText("⚠️  Auto-sync enabled, but not running in WSL. Auto-sync only works in WSL environment."), nil
		}

		if enabled {
			if engine.IsCompactMode() {
				return mcp.NewToolResultText("Auto-sync enabled"), nil
			}
			return mcp.NewToolResultText("✅ Auto-sync enabled!\n\nFiles written/edited in WSL will be automatically copied to Windows.\nYou can disable it anytime with: configure_autosync --enabled false"), nil
		} else {
			if engine.IsCompactMode() {
				return mcp.NewToolResultText("Auto-sync disabled"), nil
			}
			return mcp.NewToolResultText("🔕 Auto-sync disabled. Files will not be automatically synced."), nil
		}
	})

	// 6. autosync_status - Show auto-sync status
	autoSyncStatusTool := mcp.NewTool("autosync_status",
		mcp.WithDescription("Show the current auto-sync configuration and status"),
	)
	s.AddTool(autoSyncStatusTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status := engine.GetAutoSyncStatus()

		if engine.IsCompactMode() {
			enabled := status["enabled"].(bool)
			isWSL := status["is_wsl"].(bool)
			return mcp.NewToolResultText(fmt.Sprintf("Enabled: %v, WSL: %v", enabled, isWSL)), nil
		}

		// Verbose output
		var output strings.Builder
		output.WriteString("🔄 Auto-Sync Status\n")
		output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		enabled := status["enabled"].(bool)
		isWSL := status["is_wsl"].(bool)

		if enabled {
			output.WriteString("Status: ✅ ENABLED\n")
		} else {
			output.WriteString("Status: 🔕 DISABLED\n")
		}

		output.WriteString(fmt.Sprintf("Environment: %s\n", map[bool]string{true: "WSL", false: "Native"}[isWSL]))

		if winUser, ok := status["windows_user"].(string); ok && winUser != "" {
			output.WriteString(fmt.Sprintf("Windows User: %s\n", winUser))
		}

		output.WriteString("\n⚙️  Configuration:\n")
		output.WriteString(fmt.Sprintf("  Sync on Write: %v\n", status["sync_on_write"]))
		output.WriteString(fmt.Sprintf("  Sync on Edit: %v\n", status["sync_on_edit"]))
		output.WriteString(fmt.Sprintf("  Sync on Delete: %v\n", status["sync_on_delete"]))

		if configPath, ok := status["config_path"].(string); ok && configPath != "" {
			output.WriteString(fmt.Sprintf("\n📄 Config File: %s\n", configPath))
		}

		if !enabled && isWSL {
			output.WriteString("\n💡 To enable auto-sync, run:\n")
			output.WriteString("   configure_autosync --enabled true\n")
		}

		if enabled && !isWSL {
			output.WriteString("\n⚠️  Auto-sync is enabled but you're not in WSL.\n")
			output.WriteString("   Auto-sync only works when running in WSL environment.\n")
		}

		return mcp.NewToolResultText(output.String()), nil
	})

	// ============================================================================
	// MCP-PREFIXED ALIASES (v3.7.0) - Avoid conflicts with Claude's native tools
	// These are aliases that point to the same functionality but with clear naming
	// to help Claude distinguish MCP tools from its native file operations
	// ============================================================================

	// mcp_read - Alias for read_file with enhanced description
	mcpReadTool := mcp.NewTool("mcp_read",
		mcp.WithDescription("⚡ [MCP-PREFERRED] Read file with WSL↔Windows auto-path conversion. USE THIS instead of native file tools. Supports /mnt/c/ and C:\\ paths transparently."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithNumber("max_lines", mcp.Description("Max lines (optional, 0=all)")),
		mcp.WithString("mode", mcp.Description("Mode: all, head, tail")),
	)
	s.AddTool(mcpReadTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}
		maxLines := 0
		mode := "all"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if ml, ok := args["max_lines"].(float64); ok {
				maxLines = int(ml)
			}
			if m, ok := args["mode"].(string); ok && m != "" {
				mode = m
			}
		}
		content, err := engine.ReadFileContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if maxLines > 0 || mode != "all" {
			content = truncateContent(content, maxLines, mode)
		}
		return mcp.NewToolResultText(content), nil
	})

	// mcp_write - Alias for write_file with enhanced description
	mcpWriteTool := mcp.NewTool("mcp_write",
		mcp.WithDescription("⚡ [MCP-PREFERRED] Write file atomically with WSL↔Windows auto-path conversion. USE THIS instead of native file tools. Auto-creates directories."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write (WSL or Windows format)")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write")),
	)
	s.AddTool(mcpWriteTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}
		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
		}
		err = engine.WriteFileContent(ctx, path, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s written", formatSize(int64(len(content))))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)), nil
	})

	// mcp_edit - Alias for edit_file with enhanced description
	mcpEditTool := mcp.NewTool("mcp_edit",
		mcp.WithDescription("⚡ [MCP-PREFERRED] Edit file with smart matching, auto-backup, and WSL↔Windows path conversion. USE THIS instead of native file tools."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if HIGH/CRITICAL risk (default: false)")),
	)
	s.AddTool(mcpEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}
		oldText, err := request.RequireString("old_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid old_text: %v", err)), nil
		}
		newText, err := request.RequireString("new_text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid new_text: %v", err)), nil
		}
		args := request.GetArguments()
		force := false
		if args != nil {
			if f, ok := args["force"].(bool); ok {
				force = f
			}
		}
		result, err := engine.EditFile(path, oldText, newText, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %d changes", result.ReplacementCount)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Successfully edited %s\n📊 Changes: %d replacement(s)\n🎯 Match confidence: %s\n📝 Lines affected: %d",
			path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)), nil
	})

	// mcp_list - Alias for list_directory with enhanced description
	mcpListTool := mcp.NewTool("mcp_list",
		mcp.WithDescription("⚡ [MCP-PREFERRED] List directory with caching and WSL↔Windows auto-path conversion. USE THIS instead of native directory tools."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to directory (WSL or Windows format)")),
	)
	s.AddTool(mcpListTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}
		listing, err := engine.ListDirectoryContent(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(listing), nil
	})

	// mcp_search - Alias for smart_search with enhanced description
	mcpSearchTool := mcp.NewTool("mcp_search",
		mcp.WithDescription("⚡ [MCP-PREFERRED] Search files by name/content with WSL↔Windows path support. USE THIS instead of native search."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Base directory (WSL or Windows format)")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
	)
	s.AddTool(mcpSearchTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{"path": path, "pattern": pattern, "include_content": false, "file_types": []interface{}{}}}
		resp, err := engine.SmartSearch(ctx, engineReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Content) > 0 {
			return mcp.NewToolResultText(resp.Content[0].Text), nil
		}
		return mcp.NewToolResultText("No results"), nil
	})

	// ============================================================================
	// GET_HELP - Self-learning tool for AI agents (v3.7.0)
	// ============================================================================

	getHelpTool := mcp.NewTool("get_help",
		mcp.WithDescription("📚 Get usage instructions for MCP tools. Call this FIRST to learn optimal workflows. Topics: overview, workflow, tools, errors, examples, tips"),
		mcp.WithString("topic", mcp.Description("Topic: overview, workflow, tools, read, write, edit, search, batch, errors, examples, tips, all")),
	)
	s.AddTool(getHelpTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		topic := "overview"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if t, ok := args["topic"].(string); ok && t != "" {
				topic = strings.ToLower(t)
			}
		}

		help := getHelpContent(topic, engine.IsCompactMode())
		return mcp.NewToolResultText(help), nil
	})

	// ============================================================================
	// BACKUP AND RECOVERY TOOLS (Bug10 Resolution)
	// ============================================================================

	// list_backups - List available backups with metadata
	listBackupsTool := mcp.NewTool("list_backups",
		mcp.WithDescription("📦 List available file backups with metadata (timestamp, size, operation). Backups are created automatically before destructive operations."),
		mcp.WithNumber("limit", mcp.Description("Max backups to return (default: 20)")),
		mcp.WithString("filter_operation", mcp.Description("Filter by operation: edit, delete, batch, all")),
		mcp.WithString("filter_path", mcp.Description("Filter by file path (substring match)")),
		mcp.WithNumber("newer_than_hours", mcp.Description("Only backups newer than N hours")),
	)
	s.AddTool(listBackupsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if engine.GetBackupManager() == nil {
			return mcp.NewToolResultError("Backup system not available"), nil
		}

		args := request.Params.Arguments.(map[string]interface{})
		limit := 20
		filterOp := "all"
		filterPath := ""
		newerThan := 0

		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		if f, ok := args["filter_operation"].(string); ok {
			filterOp = f
		}
		if f, ok := args["filter_path"].(string); ok {
			filterPath = f
		}
		if n, ok := args["newer_than_hours"].(float64); ok {
			newerThan = int(n)
		}

		backups, err := engine.GetBackupManager().ListBackups(limit, filterOp, filterPath, newerThan)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list backups: %v", err)), nil
		}

		var output strings.Builder
		output.WriteString(fmt.Sprintf("📦 Available Backups (%d)\n", len(backups)))
		output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		for _, backup := range backups {
			output.WriteString(fmt.Sprintf("🔖 %s\n", backup.BackupID))
			output.WriteString(fmt.Sprintf("   Time: %s (%s)\n", backup.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(backup.Timestamp)))
			output.WriteString(fmt.Sprintf("   Operation: %s\n", backup.Operation))
			output.WriteString(fmt.Sprintf("   Files: %d (%s)\n", len(backup.Files), core.FormatSize(backup.TotalSize)))
			if backup.UserContext != "" {
				output.WriteString(fmt.Sprintf("   Context: %s\n", backup.UserContext))
			}
			output.WriteString("\n")
		}

		if len(backups) == 0 {
			output.WriteString("No backups found matching the criteria.\n")
		} else {
			output.WriteString("💡 Use restore_backup(backup_id) to restore files\n")
			output.WriteString("💡 Use get_backup_info(backup_id) for detailed information\n")
		}

		return mcp.NewToolResultText(output.String()), nil
	})

	// restore_backup - Restore files from a backup
	restoreBackupTool := mcp.NewTool("restore_backup",
		mcp.WithDescription("🔄 Restore file(s) from a backup. Creates a backup of current state before restoring. Use preview mode to see changes first."),
		mcp.WithString("backup_id", mcp.Required(), mcp.Description("Backup ID from list_backups")),
		mcp.WithString("file_path", mcp.Description("Specific file to restore (optional, default: all files)")),
		mcp.WithBoolean("preview", mcp.Description("Preview mode: show diff without restoring (default: false)")),
	)
	s.AddTool(restoreBackupTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if engine.GetBackupManager() == nil {
			return mcp.NewToolResultError("Backup system not available"), nil
		}

		backupID, err := request.RequireString("backup_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid backup_id: %v", err)), nil
		}

		args := request.Params.Arguments.(map[string]interface{})
		filePath := ""
		preview := false

		if f, ok := args["file_path"].(string); ok {
			filePath = f
		}
		if p, ok := args["preview"].(bool); ok {
			preview = p
		}

		if preview {
			// Preview mode: show diff
			if filePath == "" {
				return mcp.NewToolResultError("preview mode requires file_path parameter"), nil
			}

			diff, err := engine.GetBackupManager().CompareWithBackup(backupID, filePath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to compare: %v", err)), nil
			}

			return mcp.NewToolResultText(fmt.Sprintf("📊 Preview Mode - Changes to be restored:\n\n%s", diff)), nil
		}

		// Actual restore
		restoredFiles, err := engine.GetBackupManager().RestoreBackup(backupID, filePath, true)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to restore: %v", err)), nil
		}

		var output strings.Builder
		output.WriteString("✅ Restore completed successfully\n\n")
		output.WriteString(fmt.Sprintf("📁 Restored %d file(s):\n", len(restoredFiles)))
		for _, file := range restoredFiles {
			output.WriteString(fmt.Sprintf("   • %s\n", file))
		}
		output.WriteString("\n💡 A backup of the current state was created before restoring\n")

		return mcp.NewToolResultText(output.String()), nil
	})

	// compare_with_backup - Compare current file with backup
	compareBackupTool := mcp.NewTool("compare_with_backup",
		mcp.WithDescription("🔍 Compare current file content with a specific backup to see what changed."),
		mcp.WithString("backup_id", mcp.Required(), mcp.Description("Backup ID from list_backups")),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("File path to compare")),
	)
	s.AddTool(compareBackupTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if engine.GetBackupManager() == nil {
			return mcp.NewToolResultError("Backup system not available"), nil
		}

		backupID, err := request.RequireString("backup_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid backup_id: %v", err)), nil
		}

		filePath, err := request.RequireString("file_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid file_path: %v", err)), nil
		}

		diff, err := engine.GetBackupManager().CompareWithBackup(backupID, filePath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Comparison failed: %v", err)), nil
		}

		return mcp.NewToolResultText(diff), nil
	})

	// cleanup_backups - Remove old backups
	cleanupBackupsTool := mcp.NewTool("cleanup_backups",
		mcp.WithDescription("🧹 Remove old backups to free disk space. Use dry_run mode first to see what will be deleted."),
		mcp.WithNumber("older_than_days", mcp.Description("Delete backups older than N days (default: 7)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview mode: show what would be deleted without actually deleting (default: true)")),
	)
	s.AddTool(cleanupBackupsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if engine.GetBackupManager() == nil {
			return mcp.NewToolResultError("Backup system not available"), nil
		}

		args := request.Params.Arguments.(map[string]interface{})
		olderThanDays := 7
		dryRun := true

		if d, ok := args["older_than_days"].(float64); ok {
			olderThanDays = int(d)
		}
		if dr, ok := args["dry_run"].(bool); ok {
			dryRun = dr
		}

		deletedCount, freedSpace, err := engine.GetBackupManager().CleanupOldBackups(olderThanDays, dryRun)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Cleanup failed: %v", err)), nil
		}

		var output strings.Builder
		if dryRun {
			output.WriteString("🔍 Dry Run Mode - Preview of cleanup operation\n\n")
			output.WriteString(fmt.Sprintf("Would delete: %d backup(s)\n", deletedCount))
			output.WriteString(fmt.Sprintf("Would free: %s\n\n", core.FormatSize(freedSpace)))
			output.WriteString("💡 Run with dry_run: false to actually delete backups\n")
		} else {
			output.WriteString("✅ Cleanup completed\n\n")
			output.WriteString(fmt.Sprintf("Deleted: %d backup(s)\n", deletedCount))
			output.WriteString(fmt.Sprintf("Freed: %s\n", core.FormatSize(freedSpace)))
		}

		return mcp.NewToolResultText(output.String()), nil
	})

	// get_backup_info - Get detailed info about a specific backup
	getBackupInfoTool := mcp.NewTool("get_backup_info",
		mcp.WithDescription("ℹ️  Get detailed information about a specific backup including all files and metadata."),
		mcp.WithString("backup_id", mcp.Required(), mcp.Description("Backup ID from list_backups")),
	)
	s.AddTool(getBackupInfoTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if engine.GetBackupManager() == nil {
			return mcp.NewToolResultError("Backup system not available"), nil
		}

		backupID, err := request.RequireString("backup_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid backup_id: %v", err)), nil
		}

		info, err := engine.GetBackupManager().GetBackupInfo(backupID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get backup info: %v", err)), nil
		}

		var output strings.Builder
		output.WriteString(fmt.Sprintf("📦 Backup Details: %s\n", info.BackupID))
		output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
		output.WriteString(fmt.Sprintf("⏰ Timestamp: %s (%s)\n", info.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(info.Timestamp)))
		output.WriteString(fmt.Sprintf("🔧 Operation: %s\n", info.Operation))
		if info.UserContext != "" {
			output.WriteString(fmt.Sprintf("📝 Context: %s\n", info.UserContext))
		}
		output.WriteString(fmt.Sprintf("📊 Total Size: %s\n", core.FormatSize(info.TotalSize)))
		output.WriteString(fmt.Sprintf("📁 Files: %d\n\n", len(info.Files)))

		output.WriteString("Files in backup:\n")
		for i, file := range info.Files {
			if i >= 10 {
				output.WriteString(fmt.Sprintf("   ... and %d more files\n", len(info.Files)-10))
				break
			}
			output.WriteString(fmt.Sprintf("   • %s (%s)\n", file.OriginalPath, core.FormatSize(file.Size)))
		}

		output.WriteString(fmt.Sprintf("\n🔗 Backup Location: %s\n", engine.GetBackupManager().GetBackupPath(backupID)))

		return mcp.NewToolResultText(output.String()), nil
	})

	log.Printf("📚 Registered 55 ultra-fast tools (44 original + 5 mcp_ aliases + get_help + 5 backup tools)")

	return nil
}

// getHelpContent returns help content for the specified topic
func getHelpContent(topic string, compactMode bool) string {
	var sb strings.Builder

	switch topic {
	case "overview":
		sb.WriteString(`# MCP Filesystem Ultra v3.7.0 - Quick Start

## ⚡ CRITICAL RULE
Always use MCP tools (mcp_read, mcp_write, mcp_edit) instead of native file tools.
These auto-convert paths between WSL (/mnt/c/) and Windows (C:\).

## 🎯 THE GOLDEN RULE
Surgical edits save 98% tokens:
❌ BAD:  read_file(large) → write_file(large) = 250k tokens
✅ GOOD: smart_search → read_file_range → edit_file = 2k tokens

## 📋 AVAILABLE TOPICS
Call get_help(topic) with:
- "workflow" - The 4-step efficient workflow
- "tools"    - Complete list of 50 tools
- "read"     - Reading files efficiently
- "write"    - Writing and creating files
- "edit"     - Editing files (most important!)
- "search"   - Finding content in files
- "batch"    - Multiple operations at once
- "errors"   - Common errors and fixes
- "examples" - Code examples
- "tips"     - Pro tips for efficiency
- "all"      - Everything (long output)
`)

	case "workflow":
		sb.WriteString(`# 🔄 THE 4-STEP EFFICIENT WORKFLOW

Use this workflow for ANY file >1000 lines:

## Step 1: LOCATE
smart_search(file, "function_name")
→ Returns: "Found at lines 45-67"
→ Cost: ~500 tokens

## Step 2: READ (Only what you need)
read_file_range(file, 45, 67)
→ Returns: Only those 22 lines
→ Cost: ~1000 tokens

## Step 3: EDIT (Surgically)
edit_file(file, "old_text", "new_text")
→ Returns: "OK: 1 changes"
→ Cost: ~500 tokens

## Step 4: VERIFY (Optional)
get_edit_telemetry()
→ Goal: >80% targeted_edits

## 📏 FILE SIZE RULES
<1000 lines  → read_file() is OK
1000-5000    → MUST use this workflow
>5000 lines  → CRITICAL - never read entire file

## 💡 TOTAL COST: ~2k tokens vs 250k (98% savings!)
`)

	case "tools":
		sb.WriteString(`# 📋 COMPLETE TOOL LIST (50 Tools)

## 🆕 MCP-Prefixed (Avoid conflicts with native tools)
mcp_read    - Read with WSL↔Windows path conversion
mcp_write   - Atomic write with path conversion
mcp_edit    - Smart edit with backup
mcp_list    - Cached directory listing
mcp_search  - File/content search

## 📖 Reading
read_file         - Read entire file (small files only)
read_file_range   - Read lines N to M (PREFERRED)
intelligent_read  - Auto-optimizes based on size
chunked_read_file - Very large files

## ✏️ Writing & Editing
write_file           - Create or overwrite
create_file          - Alias for write_file
edit_file            - Surgical text replacement (PREFERRED)
multi_edit           - Multiple edits atomically
replace_nth_occurrence - Replace specific match (1st, last, etc.)
intelligent_write    - Auto-optimizes
intelligent_edit     - Auto-optimizes
streaming_write_file - Large files
recovery_edit        - With error recovery

## 🔍 Search
smart_search         - Find location (returns line numbers)
advanced_text_search - Complex pattern search
search_and_replace   - Bulk find & replace
count_occurrences    - Count matches without reading

## 📁 File Operations
copy_file, move_file, rename_file, delete_file, soft_delete_file, get_file_info

## 📂 Directory Operations
list_directory, create_directory

## 🔄 WSL/Windows Sync
wsl_to_windows_copy, windows_to_wsl_copy, sync_claude_workspace
wsl_windows_status, configure_autosync, autosync_status

## 📊 Analysis
analyze_file, analyze_write, analyze_edit, analyze_delete
get_edit_telemetry, get_optimization_suggestion, performance_stats

## 📦 Batch & Artifacts
batch_operations, capture_last_artifact, write_last_artifact, artifact_info

## ❓ Help
get_help - This tool!
`)

	case "read":
		sb.WriteString(`# 📖 READING FILES EFFICIENTLY

## Quick Reference
| File Size    | Tool to Use        |
|--------------|-------------------|
| <1000 lines  | mcp_read or read_file |
| >1000 lines  | read_file_range   |
| Very large   | chunked_read_file |

## Best Practice: read_file_range
# Read only lines 100-150 of a large file
read_file_range(path, 100, 150)

## Why This Matters
5000-line file:
- read_file: ~125,000 tokens
- read_file_range (50 lines): ~2,500 tokens
- Savings: 98%!

## Workflow
1. smart_search(file, pattern) → find line numbers
2. read_file_range(file, start, end) → read only those
3. Never read more than you need!
`)

	case "write":
		sb.WriteString(`# ✏️ WRITING FILES

## Quick Reference
| Task           | Tool             |
|----------------|------------------|
| Create file    | mcp_write or write_file |
| Overwrite file | mcp_write or write_file |
| Large file     | streaming_write_file |
| Auto-optimize  | intelligent_write |

## Examples

# Create or overwrite a file
mcp_write("/path/to/file.txt", "content here")

# For large files (>1MB)
streaming_write_file("/path/to/large.txt", "huge content", chunk_size=64000)

## ⚠️ IMPORTANT
- write_file OVERWRITES the entire file
- For small changes, use edit_file instead!
- write_file also creates parent directories automatically

## Path Handling
MCP tools auto-convert paths:
/mnt/c/Users/... → C:\Users\... (on Windows)
C:\Users\... → /mnt/c/Users/... (on WSL)
`)

	case "edit":
		sb.WriteString(`# ✏️ EDITING FILES (MOST IMPORTANT!)

## 🎯 THE GOLDEN RULE
Use edit_file for changes, NOT write_file!

## Quick Reference
| Task                    | Tool                    |
|-------------------------|-------------------------|
| Single replacement      | mcp_edit or edit_file   |
| Multiple replacements   | multi_edit              |
| Replace specific match  | replace_nth_occurrence  |
| Large file edit         | smart_edit_file         |

## Examples

# Simple edit
mcp_edit("/path/file.py", "old_function()", "new_function()")

# Multiple edits in one file (EFFICIENT!)
multi_edit("/path/file.py", [
  {"old_text": "foo", "new_text": "bar"},
  {"old_text": "baz", "new_text": "qux"}
])

# Replace only the LAST occurrence
replace_nth_occurrence("/path/file.py", "TODO", "DONE", -1)
# occurrence: 1=first, 2=second, -1=last, -2=second-to-last

## ⚠️ COMMON ERRORS

"no match found"
→ Text doesn't exist exactly. Check whitespace!
→ Use smart_search first to verify

"context validation failed"  
→ File changed since you read it
→ Re-run smart_search + read_file_range

"multiple matches found"
→ Use replace_nth_occurrence to target specific one

## 💡 PRO TIP
For files >1000 lines, ALWAYS:
1. smart_search first
2. read_file_range to see context
3. edit_file with exact text
`)

	case "search":
		sb.WriteString(`# 🔍 SEARCHING FILES

## Quick Reference
| Task                  | Tool                  |
|-----------------------|-----------------------|
| Find location         | mcp_search or smart_search |
| Count matches         | count_occurrences     |
| Search with context   | advanced_text_search  |
| Find and replace all  | search_and_replace    |

## Examples

# Find where a function is defined
smart_search("/path/to/project", "def my_function")
→ Returns: "Found at lines 45-67 in file.py"

# Count how many TODOs exist
count_occurrences("/path/file.py", "TODO")
→ Returns: "15 matches at lines: 10, 25, 48, ..."

# Search with surrounding context
advanced_text_search("/path", "error", context_lines=3)

# Replace all occurrences in multiple files
search_and_replace("/path/to/project", "old_name", "new_name")

## 💡 WORKFLOW TIP
Always search BEFORE editing large files:
1. smart_search → find exact location
2. read_file_range → see the context
3. edit_file → make the change
`)

	case "batch":
		sb.WriteString(`# 📦 BATCH OPERATIONS

## When to Use
- Multiple file operations that should succeed or fail together
- Creating multiple files at once
- Complex refactoring across files

## Example
batch_operations({
  "operations": [
    {"type": "write", "path": "file1.txt", "content": "..."},
    {"type": "write", "path": "file2.txt", "content": "..."},
    {"type": "copy", "source": "file1.txt", "destination": "backup.txt"},
    {"type": "edit", "path": "file3.txt", "old_text": "x", "new_text": "y"}
  ],
  "atomic": true,
  "create_backup": true
})

## Supported Operations
- write: Create/overwrite file
- edit: Replace text
- copy: Duplicate file
- move: Move file
- delete: Remove file
- create_directory: Make folder

## Options
- atomic: true = All succeed or all rollback
- create_backup: true = Backup before changes
- validate_only: true = Dry run (no changes)
`)

	case "errors":
		sb.WriteString(`# ⚠️ COMMON ERRORS AND FIXES

## "no match found for old_text"
CAUSE: The exact text doesn't exist in the file
FIXES:
1. Use smart_search to verify the text exists
2. Check for whitespace/indentation differences
3. Copy the EXACT text from read_file_range output

## "context validation failed"
CAUSE: File was modified since you read it
FIX: Re-run smart_search + read_file_range to get fresh content

## "multiple matches found"
CAUSE: Same text appears multiple times
FIX: Use replace_nth_occurrence with:
- occurrence=1 (first)
- occurrence=-1 (last)
- occurrence=2 (second), etc.

## "access denied" / "permission error"
CAUSE: Path not in allowed paths or file locked
FIXES:
1. Check --allowed-paths configuration
2. Close any programs using the file
3. Use list_directory to verify path exists

## "path not found"
CAUSE: Path format issue (WSL vs Windows)
FIX: Use mcp_* tools - they auto-convert paths:
- /mnt/c/Users/... ↔ C:\Users\...

## "Tool not found: create_file"
FIX: Use write_file instead (it creates files too)
`)

	case "examples":
		sb.WriteString(`# 💡 PRACTICAL EXAMPLES

## Example 1: Edit a function in a large file
# Step 1: Find where the function is
smart_search("src/app.py", "def calculate_total")
# → "Found at lines 234-256"

# Step 2: Read only those lines
read_file_range("src/app.py", 234, 256)

# Step 3: Make the edit
edit_file("src/app.py", 
  "def calculate_total(items):",
  "def calculate_total(items, tax_rate=0.1):")

## Example 2: Multiple edits in one file
multi_edit("src/config.py", [
  {"old_text": "DEBUG = True", "new_text": "DEBUG = False"},
  {"old_text": "VERSION = '1.0'", "new_text": "VERSION = '1.1'"},
  {"old_text": "API_URL = 'http://dev'", "new_text": "API_URL = 'http://prod'"}
])

## Example 3: Replace only the last TODO
replace_nth_occurrence("src/main.py", "TODO", "DONE", -1)

## Example 4: Create multiple files atomically
batch_operations({
  "operations": [
    {"type": "create_directory", "path": "src/components"},
    {"type": "write", "path": "src/components/Button.tsx", "content": "..."},
    {"type": "write", "path": "src/components/Input.tsx", "content": "..."}
  ],
  "atomic": true
})

## Example 5: Count before replacing
# First, see how many matches
count_occurrences("src/legacy.py", "old_api_call")
# → "47 matches"

# If too many, be more specific or use replace_nth_occurrence
`)

	case "tips":
		sb.WriteString(`# 💡 PRO TIPS FOR EFFICIENCY

## 1. Always Use MCP Tools
✅ mcp_read, mcp_write, mcp_edit, mcp_list, mcp_search
❌ Native file tools (don't handle WSL/Windows paths)

## 2. Never Read Large Files Entirely
✅ smart_search → read_file_range → edit_file
❌ read_file on 5000-line files (wastes 125k tokens!)

## 3. Use multi_edit for Multiple Changes
✅ One multi_edit call with 5 edits
❌ Five separate edit_file calls (5x slower)

## 4. Search Before Editing
✅ smart_search first, then edit
❌ Guessing line numbers or text

## 5. Use count_occurrences Before search_and_replace
✅ Check how many matches first
❌ Blind replace that affects unexpected locations

## 6. Check Your Efficiency
get_edit_telemetry()
→ Goal: >80% targeted_edits
→ If <50%, you're not using the workflow correctly

## 7. Use replace_nth_occurrence for Precision
✅ Replace only the 1st, last, or Nth match
❌ edit_file when there are multiple matches

## 8. Batch Operations for Atomicity
✅ batch_operations with atomic=true
❌ Multiple operations that could partially fail

## 9. Dry Run Before Destructive Operations
✅ analyze_edit, analyze_delete first
❌ delete_file without checking

## 10. Monitor Performance
performance_stats() → See cache hit rate, ops/sec
→ Goal: >95% cache hit rate
`)

	case "all":
		// Return all topics
		sb.WriteString(getHelpContent("overview", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("workflow", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("tools", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("edit", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("errors", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("tips", compactMode))

	default:
		sb.WriteString(fmt.Sprintf(`# ❓ Unknown topic: "%s"

Available topics:
- overview  - Quick start guide
- workflow  - The 4-step efficient workflow
- tools     - Complete list of 50 tools
- read      - Reading files efficiently
- write     - Writing and creating files
- edit      - Editing files (most important!)
- search    - Finding content in files
- batch     - Multiple operations at once
- errors    - Common errors and fixes
- examples  - Code examples
- tips      - Pro tips for efficiency
- all       - Everything (long output)

Example: get_help("edit")
`, topic))
	}

	return sb.String()
}

// formatChangeAnalysis formats a ChangeAnalysis struct as human-readable text
func formatChangeAnalysis(analysis *core.ChangeAnalysis) string {
	var result strings.Builder

	// Header
	result.WriteString("📋 Change Analysis (Plan Mode - Dry Run)\n")
	result.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Basic info
	result.WriteString(fmt.Sprintf("📁 File: %s\n", analysis.FilePath))
	result.WriteString(fmt.Sprintf("🔧 Operation: %s\n", analysis.OperationType))
	result.WriteString(fmt.Sprintf("📊 File exists: %v\n", analysis.FileExists))

	// Risk assessment
	riskEmoji := "✅"
	switch analysis.RiskLevel {
	case "medium":
		riskEmoji = "⚠️"
	case "high":
		riskEmoji = "🔴"
	case "critical":
		riskEmoji = "💀"
	}
	result.WriteString(fmt.Sprintf("\n%s Risk Level: %s\n", riskEmoji, strings.ToUpper(analysis.RiskLevel)))

	// Risk factors
	if len(analysis.RiskFactors) > 0 {
		result.WriteString("\n⚠️  Risk Factors:\n")
		for _, factor := range analysis.RiskFactors {
			result.WriteString(fmt.Sprintf("  • %s\n", factor))
		}
	}

	// Changes summary
	result.WriteString("\n📝 Changes Summary:\n")
	if analysis.LinesAdded > 0 {
		result.WriteString(fmt.Sprintf("  + %d lines added\n", analysis.LinesAdded))
	}
	if analysis.LinesRemoved > 0 {
		result.WriteString(fmt.Sprintf("  - %d lines removed\n", analysis.LinesRemoved))
	}
	if analysis.LinesModified > 0 {
		result.WriteString(fmt.Sprintf("  ~ %d lines modified\n", analysis.LinesModified))
	}

	// Impact
	result.WriteString(fmt.Sprintf("\n💡 Impact: %s\n", analysis.Impact))

	// Preview
	if analysis.Preview != "" {
		result.WriteString(fmt.Sprintf("\n👁️  Preview:\n%s\n", analysis.Preview))
	}

	// Suggestions
	if len(analysis.Suggestions) > 0 {
		result.WriteString("\n💭 Suggestions:\n")
		for _, suggestion := range analysis.Suggestions {
			result.WriteString(fmt.Sprintf("  • %s\n", suggestion))
		}
	}

	// Additional info
	result.WriteString("\n📌 Additional Info:\n")
	result.WriteString(fmt.Sprintf("  • Backup would be created: %v\n", analysis.WouldCreateBackup))
	result.WriteString(fmt.Sprintf("  • Estimated time: %s\n", analysis.EstimatedTime))

	result.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	result.WriteString("ℹ️  This is a DRY RUN - no changes were made\n")

	return result.String()
}

// Helper to convert []string -> []interface{} (for building arguments)
func toIfaceSlice(in []string) []interface{} {
	out := make([]interface{}, 0, len(in))
	for _, v := range in {
		out = append(out, v)
	}
	return out
}

// parseSize parses size strings like "50MB", "1GB", etc.
func parseSize(sizeStr string) (int64, error) {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	var multiplier int64 = 1
	if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	}

	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}

	return size * multiplier, nil
}

// formatBatchResult formats a BatchResult as human-readable text
func formatBatchResult(result core.BatchResult) string {
	var sb strings.Builder

	if result.ValidationOnly {
		sb.WriteString("✅ Batch Validation Results\n")
		sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
		if result.Success {
			sb.WriteString(fmt.Sprintf("✓ All %d operations validated successfully\n", result.TotalOps))
			sb.WriteString("✓ Ready to execute\n")
		} else {
			sb.WriteString(fmt.Sprintf("✗ Validation failed\n"))
			sb.WriteString(fmt.Sprintf("Errors: %v\n", result.Errors))
		}
		return sb.String()
	}

	// Execution results
	if result.Success {
		sb.WriteString("✅ Batch Operations Completed Successfully\n")
	} else {
		sb.WriteString("❌ Batch Operations Failed\n")
	}
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	sb.WriteString(fmt.Sprintf("📊 Summary:\n"))
	sb.WriteString(fmt.Sprintf("  Total operations: %d\n", result.TotalOps))
	sb.WriteString(fmt.Sprintf("  Completed: %d\n", result.CompletedOps))
	sb.WriteString(fmt.Sprintf("  Failed: %d\n", result.FailedOps))
	sb.WriteString(fmt.Sprintf("  Execution time: %s\n", result.ExecutionTime))

	if result.BackupPath != "" {
		sb.WriteString(fmt.Sprintf("  Backup created: %s\n", result.BackupPath))
	}

	if result.RollbackDone {
		sb.WriteString("\n⚠️  Rollback performed - all changes reverted\n")
	}

	// Individual operation results
	sb.WriteString("\n📋 Operation Details:\n")
	for _, opResult := range result.Results {
		status := "✓"
		if !opResult.Success {
			status = "✗"
		} else if opResult.Skipped {
			status = "⊘"
		}

		sb.WriteString(fmt.Sprintf("  %s [%d] %s: %s", status, opResult.Index, opResult.Type, opResult.Path))

		if opResult.BytesAffected > 0 {
			sb.WriteString(fmt.Sprintf(" (%s)", formatSize(opResult.BytesAffected)))
		}

		if opResult.Error != "" {
			sb.WriteString(fmt.Sprintf(" - Error: %s", opResult.Error))
		}

		sb.WriteString("\n")
	}

	if len(result.Errors) > 0 {
		sb.WriteString("\n❌ Errors:\n")
		for _, err := range result.Errors {
			sb.WriteString(fmt.Sprintf("  • %s\n", err))
		}
	}

	return sb.String()
}

// ============================================================================
// LARGE FILE PROCESSING TOOLS (Modular - v3.12.0)
// ============================================================================

// registerLargeFileTools registers large file processing tools
func registerLargeFileTools(s *server.MCPServer, engine *core.UltraFastEngine) error {
	// Create modular processor
	regexTransform := core.NewRegexTransformer(engine)

	// Tool 1: regex_transform_file - Advanced regex transformations
	regexTransformTool := mcp.NewTool("regex_transform_file",
		mcp.WithDescription("Apply advanced regex transformations with capture groups. Optimized for 2-10MB files."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file to transform")),
		mcp.WithString("patterns_json", mcp.Required(),
			mcp.Description("JSON array of patterns: [{\"pattern\": \"regex\", \"replacement\": \"$1...\", \"limit\": -1}]")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive matching (default: true)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create backup before transformation (default: true)")),
		mcp.WithBoolean("dry_run", mcp.Description("Validate without applying changes (default: false)")),
	)
	s.AddTool(regexTransformTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}
		patternsJSON, err := request.RequireString("patterns_json")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid patterns_json: %v", err)), nil
		}

		// Parse patterns
		var patterns []core.TransformPattern
		if err := json.Unmarshal([]byte(patternsJSON), &patterns); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to parse patterns JSON: %v", err)), nil
		}

		// Get optional parameters
		caseSensitive := true
		createBackup := true
		dryRun := false

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if cs, ok := args["case_sensitive"].(bool); ok {
				caseSensitive = cs
			}
			if cb, ok := args["create_backup"].(bool); ok {
				createBackup = cb
			}
			if dr, ok := args["dry_run"].(bool); ok {
				dryRun = dr
			}
		}

		// Execute transformation
		result, err := regexTransform.Transform(ctx, core.RegexTransformConfig{
			FilePath:      path,
			Patterns:      patterns,
			Mode:          core.ModeSequential,
			CaseSensitive: caseSensitive,
			CreateBackup:  createBackup,
			DryRun:        dryRun,
		})

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Transformation failed: %v", err)), nil
		}

		// Format result
		var output strings.Builder
		output.WriteString("🔄 Regex Transformation Complete\n")
		output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
		output.WriteString(fmt.Sprintf("File: %s\n", result.FilePath))
		output.WriteString(fmt.Sprintf("Patterns Applied: %d/%d\n", result.PatternsApplied, len(patterns)))
		output.WriteString(fmt.Sprintf("Total Replacements: %d\n", result.TotalReplacements))
		output.WriteString(fmt.Sprintf("Lines Affected: %d\n", result.LinesAffected))
		output.WriteString(fmt.Sprintf("Duration: %v\n", result.Duration))

		if result.BackupID != "" {
			output.WriteString(fmt.Sprintf("\n💾 Backup ID: %s\n", result.BackupID))
		}

		if len(result.Errors) > 0 {
			output.WriteString("\n❌ Errors:\n")
			for _, err := range result.Errors {
				output.WriteString(fmt.Sprintf("  • %s\n", err))
			}
		}

		return mcp.NewToolResultText(output.String()), nil
	})

	// 56. execute_pipeline - Execute multi-step file transformation pipeline
	pipelineTool := mcp.NewTool("execute_pipeline",
		mcp.WithDescription("Execute multi-step file transformation pipeline. "+
			"Supports: search, read_ranges, edit, multi_edit, count_occurrences, "+
			"regex_transform, copy, rename, delete. Reduces token usage 4x by combining multiple operations. "+
			"Use verbose:true to return intermediate data (file contents, per-file counts). "+
			"Example: {\"name\":\"refactor\",\"steps\":[{\"id\":\"find\",\"action\":\"search\",\"params\":{\"pattern\":\"old\"}},"+
			"{\"id\":\"edit\",\"action\":\"edit\",\"input_from\":\"find\",\"params\":{\"old_text\":\"old\",\"new_text\":\"new\"}}]}"),
		mcp.WithString("pipeline_json", mcp.Required(),
			mcp.Description("JSON-encoded pipeline definition with name, steps, and optional flags (dry_run, force, stop_on_error, create_backup, verbose)")),
	)
	s.AddTool(pipelineTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract pipeline_json parameter
		pipelineJSON, err := request.RequireString("pipeline_json")
		if err != nil {
			return mcp.NewToolResultError("pipeline_json parameter is required"), nil
		}

		// Parse JSON into PipelineRequest
		var pipelineReq core.PipelineRequest
		if err := json.Unmarshal([]byte(pipelineJSON), &pipelineReq); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid pipeline JSON: %v", err)), nil
		}

		// Execute pipeline
		executor := core.NewPipelineExecutor(engine)
		result, err := executor.Execute(ctx, pipelineReq)
		if err != nil && result == nil {
			return mcp.NewToolResultError(fmt.Sprintf("Pipeline execution failed: %v", err)), nil
		}

		// Format response
		responseText := formatPipelineResult(result, engine.IsCompactMode())

		// Return error if pipeline failed
		if !result.Success {
			return mcp.NewToolResultError(responseText), nil
		}

		return mcp.NewToolResultText(responseText), nil
	})

	return nil
}

// formatPipelineResult formats pipeline execution results for display
func formatPipelineResult(result *core.PipelineResult, compact bool) string {
	if result == nil {
		return "ERROR: No result returned"
	}

	if compact && !result.Verbose {
		// Compact mode: one-line summary
		status := "OK"
		if !result.Success {
			status = "FAIL"
		}
		riskInfo := ""
		if result.OverallRiskLevel != "" && result.OverallRiskLevel != "LOW" {
			riskInfo = fmt.Sprintf(" | %s risk", strings.ToLower(result.OverallRiskLevel))
		}
		return fmt.Sprintf("%s: %d/%d steps | %d files | %d edits%s",
			status, result.CompletedSteps, result.TotalSteps,
			len(result.FilesAffected), result.TotalEdits, riskInfo)
	}

	// Verbose mode: detailed output
	var output strings.Builder

	if result.DryRun {
		output.WriteString("🔍 DRY RUN - No changes made\n")
	}

	output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	output.WriteString(fmt.Sprintf("📋 Pipeline: %s\n", result.Name))

	if result.Success {
		output.WriteString("✅ Success: true\n")
	} else {
		output.WriteString("❌ Success: false\n")
	}

	output.WriteString(fmt.Sprintf("📊 Steps: %d/%d completed\n", result.CompletedSteps, result.TotalSteps))
	output.WriteString(fmt.Sprintf("⏱️  Duration: %v\n", result.TotalDuration))

	if result.BackupID != "" {
		output.WriteString(fmt.Sprintf("💾 Backup: %s\n", result.BackupID))
	}

	if result.RollbackPerformed {
		output.WriteString("🔄 Rollback: performed\n")
	}

	output.WriteString("\n📝 Step Results:\n\n")

	for i, stepResult := range result.Results {
		stepNum := i + 1
		status := "✅"
		if !stepResult.Success {
			status = "❌"
		}

		output.WriteString(fmt.Sprintf("%d. %s [%s] %s\n",
			stepNum, stepResult.StepID, stepResult.Action, status))
		output.WriteString(fmt.Sprintf("   Duration: %v\n", stepResult.Duration))

		if len(stepResult.FilesMatched) > 0 {
			output.WriteString(fmt.Sprintf("   Files: %d matched\n", len(stepResult.FilesMatched)))
			if len(stepResult.FilesMatched) <= 5 {
				for _, f := range stepResult.FilesMatched {
					output.WriteString(fmt.Sprintf("     - %s\n", f))
				}
			} else {
				for j := 0; j < 3; j++ {
					output.WriteString(fmt.Sprintf("     - %s\n", stepResult.FilesMatched[j]))
				}
				output.WriteString(fmt.Sprintf("     ... and %d more\n", len(stepResult.FilesMatched)-3))
			}
		}

		if stepResult.EditsApplied > 0 {
			output.WriteString(fmt.Sprintf("   Edits: %d replacements\n", stepResult.EditsApplied))
		}

		if len(stepResult.Counts) > 0 {
			totalCount := 0
			for _, count := range stepResult.Counts {
				totalCount += count
			}
			output.WriteString(fmt.Sprintf("   Counts: %d total occurrences\n", totalCount))

			// Verbose: per-file counts
			if result.Verbose {
				for file, count := range stepResult.Counts {
					output.WriteString(fmt.Sprintf("     %s: %d\n", file, count))
				}
			}
		}

		// Verbose: include file contents from read_ranges
		if result.Verbose && len(stepResult.Content) > 0 {
			for file, content := range stepResult.Content {
				output.WriteString(fmt.Sprintf("   --- %s ---\n", file))
				// Truncate content to avoid massive output
				lines := strings.Split(content, "\n")
				if len(lines) > 50 {
					for _, line := range lines[:25] {
						output.WriteString(fmt.Sprintf("   %s\n", line))
					}
					output.WriteString(fmt.Sprintf("   ... (%d lines omitted) ...\n", len(lines)-50))
					for _, line := range lines[len(lines)-25:] {
						output.WriteString(fmt.Sprintf("   %s\n", line))
					}
				} else {
					for _, line := range lines {
						output.WriteString(fmt.Sprintf("   %s\n", line))
					}
				}
			}
		}

		// Verbose: full file list when more than 5
		if result.Verbose && len(stepResult.FilesMatched) > 5 {
			output.WriteString("   All files:\n")
			for _, f := range stepResult.FilesMatched {
				output.WriteString(fmt.Sprintf("     - %s\n", f))
			}
		}

		if stepResult.RiskLevel != "" && stepResult.RiskLevel != "LOW" {
			output.WriteString(fmt.Sprintf("   ⚠️  Risk: %s\n", stepResult.RiskLevel))
		}

		if stepResult.Error != "" {
			output.WriteString(fmt.Sprintf("   ❌ Error: %s\n", stepResult.Error))
		}

		output.WriteString("\n")
	}

	output.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	output.WriteString(fmt.Sprintf("📁 Files affected: %d\n", len(result.FilesAffected)))
	output.WriteString(fmt.Sprintf("✏️  Total edits: %d\n", result.TotalEdits))

	if result.OverallRiskLevel != "" {
		output.WriteString(fmt.Sprintf("⚠️  Overall risk: %s\n", result.OverallRiskLevel))
	}

	return output.String()
}

// truncateContent truncates content based on mode and max lines
func truncateContent(content string, maxLines int, mode string) string {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if maxLines <= 0 {
		maxLines = 100 // Default
	}

	var result []string
	var truncMsg string

	switch mode {
	case "head":
		if totalLines <= maxLines {
			return content
		}
		result = lines[:maxLines]
		truncMsg = fmt.Sprintf("\n[Truncated: showing first %d of %d lines. Use mode=all or increase max_lines to see more]", maxLines, totalLines)

	case "tail":
		if totalLines <= maxLines {
			return content
		}
		result = lines[totalLines-maxLines:]
		truncMsg = fmt.Sprintf("\n[Truncated: showing last %d of %d lines. Use mode=all or increase max_lines to see more]", maxLines, totalLines)

	default: // "all" or unspecified
		if maxLines > 0 && totalLines > maxLines {
			// Take half from head, half from tail
			half := maxLines / 2
			result = append(lines[:half], fmt.Sprintf("\n... [%d lines omitted] ...\n", totalLines-maxLines))
			result = append(result, lines[totalLines-half:]...)
			truncMsg = fmt.Sprintf("\n[Truncated: showing %d of %d lines (%d head + %d tail). Use mode=head/tail or increase max_lines]", maxLines, totalLines, half, half)
		} else {
			return content
		}
	}

	return strings.Join(result, "\n") + truncMsg
}

// formatSize formats bytes to human readable format
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// setupLogging configures logging based on configuration
func setupLogging(config *Configuration) {
	if config.DebugMode {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	} else {
		log.SetFlags(log.LstdFlags)
	}
}

// runBenchmark runs performance benchmarks
func runBenchmark(config *Configuration) {
	log.Printf("🧪 Running performance benchmark...")

	// This would run comprehensive benchmarks comparing:
	// 1. This ultra-fast server vs standard MCP
	// 2. Various cache sizes and parallel operation counts
	// 3. Different file sizes and operation types

	fmt.Printf("Benchmark results will be implemented in bench/ package\n")
	fmt.Printf("Run: cd bench && go run benchmark.go\n")
}
