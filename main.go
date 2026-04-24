package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
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

// serverInstructions is sent to the client during the MCP initialize handshake
// and returned by the help tool.
const serverInstructions = `MCP Filesystem Ultra — File operations server. Run 'help' for tool list.`

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

		// Logging
		logDir          = flag.String("log-dir", "", "Directory for audit logs and metrics snapshots (enables operation logging)")
		normalizerRules = flag.String("normalizer-rules", "", "Path to external normalizer rules JSON file (extends built-in rules)")

		// Risk thresholds
		riskThresholdMedium   = flag.Float64("risk-threshold-medium", 20.0, "Percentage change threshold for medium risk")
		riskThresholdHigh     = flag.Float64("risk-threshold-high", 75.0, "Percentage change threshold for high risk")
		riskOccurrencesMedium = flag.Int("risk-occurrences-medium", 50, "Number of occurrences threshold for medium risk")
		riskOccurrencesHigh   = flag.Int("risk-occurrences-high", 100, "Number of occurrences threshold for high risk")
	)
	flag.Parse()

	if *version {
		fmt.Printf("MCP Filesystem Server Ultra-Fast v4.2.1\n")
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

	log.Printf("Starting MCP Filesystem Server Ultra-Fast v4.2.1")
	log.Printf("Config: Cache=%s, Parallel=%d, Binary=%s, VSCode=%v, Compact=%v",
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

		// Logging
		LogDir:              *logDir,
		NormalizerRulesPath: *normalizerRules,

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
		"4.2.0",
		server.WithToolCapabilities(true), // listChanged=true enables tools/list_changed notifications
		server.WithLogging(),
		server.WithInstructions(serverInstructions),
	)

	// Register all 16 consolidated tools
	if err := registerTools(s, engine); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start performance monitoring
	go engine.StartMonitoring(ctx)

	log.Printf("Server ready - Waiting for connections...")

	// Start the stdio server using new API
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
