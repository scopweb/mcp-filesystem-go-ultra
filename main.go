package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	"github.com/mcp/filesystem-ultra/mcp"
	"github.com/mcp/filesystem-ultra/protocol"
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
		AllowedPaths:     []string{}, // No restrictions by default
	}
}

func main() {
	config := DefaultConfiguration()

	// Parse command line arguments
	var (
		cacheSize       = flag.String("cache-size", "100MB", "Memory cache limit (e.g., 50MB, 1GB)")
		parallelOps     = flag.Int("parallel-ops", config.ParallelOps, "Max concurrent operations")
		binaryThreshold = flag.String("binary-threshold", "1MB", "File size threshold for binary protocol")
		vsCodeAPI       = flag.Bool("vscode-api", true, "Enable VSCode API integration when available")
		debugMode       = flag.Bool("debug", false, "Enable debug mode")
		logLevel        = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		allowedPaths    = flag.String("allowed-paths", "", "Comma-separated list of allowed base paths for access control")
		version         = flag.Bool("version", false, "Show version information")
		benchmark       = flag.Bool("bench", false, "Run performance benchmark")
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
	if *allowedPaths != "" {
		config.AllowedPaths = strings.Split(*allowedPaths, ",")
		for i, path := range config.AllowedPaths {
			config.AllowedPaths[i] = strings.TrimSpace(path)
		}
	}

	// Setup logging
	setupLogging(config)

	log.Printf("🚀 Starting MCP Filesystem Server Ultra-Fast")
	log.Printf("📊 Config: Cache=%s, Parallel=%d, Binary=%s, VSCode=%v, AllowedPaths=%v",
		formatSize(config.CacheSize), config.ParallelOps,
		formatSize(config.BinaryThreshold), config.VSCodeAPIEnabled, config.AllowedPaths)

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

	// Initialize protocol handler
	protocolHandler := protocol.NewOptimizedHandler(config.BinaryThreshold)

	// Initialize core engine
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:            cacheSystem,
		ProtocolHandler:  protocolHandler,
		ParallelOps:      config.ParallelOps,
		VSCodeAPIEnabled: config.VSCodeAPIEnabled,
		DebugMode:        config.DebugMode,
		AllowedPaths:     config.AllowedPaths,
	})
	if err != nil {
		log.Fatalf("Failed to initialize engine: %v", err)
	}
	defer engine.Close()

	// Create MCP server implementation
	impl := &mcp.Implementation{
		Name:    "filesystem-ultra",
		Version: "1.0.0",
	}

	// Create server options
	opts := &mcp.ServerOptions{}

	// Create MCP server
	server := mcp.NewServer(impl, opts)

	// Register all optimized tools
	if err := registerTools(server, engine); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start performance monitoring
	go engine.StartMonitoring(ctx)

	log.Printf("✅ Server ready - Waiting for connections...")

	// Create stdio transport and connect
	transport := mcp.NewStdioTransport()
	_, err = server.Connect(ctx, transport)
	if err != nil {
		log.Fatalf("Server connection error: %v", err)
	}
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

// registerTools registers all optimized filesystem tools
func registerTools(server *mcp.Server, engine *core.UltraFastEngine) error {
	tools := []struct {
		name        string
		description string
		handler     func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
	}{
		// Core ultra-fast operations
		{
			"read_file",
			"Read file with ultra-fast caching and memory mapping",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				request := mcp.CallToolRequest{Arguments: params.Arguments}
				response, err := engine.ReadFile(ctx, request)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: response.Content[0].Text}},
				}, nil
			},
		},
		{
			"write_file",
			"Write file with atomic operations and backup",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				request := mcp.CallToolRequest{Arguments: params.Arguments}
				response, err := engine.WriteFile(ctx, request)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: response.Content[0].Text}},
				}, nil
			},
		},
		{
			"list_directory",
			"List directory with intelligent caching",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				request := mcp.CallToolRequest{Arguments: params.Arguments}
				response, err := engine.ListDirectory(ctx, request)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: response.Content[0].Text}},
				}, nil
			},
		},
		// Ultra-fast editing operations (like Cline)
		{
			"edit_file",
			"Intelligent file editing with backup and rollback - ultra-fast like Cline",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				path, _ := params.Arguments["path"].(string)
				oldText, _ := params.Arguments["old_text"].(string)
				newText, _ := params.Arguments["new_text"].(string)
				
				if path == "" || oldText == "" {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: "Error: path, old_text, and new_text are required"}},
					}, nil
				}
				
				result, err := engine.EditFile(path, oldText, newText)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{
						Text: fmt.Sprintf("✅ Successfully edited %s\n📊 Changes: %d replacement(s)\n🎯 Match confidence: %s\n📝 Lines affected: %d",
							path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected),
					}},
				}, nil
			},
		},
		{
			"search_and_replace",
			"Ultra-fast search and replace across files and directories",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				path, _ := params.Arguments["path"].(string)
				pattern, _ := params.Arguments["pattern"].(string)
				replacement, _ := params.Arguments["replacement"].(string)
				caseSensitive, _ := params.Arguments["case_sensitive"].(bool)
				
				if path == "" || pattern == "" || replacement == "" {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: "Error: path, pattern, and replacement are required"}},
					}, nil
				}
				
				response, err := engine.SearchAndReplace(path, pattern, replacement, caseSensitive)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: response.Content[0].Text}},
				}, nil
			},
		},
		// Advanced search operations
		{
			"smart_search",
			"Intelligent search with regex and content filtering",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				request := mcp.CallToolRequest{Arguments: params.Arguments}
				response, err := engine.SmartSearch(ctx, request)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: response.Content[0].Text}},
				}, nil
			},
		},
		{
			"advanced_text_search",
			"Advanced text search with context and flexible matching",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				request := mcp.CallToolRequest{Arguments: params.Arguments}
				response, err := engine.AdvancedTextSearch(ctx, request)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: response.Content[0].Text}},
				}, nil
			},
		},
		// Performance monitoring
		{
			"performance_stats",
			"Get real-time performance statistics",
			func(ctx context.Context, session *mcp.ServerSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				request := mcp.CallToolRequest{Arguments: params.Arguments}
				response, err := engine.PerformanceStats(ctx, request)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
					}, nil
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: response.Content[0].Text}},
				}, nil
			},
		},
	}

	// Register all tools
	for _, tool := range tools {
		mcpTool := &mcp.Tool{
			Name:        tool.name,
			Description: tool.description,
		}
		mcp.AddTool(server, mcpTool, tool.handler)
	}

	log.Printf("📚 Registered %d ultra-fast tools (including Cline-like editing)", len(tools))
	return nil
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