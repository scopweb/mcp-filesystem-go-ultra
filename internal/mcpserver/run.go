package mcpserver

import (
	"log/slog"
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
	ResponseMode     string        // compat or structured
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
Do not use native Read/Edit/Write/Glob for host paths. Prefer these tools over bash cat/head/tail/cut/ls/grep/find/wc/stat. Line census: list_directory output_format=lines. $HOME/mnt is a sandbox path; use the Windows path from list_allowed_directories.
Subagent: request a handoff (path + content_hash). Do not assume the parent session's file body.
help(tool:X) on demand. Do not load the full catalog at startup.`

// serverVersion is the single source of truth for the version reported by
// --version, the MCP handshake, the help header and the startup logs.
// Keep in sync with the top CHANGELOG entry.
const serverVersion = "4.8.0"

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

func newFilesystemMCPServer() *server.MCPServer {
	return server.NewMCPServer(
		"filesystem-ultra",
		serverVersion,
		server.WithToolCapabilities(true),
		server.WithRoots(),
		server.WithResourceCapabilities(false, true),
		server.WithLogging(),
		server.WithInstructions(serverInstructions),
		server.WithRecovery(),
		server.WithResourceRecovery(),
	)
}

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
		logLevel         = flag.String("log-level", "info", "Stderr log level: debug, info, warn, error. debug is the same as --debug.")
		allowedPaths     = flag.String("allowed-paths", "", "Comma-separated list of allowed base paths (required unless --insecure-open; alternative: pass paths as positional arguments)")
		insecureOpen     = flag.Bool("insecure-open", false, "Disable access control (entire disk). Labs only. Default since v4.6.0 is fail-closed.")
		rootsMode        = flag.String("roots-mode", "replace", "How MCP client Roots combine with CLI paths: replace (default), union, ignore")
		readOnly         = flag.Bool("readonly", false, "Reject mutating tools (READONLY)")
		mutationBudget   = flag.Int("mutation-budget", 0, "Max applied mutations per process (0=off). Shared by all stdio clients.")
		allowSecrets     = flag.Bool("allow-secrets", false, "Allow reading secret files (.env, *.pem, keys). Audited.")
		profileFlag      = flag.String("profile", "ultra", "Tool catalog: ultra (default, all tools), strict (agent core), or agent (strict plus git)")
		gitNetwork       = flag.Bool("git-network", false, "Enable git push/fetch (network). Off by default; ignored in profile=strict. Honored by ultra and agent.")
		gitRemoteAllow   = flag.String("git-remote-allow", "", "Optional. Comma-separated allowed push/fetch destinations (host, host/org, or repo URL). Empty = no extra lock (any configured remote). Example: github.com,gitlab.com. force:true does not bypass. git(action:\"remote\") works without --git-network.")
		compactMode      = flag.Bool("compact-mode", false, "Enable compact responses (minimal tokens for Claude Desktop)")
		responseMode     = flag.String("response-mode", "compat", "Text fallback: compat (full text + structured) or structured (brief text; bodies stay once in structuredContent)")
		maxResponseSize  = flag.String("max-response-size", "10MB", "Maximum response size")
		maxSearchResults = flag.Int("max-search-results", 1000, "Ceiling for search_files max_results (omitted page is 40)")
		maxListItems     = flag.Int("max-list-items", 500, "Maximum items in directory listings")
		maxInspectScan   = flag.Int("max-inspect-scan-bytes", inspectScanMaximum, "Maximum file_bytes diagnostic prefix in bytes (1..67108864); default scan is 1 MiB")
		hooksEnabled     = flag.Bool("hooks-enabled", false, "Enable hooks system for pre/post operation validation and formatting")
		hooksConfig      = flag.String("hooks-config", "", "Path to hooks configuration JSON file (e.g., hooks.json)")
		version          = flag.Bool("version", false, "Show version information")
		benchmark        = flag.Bool("bench", false, "Run performance benchmark")

		// Backup configuration
		backupDir      = flag.String("backup-dir", "", "Directory for backup storage (default: temp/mcp-batch-backups)")
		receiptDir     = flag.String("receipt-dir", "", "Directory for e3-v1 retry receipts (empty = process-local memory only)")
		lockTimeout    = flag.Duration("lock-timeout", 10*time.Second, "Max wait for a per-file mutation lock held by another operation or server process; on expiry the call fails with LOCK_BUSY (retryable). 0 = wait until the client cancels.")
		backupMaxAge   = flag.Int("backup-max-age", 7, "Max age of backups in days")
		backupMaxCount = flag.Int("backup-max-count", 100, "Max number of backups to keep")

		// Logging
		logDir          = flag.String("log-dir", "", "Directory for audit logs and metrics snapshots (enables operation logging)")
		normalizerRules = flag.String("normalizer-rules", "", "Path to external normalizer rules JSON file (extends built-in rules)")

		// Auto-OCC (new point 4): automatic optimistic-concurrency check on edits
		// without an explicit expected_hash. off | warn (default) | block.
		autoOCC            = flag.String("auto-occ", "warn", "Auto optimistic-concurrency on edits: off|warn|block (default warn). State is per process; block only refuses while the known hash is under 2m old, older baselines warn.")
		fileSecurityConfig = flag.String("file-security-config", "", "Path to a JSON file security policy. Invalid config aborts startup. Immutable for the process lifetime.")

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
	mode, modeErr := core.ParseResponseMode(*responseMode)
	if modeErr != nil {
		fmt.Fprintln(os.Stderr, modeErr.Error())
		os.Exit(2)
	}
	config.ResponseMode = mode
	config.MaxSearchResults = *maxSearchResults
	config.MaxListItems = *maxListItems
	if *maxInspectScan <= 0 || *maxInspectScan > 64<<20 {
		fmt.Fprintln(os.Stderr, "--max-inspect-scan-bytes must be in 1..67108864")
		os.Exit(2)
	}

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
	if err := setupLogging(config); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}

	// Process lifecycle trace (start/stop/crash) under --log-dir. Nil when
	// --log-dir is empty; every call on it is then a no-op.
	lifecycle := newLifecycleLogger(*logDir)
	crashFile := lifecycle.installCrashOutput()
	startedAt := time.Now()
	lifecycle.event("start", map[string]any{
		"version":    serverVersion,
		"commit":     BuildCommit,
		"build_date": BuildDate,
		"profile":    *profileFlag,
		"args":       os.Args[1:],
		"crash_file": crashFile,
	})
	defer func() {
		if r := recover(); r != nil {
			lifecycle.event("stop", map[string]any{
				"reason":   "panic",
				"value":    fmt.Sprint(r),
				"uptime_s": int64(time.Since(startedAt).Seconds()),
			})
			panic(r) // the runtime crash report lands in crash_file
		}
	}()
	if *insecureOpen {
		slog.Warn("--insecure-open: sandbox disabled (entire disk). Labs only.")
	}

	slog.Info("starting filesystem-ultra", "version", serverVersion, "commit", BuildCommit)
	slog.Info("config", "cache", formatSize(config.CacheSize), "ttl", config.CacheTTL,
		"parallel", config.ParallelOps, "binary", formatSize(config.BinaryThreshold),
		"vscode", config.VSCodeAPIEnabled, "compact", config.CompactMode)

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
		Cache:               cacheSystem,
		ParallelOps:         config.ParallelOps,
		VSCodeAPIEnabled:    config.VSCodeAPIEnabled,
		DebugMode:           config.DebugMode,
		AllowedPaths:        config.AllowedPaths,
		BinaryThreshold:     config.BinaryThreshold,
		CompactMode:         config.CompactMode,
		ResponseMode:        config.ResponseMode,
		MaxResponseSize:     config.MaxResponseSize,
		MaxSearchResults:    config.MaxSearchResults,
		MaxListItems:        config.MaxListItems,
		MaxInspectScanBytes: *maxInspectScan,
		HooksEnabled:        *hooksEnabled,
		HooksConfigPath:     *hooksConfig,

		// Backup configuration
		BackupDir:      *backupDir,
		BackupMaxAge:   *backupMaxAge,
		BackupMaxCount: *backupMaxCount,

		// Logging
		LogDir:              *logDir,
		NormalizerRulesPath: *normalizerRules,
		ReceiptDir:          *receiptDir,
		LockTimeout:         *lockTimeout,

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
	if strings.TrimSpace(*fileSecurityConfig) != "" {
		policy, perr := core.LoadFilePolicy(*fileSecurityConfig)
		if perr != nil {
			fmt.Fprintln(os.Stderr, perr.Error())
			os.Exit(2)
		}
		if *insecureOpen {
			if perr = policy.RejectInsecureOpen(); perr != nil {
				fmt.Fprintln(os.Stderr, perr.Error())
				os.Exit(2)
			}
		}
		if perr = policy.PinConfig(*fileSecurityConfig); perr != nil {
			fmt.Fprintln(os.Stderr, perr.Error())
			os.Exit(2)
		}
		engine.SetFilePolicy(policy)
		slog.Info("file security policy loaded (immutable until restart)")
	}

	s := newFilesystemMCPServer()

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

	slog.Info("server ready")

	// Start the stdio server using new API. ServeStdio returns nil on stdin
	// EOF (client closed the pipe) and on SIGINT/SIGTERM, which it handles
	// itself; any other return is a transport error.
	serveErr := server.ServeStdio(s)
	stop := map[string]any{
		"reason":   "stdin_closed_or_signal",
		"uptime_s": int64(time.Since(startedAt).Seconds()),
	}
	if serveErr != nil {
		stop["reason"] = "serve_error"
		stop["error"] = serveErr.Error()
	}
	lifecycle.event("stop", stop)
	lifecycle.removeEmptyCrashFile()
	if serveErr != nil {
		log.Fatalf("Server error: %v", serveErr)
	}
}
