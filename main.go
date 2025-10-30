package main

import (
	"context"
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
	)
	flag.Parse()

	if *version {
		fmt.Printf("MCP Filesystem Server Ultra-Fast v1.0.0\n")
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
	})
	if err != nil {
		log.Fatalf("Failed to initialize engine: %v", err)
	}
	defer engine.Close()

	// Create MCP server using mark3labs SDK
	s := server.NewMCPServer(
		"filesystem-ultra",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register tools
	if err := registerTools(s, engine); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
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
		mcp.WithDescription("Edit file (smart, backup)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
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

		result, err := engine.EditFile(path, oldText, newText)
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
		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{"path": path, "pattern": pattern, "include_content": false, "file_types": []interface{}{}}}
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
		mcp.WithDescription("Advanced text search"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory or file")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
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
		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{"path": path, "pattern": pattern, "case_sensitive": false, "whole_word": false, "include_context": false, "context_lines": 3}}
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
		mcp.WithDescription("Automatically optimized edit for Claude Desktop (chooses direct or smart edit)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
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

		result, err := engine.IntelligentEdit(ctx, path, oldText, newText)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Intelligent edit completed on %s\n📊 Changes: %d replacement(s)\n🎯 Match confidence: %s\n📝 Lines affected: %d",
			path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)), nil
	})

	// Advanced recovery edit
	recoveryEditTool := mcp.NewTool("recovery_edit",
		mcp.WithDescription("Edit with automatic error recovery (fuzzy matching, whitespace normalization)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Required(), mcp.Description("New text to replace with")),
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

		result, err := engine.AutoRecoveryEdit(ctx, path, oldText, newText)
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
		mcp.WithDescription("Copy a file or directory to a new location"),
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

	log.Printf("📚 Registered 35 ultra-fast tools (32 previous + 3 new: read_file_range, count_occurrences, replace_nth_occurrence)")

	return nil
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
