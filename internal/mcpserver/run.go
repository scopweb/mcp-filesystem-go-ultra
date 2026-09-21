package mcpserver

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// Configuration holds all server configuration
type Configuration struct {
	CacheSize        int64         // Cache size in bytes
	CacheTTL         time.Duration // File content cache life (retention, not coherence)
	ParallelOps      int           // Max concurrent operations
	BinaryThreshold  int64         // File size threshold for binary protocol
	VSCodeAPIEnabled bool          // Enable VSCode API integration when available
	DebugMode        bool          // Enable debug logging
	LogLevel         string        // Log level (info, debug, error)
	AllowedPaths     []string      // List of allowed base paths for access control
	CompactMode      bool          // Enable compact responses (minimal tokens)
	MaxResponseSize  int64         // Max response size in bytes
	MaxSearchResults int           // Max search results to return
	MaxListItems     int           // Max items in directory listings
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
		CacheTTL:         cache.DefaultFileTTL,
		ParallelOps:      parallelOps,
		BinaryThreshold:  1024 * 1024, // 1MB threshold
		VSCodeAPIEnabled: true,
		DebugMode:        false,
		LogLevel:         "info",
		AllowedPaths:     []string{},       // Fail-closed at startup unless --insecure-open
		CompactMode:      false,            // Verbose by default
		MaxResponseSize:  10 * 1024 * 1024, // 10MB default
		MaxSearchResults: 1000,             // 1000 results default
		MaxListItems:     500,              // 500 items default
	}
}

// serverInstructions is sent to the client during the MCP initialize handshake.
// Keep this factual and compact (≤12 lines): detailed workflow policy belongs
// in help(tool:X) and the filesystem-ultra-tools skill, not in every user turn.
// Subagents do not inherit the skill; these lines are the portable contract.
// Never dump the full catalog here.
const serverInstructions = `MCP Filesystem Ultra operates on the real host filesystem (C:\, D:\, /mnt/...). Runtime-native tools may target a different sandbox.
First call list_allowed_directories.
Do not use native Read/Edit/Write/Glob for host paths. Prefer these tools over bash cat/head/tail/cut/ls/grep/find/stat.
Subagent: request a handoff (path + content_hash). Do not assume the parent session's file body.
help(tool:X) on demand. Do not load the full catalog at startup.`

// serverVersion is the single source of truth for the version reported by
// --version, the MCP handshake, the help header and the startup logs.
// Keep in sync with the top CHANGELOG entry.
const serverVersion = "4.7.1"

// BuildCommit and BuildDate are stamped at build time via
//
//	-ldflags "-X github.com/mcp/filesystem-ultra/internal/mcpserver.BuildCommit=<hash> \
//	          -X github.com/mcp/filesystem-ultra/internal/mcpserver.BuildDate=<date>"
//
// and default to dev/unknown for un-stamped builds (plain go build).
// They are exported because -X can only stamp package-level exported or
// unexported vars by full package path; keeping them exported documents that
// the build scripts write them.
var (
	BuildCommit = "dev"
	BuildDate   = "unknown"
)

// Run is the server entry point, invoked by cmd/filesystem-ultra.
func Run() {
	config := DefaultConfiguration()

	// Parse command line arguments
	var (
		cacheSize        = flag.String("cache-size", "100MB", "Memory cache limit (e.g., 50MB, 1GB)")
		cacheTTL         = flag.String("cache-ttl", "3m", "File content cache life (Go duration, e.g. 3m, 10m). Retention only; freshness is size/mtime. Does not apply to directory listings.")
		parallelOps      = flag.Int("parallel-ops", config.ParallelOps, "Max concurrent operations")
		binaryThreshold  = flag.String("binary-threshold", "1MB", "File size threshold for binary protocol")
		vsCodeAPI        = flag.Bool("vscode-api", true, "Enable VSCode API integration when available")
		debugMode        = flag.Bool("debug", false, "Enable debug mode")
		logLevel         = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		allowedPaths     = flag.String("allowed-paths", "", "Comma-separated list of allowed base paths (required unless --insecure-open; alternative: pass paths as positional arguments)")
		insecureOpen     = flag.Bool("insecure-open", false, "Disable access control (entire disk). Labs only. Default since v4.6.0 is fail-closed.")
		rootsMode        = flag.String("roots-mode", "replace", "How MCP client Roots combine with CLI paths: replace (default), union, ignore")
		readOnly         = flag.Bool("readonly", false, "Reject mutating tools (READONLY)")
		mutationBudget   = flag.Int("mutation-budget", 0, "Max applied mutations per process (0=off). Shared by all stdio clients.")
		allowSecrets     = flag.Bool("allow-secrets", false, "Allow reading secret files (.env, *.pem, keys). Audited.")
		profileFlag      = flag.String("profile", "ultra", "Tool catalog: ultra (default, all tools) or strict (agent core only)")
		gitNetwork       = flag.Bool("git-network", false, "Enable git push/fetch (network). Off by default; ignored in profile=strict.")
		gitRemoteAllow   = flag.String("git-remote-allow", "", "Optional. Comma-separated allowed push/fetch destinations (host, host/org, or repo URL). Empty = no extra lock (any configured remote). Example: github.com,gitlab.com. force:true does not bypass. git(action:\"remote\") works without --git-network.")
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

		// Auto-OCC (new point 4): automatic optimistic-concurrency check on edits
		// without an explicit expected_hash. off | warn (default) | block.
		autoOCC = flag.String("auto-occ", "warn", "Auto optimistic-concurrency on edits: off|warn|block (default warn)")

		// Risk thresholds
		riskThresholdMedium   = flag.Float64("risk-threshold-medium", 20.0, "Percentage change threshold for medium risk")
		riskThresholdHigh     = flag.Float64("risk-threshold-high", 75.0, "Percentage change threshold for high risk")
		riskOccurrencesMedium = flag.Int("risk-occurrences-medium", 50, "Number of occurrences threshold for medium risk")
		riskOccurrencesHigh   = flag.Int("risk-occurrences-high", 100, "Number of occurrences threshold for high risk")
	)
	flag.Parse()

	// Configure auto-OCC mode (new point 4).
	core.SetAutoOCCMode(*autoOCC)

	if *version {
		fmt.Printf("MCP Filesystem Server Ultra-Fast v%s\n", serverVersion)
		fmt.Printf("Protocol: MCP 2025-11-25\n")
		fmt.Printf("Commit: %s\n", BuildCommit)
		fmt.Printf("Build: %s\n", BuildDate)
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

	if ttl, err := cache.ParseFileTTL(*cacheTTL); err != nil {
		log.Fatalf("Invalid cache TTL: %v", err)
	} else {
		config.CacheTTL = ttl
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
		config.AllowedPaths = sanitizeAllowedPaths(strings.Split(*allowedPaths, ","))
	} else if additionalArgs := flag.Args(); len(additionalArgs) > 0 {
		config.AllowedPaths = sanitizeAllowedPaths(additionalArgs)
	}

	// Fail-closed: refuse to start with an open disk unless --insecure-open.
	// --version already returned. --bench is allowed without a sandbox.
	if !*benchmark {
		if err := requireAllowedPaths(config.AllowedPaths, *insecureOpen); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
	}

	// Setup logging
	setupLogging(config)
	if *insecureOpen {
		log.Printf("WARNING: --insecure-open: sandbox disabled (entire disk). Labs only.")
	}

	log.Printf("Starting MCP Filesystem Server Ultra-Fast v%s (commit %s)", serverVersion, BuildCommit)
	log.Printf("Config: Cache=%s ttl=%s, Parallel=%d, Binary=%s, VSCode=%v, Compact=%v",
		formatSize(config.CacheSize), config.CacheTTL, config.ParallelOps,
		formatSize(config.BinaryThreshold), config.VSCodeAPIEnabled, config.CompactMode)

	if *benchmark {
		runBenchmark(config)
		return
	}

	// Initialize components
	ctx := context.Background()

	// Initialize cache system
	cacheSystem, err := cache.NewIntelligentCacheTTL(config.CacheSize, config.CacheTTL)
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
		ReadOnly:              *readOnly,
		AllowSecrets:          *allowSecrets,
		RootsMode:             core.ParseRootsMode(*rootsMode),
		MutationBudget:        *mutationBudget,
		GitRemoteAllow:        sanitizeAllowedPaths(strings.Split(*gitRemoteAllow, ",")),
	})
	if err != nil {
		log.Fatalf("Failed to initialize engine: %v", err)
	}
	defer engine.Close()

	// Create MCP server using mark3labs SDK
	s := server.NewMCPServer(
		"filesystem-ultra",
		serverVersion,
		server.WithToolCapabilities(true), // listChanged=true enables tools/list_changed notifications
		server.WithRoots(),
		server.WithResourceCapabilities(false, true),
		server.WithLogging(),
		server.WithInstructions(serverInstructions),
	)

	activeProfile, profErr := parseToolProfile(*profileFlag)
	if profErr != nil {
		log.Fatal(profErr)
	}
	if err := registerToolsOpts(s, engine, registerOpts{Profile: activeProfile, GitNetwork: *gitNetwork}); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	registerRootsSync(s, engine, config.AllowedPaths, core.ParseRootsMode(*rootsMode))
	registerFileResources(s, engine)

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
