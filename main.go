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

// serverInstructions is sent to the client during the MCP initialize handshake
// and returned by the help tool. Ensures the AI knows ALL available tools.
const serverInstructions = `You have access to MCP Filesystem Ultra with 16 tools. Use the RIGHT tool for each task:

## Editing files (IMPORTANT)
- **edit_file**: Smart text replacement with auto-backup. Modes: default (replace), search_replace, regex. ALWAYS prefer this over write_file for existing files.
- **multi_edit**: Multiple edits to one file atomically. MUCH faster than calling edit_file multiple times.
- **write_file**: Create new files or full overwrite only. Supports base64 for binary.

## Reading files
- **read_file**: Read file contents. Use start_line/end_line for ranges. Supports base64, head/tail modes.

## File operations
- **copy_file**: Copy files or directories.
- **move_file**: Move or rename files and directories.
- **delete_file**: Soft-delete (trash) by default. Use permanent:true for hard delete.
- **create_directory**: Create directories with parents.

## Directory & search
- **list_directory**: Cached directory listing.
- **search_files**: Search by name or content. Supports regex, count_only mode.

## Analysis & planning
- **get_file_info**: File/directory metadata.
- **analyze_operation**: Dry-run analysis before executing operations.

## Bulk & advanced
- **batch_operations**: Batch ops, pipelines, and batch rename in one call.
- **backup**: List, info, compare, cleanup, restore, undo_last.
- **wsl**: WSL/Windows file sync and path conversion.

## System
- **server_info**: Help topics, stats, artifact management.

## Recommended workflow
1. **Locate** → search_files(include_content:true, context_lines:3) to find exact blocks
2. **Read range** → read_file(start_line/end_line) for precise context
3. **Edit** → edit_file (NOT write_file) for modifications
4. **Verify** → analyze_operation or read_file after large edits

## Key rules
1. To modify existing files: use edit_file, NOT write_file
2. For multiple edits in one file: use multi_edit, NOT multiple edit_file calls
3. Surgical edits save 98% tokens: search_files -> read_file(range) -> edit_file
4. For bulk renames/deletes: use batch_operations, not individual calls
5. Check backup before destructive ops: backup(action:"list")
6. After large edits (>10 lines): verify with read_file or analyze_operation`

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
		fmt.Printf("MCP Filesystem Server Ultra-Fast v4.1.3\n")
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

	log.Printf("Starting MCP Filesystem Server Ultra-Fast v4.1.3")
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
		"4.1.3",
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

// auditWrap wraps a tool handler with request normalization and audit logging.
// Normalization: applies data-driven rules (param aliases, type coercions, nested fixes).
// Audit: creates an AuditEntry, injects it into context, logs timing/status/path on completion.
func auditWrap(engine *core.UltraFastEngine, tool string, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()

		// Create audit entry and inject into context for sub-op annotation
		entry := &core.AuditEntry{Timestamp: start, Tool: tool}
		ctx = context.WithValue(ctx, core.AuditEntryKey{}, entry)

		// Run normalizer on arguments
		if normalizer := engine.GetNormalizer(); normalizer != nil {
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				result := normalizer.Normalize(tool, args)
				if result.WasModified {
					request.Params.Arguments = result.Args
					entry.Normalizations = result.Applied
				}
			}
		}

		// Call actual handler
		res, err := handler(ctx, request)

		// Complete audit entry
		entry.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			entry.Status = "error"
			entry.Error = err.Error()
		} else if res != nil && res.IsError {
			entry.Status = "error"
			// Extract error text from result content
			if len(res.Content) > 0 {
				if tc, ok := res.Content[0].(mcp.TextContent); ok {
					entry.Error = tc.Text
				}
			}
		} else {
			entry.Status = "ok"
		}

		// Extract path and summarize args for logging
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if p, ok := args["path"].(string); ok {
				entry.Path = p
			}
			entry.Args = summarizeArgs(args)
			// Extract bytes_out from result
			if res != nil && len(res.Content) > 0 {
				if tc, ok := res.Content[0].(mcp.TextContent); ok {
					entry.BytesOut = int64(len(tc.Text))
				}
			}
		}

		// Log the audit entry
		engine.Audit(*entry)

		return res, err
	}
}

// summarizeArgs creates a compact map of key arguments for audit logging
func summarizeArgs(args map[string]interface{}) map[string]string {
	summary := make(map[string]string)
	for k, v := range args {
		switch k {
		case "content", "edits_json", "pipeline_json", "request_json", "rename_json":
			// Skip large payloads, just note their presence
			if s, ok := v.(string); ok {
				summary[k] = fmt.Sprintf("(%d chars)", len(s))
			} else {
				summary[k] = "(present)"
			}
		default:
			s := fmt.Sprintf("%v", v)
			if len(s) > 100 {
				s = s[:100] + "..."
			}
			summary[k] = s
		}
	}
	return summary
}

// registerTools registers all 16 consolidated filesystem tools
func registerTools(s *server.MCPServer, engine *core.UltraFastEngine) error {
	// Create modular processor for regex transforms (used by edit_file regex mode)
	regexTransform := core.NewRegexTransformer(engine)

	// ============================================================================
	// 1. read_file — Read file (consolidated: mcp_read + read_file + read_file_range + read_base64 + chunked_read + intelligent_read)
	// ============================================================================
	readFileTool := mcp.NewTool("read_file",
		mcp.WithTitleAnnotation("Read File"),
		mcp.WithDescription("Read file contents. Supports line ranges (start_line/end_line), base64 encoding for binary files (encoding:\"base64\"), and mode control (head/tail). To MODIFY files use edit_file (not write_file). Other tools: search_files, multi_edit, batch_operations, backup, analyze_operation."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithNumber("max_lines", mcp.Description("Max lines (optional, 0=all)")),
		mcp.WithString("mode", mcp.Description("Mode: all, head, tail")),
		mcp.WithNumber("start_line", mcp.Description("Starting line number (1-indexed) for range read")),
		mcp.WithNumber("end_line", mcp.Description("Ending line number (inclusive) for range read")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" to read file as base64-encoded binary")),
	)
	readFileHandler := auditWrap(engine, "read_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Get optional parameters
		maxLines := 0
		mode := "all"
		startLine := 0
		endLine := 0
		encoding := ""

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if ml, ok := args["max_lines"].(float64); ok {
				maxLines = int(ml)
			}
			if m, ok := args["mode"].(string); ok && m != "" {
				mode = m
			}
			if sl, ok := args["start_line"].(float64); ok {
				startLine = int(sl)
			}
			if el, ok := args["end_line"].(float64); ok {
				endLine = int(el)
			}
			if enc, ok := args["encoding"].(string); ok {
				encoding = enc
			}
		}

		// Base64 mode: read binary file as base64
		if encoding == "base64" {
			encoded, originalSize, err := engine.ReadBase64(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText(encoded), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("# File: %s (%d bytes)\n# Base64 encoded:\n%s", path, originalSize, encoded)), nil
		}

		// Range read mode: read specific line range
		// If start_line is set but end_line is not, read from start_line to end of file
		if startLine > 0 && endLine == 0 {
			endLine = 999999 // ReadFileRange will clamp to actual file length
		}
		// If end_line is set but start_line is not, read from line 1
		if endLine > 0 && startLine == 0 {
			startLine = 1
		}
		if startLine > 0 && endLine > 0 {
			content, err := engine.ReadFileRange(ctx, path, startLine, endLine)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(content), nil
		}

		// Default: read full file
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
	s.AddTool(readFileTool, readFileHandler)

	// ============================================================================
	// 2. write_file — Write file (consolidated: mcp_write + write_file + create_file + write_base64 + streaming_write + intelligent_write)
	// ============================================================================
	writeFileTool := mcp.NewTool("write_file",
		mcp.WithTitleAnnotation("Write File"),
		mcp.WithDescription("Create NEW files or full overwrite. For modifying existing files use edit_file instead. Supports base64 binary (encoding:\"base64\"). Other tools: edit_file, multi_edit, copy_file, move_file, batch_operations, backup."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write (WSL or Windows format)")),
		mcp.WithString("content", mcp.Description("Text content to write to the file")),
		mcp.WithString("content_base64", mcp.Description("Base64-encoded binary content to write")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" when content is base64-encoded")),
	)
	writeFileHandler := auditWrap(engine, "write_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Check for base64 content
		contentBase64 := ""
		encoding := ""
		content := ""

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if cb, ok := args["content_base64"].(string); ok {
				contentBase64 = cb
			}
			if enc, ok := args["encoding"].(string); ok {
				encoding = enc
			}
			if c, ok := args["content"].(string); ok {
				content = c
			}
		}

		// Base64 write mode
		if contentBase64 != "" || encoding == "base64" {
			b64Content := contentBase64
			if b64Content == "" {
				b64Content = content
			}
			if b64Content == "" {
				return mcp.NewToolResultError("content_base64 or content with encoding:\"base64\" is required"), nil
			}

			// Validate base64 before passing to engine (fast fail)
			if _, decodeErr := base64.StdEncoding.DecodeString(b64Content); decodeErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid base64: %v", decodeErr)), nil
			}

			bytesWritten, err := engine.WriteBase64(ctx, path, b64Content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText(fmt.Sprintf("OK: %s written", formatSize(int64(bytesWritten)))), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes (from base64) to %s", bytesWritten, path)), nil
		}

		// Normal text write
		if content == "" {
			// Try RequireString for backward compat
			c, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid content: %v", err)), nil
			}
			content = c
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
	s.AddTool(writeFileTool, writeFileHandler)

	// ============================================================================
	// 3. edit_file — Edit file (consolidated: mcp_edit + edit_file + search_and_replace + replace_nth_occurrence + regex_transform_file + smart_edit + intelligent_edit + recovery_edit)
	// ============================================================================
	editFileTool := mcp.NewTool("edit_file",
		mcp.WithTitleAnnotation("Edit File"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("Modify existing files. Modes: default (smart replace), search_replace, regex. Auto-backup with undo. "+
			"ALWAYS prefer this over write_file for existing files. "+
			"UNDO: backup(action:\"restore\", backup_id:\"...\") or backup(action:\"undo_last\"). "+
			"Other tools: multi_edit (multiple edits atomically), read_file, search_files, batch_operations, backup, analyze_operation."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file (WSL or Windows format)")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced (default mode)")),
		mcp.WithString("new_text", mcp.Description("New text to replace with (default mode)")),
		mcp.WithString("old_str", mcp.Description("Alias for old_text")),
		mcp.WithString("new_str", mcp.Description("Alias for new_text")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if CRITICAL risk (default: false)")),
		mcp.WithString("mode", mcp.Description("Edit mode: \"replace\" (default), \"search_replace\", \"regex\"")),
		mcp.WithNumber("occurrence", mcp.Description("Which occurrence to replace: 1=first, 2=second, -1=last, -2=second-to-last (default: all)")),
		// search_replace mode params
		mcp.WithString("pattern", mcp.Description("Regex or literal pattern (for search_replace and regex modes)")),
		mcp.WithString("replacement", mcp.Description("Replacement text (for search_replace mode)")),
		// regex mode params
		mcp.WithString("patterns_json", mcp.Description("JSON array of patterns for regex mode: [{\"pattern\": \"regex\", \"replacement\": \"$1...\", \"limit\": -1}]")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive matching (default: true, for regex mode)")),
		mcp.WithBoolean("create_backup", mcp.Description("Create backup before transformation (default: true, for regex mode)")),
		mcp.WithBoolean("dry_run", mcp.Description("Validate without applying changes (default: false, for regex mode)")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false, for occurrence mode)")),
	)
	editFileHandler := auditWrap(engine, "edit_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Extract all optional parameters
		args := request.GetArguments()
		mode := ""
		oldText := ""
		newText := ""
		force := false
		occurrence := 0

		if args != nil {
			if m, ok := args["mode"].(string); ok {
				mode = m
			}
			if f, ok := args["force"].(bool); ok {
				force = f
			}
			if occ, ok := args["occurrence"].(float64); ok {
				occurrence = int(occ)
			}
			// old_str/new_str → old_text/new_text aliasing handled by normalizer
			if ot, ok := args["old_text"].(string); ok {
				oldText = ot
			}
			if nt, ok := args["new_text"].(string); ok {
				newText = nt
			}
		}

		// ---- MODE: regex ----
		if mode == "regex" {
			patternsJSON := ""
			if args != nil {
				if pj, ok := args["patterns_json"].(string); ok {
					patternsJSON = pj
				}
			}
			if patternsJSON == "" {
				return mcp.NewToolResultError("patterns_json is required for mode:\"regex\""), nil
			}

			// Parse patterns
			var patterns []core.TransformPattern
			if err := json.Unmarshal([]byte(patternsJSON), &patterns); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to parse patterns JSON: %v", err)), nil
			}

			// Get optional regex params
			caseSensitive := true
			createBackup := true
			dryRun := false

			if args != nil {
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
			output.WriteString("Regex Transformation Complete\n")
			output.WriteString("---\n")
			output.WriteString(fmt.Sprintf("File: %s\n", result.FilePath))
			output.WriteString(fmt.Sprintf("Patterns Applied: %d/%d\n", result.PatternsApplied, len(patterns)))
			output.WriteString(fmt.Sprintf("Total Replacements: %d\n", result.TotalReplacements))
			output.WriteString(fmt.Sprintf("Lines Affected: %d\n", result.LinesAffected))
			output.WriteString(fmt.Sprintf("Duration: %v\n", result.Duration))

			if result.BackupID != "" {
				output.WriteString(fmt.Sprintf("Backup ID: %s\n", result.BackupID))
			}

			if len(result.Errors) > 0 {
				output.WriteString("\nErrors:\n")
				for _, err := range result.Errors {
					output.WriteString(fmt.Sprintf("  - %s\n", err))
				}
			}

			return mcp.NewToolResultText(output.String()), nil
		}

		// ---- MODE: search_replace ----
		if mode == "search_replace" {
			pattern := ""
			replacement := ""
			if args != nil {
				if p, ok := args["pattern"].(string); ok {
					pattern = p
				}
				if r, ok := args["replacement"].(string); ok {
					replacement = r
				}
			}
			if pattern == "" {
				return mcp.NewToolResultError("pattern is required for mode:\"search_replace\""), nil
			}
			if replacement == "" && args != nil {
				// Also accept new_text as replacement for convenience
				if nt, ok := args["new_text"].(string); ok {
					replacement = nt
				}
			}

			resp, err := engine.SearchAndReplace(ctx, path, pattern, replacement, false)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resp.Content) > 0 {
				return mcp.NewToolResultText(resp.Content[0].Text), nil
			}
			return mcp.NewToolResultText("No output"), nil
		}

		// ---- MODE: replace (default) with optional occurrence ----
		if oldText == "" {
			return mcp.NewToolResultError("old_text (or old_str) is required"), nil
		}

		// If occurrence is specified, use ReplaceNthOccurrence
		if occurrence != 0 {
			wholeWord := false
			if args != nil {
				if ww, ok := args["whole_word"].(string); ok {
					wholeWord = (ww == "true" || ww == "True" || ww == "TRUE")
				} else if ww, ok := args["whole_word"].(bool); ok {
					wholeWord = ww
				}
			}

			result, err := engine.ReplaceNthOccurrence(ctx, path, oldText, newText, occurrence, wholeWord)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			if engine.IsCompactMode() {
				return mcp.NewToolResultText(fmt.Sprintf("OK: replaced occurrence #%d", occurrence)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Successfully replaced occurrence #%d\nLine affected: %d\nConfidence: %s",
				occurrence, result.LinesAffected, result.MatchConfidence)), nil
		}

		// Default: standard EditFile
		result, err := engine.EditFile(ctx, path, oldText, newText, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}

		if engine.IsCompactMode() {
			msg := fmt.Sprintf("OK: %d changes", result.ReplacementCount)
			if result.BackupID != "" {
				msg += fmt.Sprintf(" [backup:%s | UNDO: backup(action:\"restore\", backup_id:\"%s\")]", result.BackupID, result.BackupID)
			}
			if result.RiskWarning != "" {
				msg += result.RiskWarning
			}
			return mcp.NewToolResultText(msg), nil
		}
		msg := fmt.Sprintf("Successfully edited %s\nChanges: %d replacement(s)\nMatch confidence: %s\nLines affected: %d",
			path, result.ReplacementCount, result.MatchConfidence, result.LinesAffected)
		if result.BackupID != "" {
			msg += fmt.Sprintf("\nBackup ID: %s\nUNDO: backup(action:\"restore\", backup_id:\"%s\")", result.BackupID, result.BackupID)
		}
		if result.RiskWarning != "" {
			msg += result.RiskWarning
		}
		if result.LinesAffected > 10 {
			msg += "\nTIP: Use read_file to verify large edits, or analyze_operation for dry-run preview"
		}
		return mcp.NewToolResultText(msg), nil
	})
	s.AddTool(editFileTool, editFileHandler)

	// ============================================================================
	// 4. list_directory — List directory (consolidated: mcp_list + list_directory)
	// ============================================================================
	listDirTool := mcp.NewTool("list_directory",
		mcp.WithTitleAnnotation("List Directory"),
		mcp.WithDescription("List directory contents (cached). Other tools: search_files (find by name/content), read_file, edit_file, create_directory, analyze_operation, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to directory (WSL or Windows format)")),
	)
	listDirHandler := auditWrap(engine, "list_directory", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
	s.AddTool(listDirTool, listDirHandler)

	// ============================================================================
	// 5. search_files — Search files (consolidated: mcp_search + smart_search + advanced_text_search + count_occurrences)
	// ============================================================================
	searchFilesTool := mcp.NewTool("search_files",
		mcp.WithTitleAnnotation("Search Files"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("Search files by name/content. Supports regex, count_only, include_content. "+
			"Other tools: edit_file (modify files), read_file (line ranges), multi_edit, batch_operations, analyze_operation, backup."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Base directory or file (WSL or Windows format)")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
		mcp.WithBoolean("include_content", mcp.Description("Include file content search (default: false)")),
		mcp.WithString("file_types", mcp.Description("Comma-separated file extensions (e.g., '.go,.txt')")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive search (default: false)")),
		mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false)")),
		mcp.WithBoolean("include_context", mcp.Description("Include context lines (default: false)")),
		mcp.WithNumber("context_lines", mcp.Description("Number of context lines (default: 3)")),
		mcp.WithBoolean("count_only", mcp.Description("Count pattern occurrences without full search (default: false)")),
		mcp.WithString("return_lines", mcp.Description("Return line numbers of count matches (true/false, for count_only mode)")),
	)
	searchFilesHandler := auditWrap(engine, "search_files", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// Normalize WSL/Windows paths (Bug #19: was missing, causing /mnt/c/ → C:\mnt\c\)
		path = core.NormalizePath(path)

		pattern, err := request.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Extract optional parameters
		countOnly := false
		includeContent := false
		caseSensitive := false
		wholeWord := false
		includeContext := false
		contextLines := 3
		fileTypes := []interface{}{}
		returnLines := false

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if co, ok := args["count_only"].(bool); ok {
				countOnly = co
			}
			if ic, ok := args["include_content"].(bool); ok {
				includeContent = ic
			}
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
			if ft, ok := args["file_types"].(string); ok && ft != "" {
				parts := strings.Split(ft, ",")
				for _, part := range parts {
					fileTypes = append(fileTypes, strings.TrimSpace(part))
				}
			}
			if rl, ok := args["return_lines"].(string); ok {
				returnLines = (rl == "true" || rl == "True" || rl == "TRUE")
			} else if rl, ok := args["return_lines"].(bool); ok {
				returnLines = rl
			}
		}

		// Count-only mode: dispatch to CountOccurrences
		if countOnly {
			result, err := engine.CountOccurrences(ctx, path, pattern, returnLines)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(result), nil
		}

		// Advanced search mode (with context, case sensitivity, whole word)
		if caseSensitive || wholeWord || includeContext {
			engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{
				"path": path, "pattern": pattern,
				"case_sensitive": caseSensitive, "whole_word": wholeWord,
				"include_context": includeContext, "context_lines": contextLines,
			}}
			resp, err := engine.AdvancedTextSearch(ctx, engineReq)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resp.Content) > 0 {
				return mcp.NewToolResultText(resp.Content[0].Text), nil
			}
			return mcp.NewToolResultText("No matches"), nil
		}

		// Default: SmartSearch
		engineReq := localmcp.CallToolRequest{Arguments: map[string]interface{}{
			"path": path, "pattern": pattern,
			"include_content": includeContent, "file_types": fileTypes,
		}}
		resp, err := engine.SmartSearch(ctx, engineReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Content) > 0 {
			return mcp.NewToolResultText(resp.Content[0].Text), nil
		}
		return mcp.NewToolResultText("No matches"), nil
	})
	s.AddTool(searchFilesTool, searchFilesHandler)

	// ============================================================================
	// 6. analyze_operation — Analyze operations (Plan Mode / dry-run)
	// ============================================================================
	analyzeOpTool := mcp.NewTool("analyze_operation",
		mcp.WithTitleAnnotation("Analyze Operation"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("Dry-run analysis before executing. Operations: file, optimize, write, edit, delete. "+
			"Other tools: edit_file, multi_edit, search_files, batch_operations, backup, read_file."),
		mcp.WithString("operation", mcp.Required(), mcp.Description("Operation to analyze: file, optimize, write, edit, delete")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithString("content", mcp.Description("Content for write analysis")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced (for edit analysis)")),
		mcp.WithString("new_text", mcp.Description("Replacement text (for edit analysis)")),
	)
	s.AddTool(analyzeOpTool, auditWrap(engine, "analyze_operation", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		operation, err := request.RequireString("operation")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid operation: %v", err)), nil
		}
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		switch operation {
		case "file":
			analysis, err := engine.GetFileAnalysis(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(analysis), nil

		case "optimize":
			suggestion, err := engine.GetOptimizationSuggestion(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			return mcp.NewToolResultText(suggestion), nil

		case "write":
			content := ""
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if c, ok := args["content"].(string); ok {
					content = c
				}
			}
			if content == "" {
				return mcp.NewToolResultError("content parameter is required for write analysis"), nil
			}
			analysis, err := engine.AnalyzeWriteChange(ctx, path, content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
			}
			return mcp.NewToolResultText(formatChangeAnalysis(analysis)), nil

		case "edit":
			oldText := ""
			newText := ""
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if ot, ok := args["old_text"].(string); ok {
					oldText = ot
				}
				if nt, ok := args["new_text"].(string); ok {
					newText = nt
				}
			}
			if oldText == "" {
				return mcp.NewToolResultError("old_text parameter is required for edit analysis"), nil
			}
			analysis, err := engine.AnalyzeEditChange(ctx, path, oldText, newText)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
			}
			return mcp.NewToolResultText(formatChangeAnalysis(analysis)), nil

		case "delete":
			analysis, err := engine.AnalyzeDeleteChange(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
			}
			return mcp.NewToolResultText(formatChangeAnalysis(analysis)), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown operation: %s. Valid: file, optimize, write, edit, delete", operation)), nil
		}
	}))

	// ============================================================================
	// 7. create_directory — Create directory
	// ============================================================================
	createDirTool := mcp.NewTool("create_directory",
		mcp.WithTitleAnnotation("Create Directory"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("Create directories (recursive). Other tools: list_directory, write_file (create files), delete_file, batch_operations, search_files."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the directory to create")),
	)
	s.AddTool(createDirTool, auditWrap(engine, "create_directory", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		return mcp.NewToolResultText(fmt.Sprintf("Successfully created directory: %s", path)), nil
	}))

	// ============================================================================
	// 8. delete_file — Delete file (soft-delete by default, permanent with permanent:true)
	// ============================================================================
	deleteFileTool := mcp.NewTool("delete_file",
		mcp.WithTitleAnnotation("Delete File"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("Delete files or directories. Default: soft-delete (trash). Use permanent:true for hard delete. Other tools: copy_file, move_file, edit_file, batch_operations (bulk delete), backup."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file or directory to delete")),
		mcp.WithBoolean("permanent", mcp.Description("Permanently delete instead of soft-delete (default: false)")),
	)
	s.AddTool(deleteFileTool, auditWrap(engine, "delete_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		permanent := false
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if p, ok := args["permanent"].(bool); ok {
				permanent = p
			}
		}

		if permanent {
			err = engine.DeleteFile(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText(fmt.Sprintf("OK: %s deleted", path)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Successfully deleted: %s", path)), nil
		}

		// Default: soft delete
		err = engine.SoftDeleteFile(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		if engine.IsCompactMode() {
			return mcp.NewToolResultText(fmt.Sprintf("OK: %s soft-deleted", path)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully moved '%s' to filesdelete folder", path)), nil
	}))

	// ============================================================================
	// 9. move_file — Move/rename file (also replaces rename_file)
	// ============================================================================
	moveFileTool := mcp.NewTool("move_file",
		mcp.WithTitleAnnotation("Move / Rename"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("Move or rename files and directories. Other tools: copy_file, delete_file, edit_file (modify content), batch_operations (bulk rename)."),
		mcp.WithString("source_path", mcp.Required(), mcp.Description("Current path of the file/directory")),
		mcp.WithString("dest_path", mcp.Required(), mcp.Description("New path for the file/directory")),
	)
	s.AddTool(moveFileTool, auditWrap(engine, "move_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		return mcp.NewToolResultText(fmt.Sprintf("Successfully moved '%s' to '%s'", sourcePath, destPath)), nil
	}))

	// ============================================================================
	// 10. copy_file — Copy file
	// ============================================================================
	copyFileTool := mcp.NewTool("copy_file",
		mcp.WithTitleAnnotation("Copy File"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("Copy files and directories. Other tools: move_file (rename), delete_file, edit_file (modify content), batch_operations (bulk copy), backup."),
		mcp.WithString("source_path", mcp.Required(), mcp.Description("Path of the file/directory to copy")),
		mcp.WithString("dest_path", mcp.Required(), mcp.Description("Destination path for the copy")),
	)
	s.AddTool(copyFileTool, auditWrap(engine, "copy_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		return mcp.NewToolResultText(fmt.Sprintf("Successfully copied '%s' to '%s'", sourcePath, destPath)), nil
	}))

	// ============================================================================
	// 11. get_file_info — Get file information
	// ============================================================================
	fileInfoTool := mcp.NewTool("get_file_info",
		mcp.WithTitleAnnotation("File Info"),
		mcp.WithDescription("File/directory metadata (size, permissions, dates). Other tools: read_file, edit_file, search_files, list_directory, analyze_operation."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file or directory")),
	)
	s.AddTool(fileInfoTool, auditWrap(engine, "get_file_info", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		info, err := engine.GetFileInfo(ctx, path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
		}
		return mcp.NewToolResultText(info), nil
	}))

	// ============================================================================
	// 12. multi_edit — Multi-edit (unchanged)
	// ============================================================================
	multiEditTool := mcp.NewTool("multi_edit",
		mcp.WithTitleAnnotation("Multi Edit"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("Multiple edits to one file atomically. MUCH faster than calling edit_file multiple times. Auto-backup with undo. "+
			"Other tools: edit_file (single edit), read_file, search_files, batch_operations, backup, analyze_operation."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file to edit")),
		mcp.WithString("edits_json", mcp.Required(), mcp.Description("JSON array of edits: [{\"old_text\": \"...\", \"new_text\": \"...\"}, ...]. Also accepts old_str/new_str as aliases.")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if CRITICAL risk (default: false)")),
	)
	s.AddTool(multiEditTool, auditWrap(engine, "multi_edit", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Accept edits_json as string (normal) or as raw JSON array (Claude Desktop sometimes sends this)
		var edits []core.MultiEditOperation
		args, _ := request.Params.Arguments.(map[string]interface{})

		editsJSON, strErr := request.RequireString("edits_json")
		if strErr == nil {
			// Bug #26: Claude Desktop sends literal newlines inside JSON string values.
			// json.Unmarshal rejects raw \n in strings — fix by escaping them.
			// We only escape newlines that are NOT already escaped (not preceded by \).
			sanitized := editsJSON
			{
				var buf strings.Builder
				inString := false
				escaped := false
				for i := 0; i < len(sanitized); i++ {
					ch := sanitized[i]
					if escaped {
						buf.WriteByte(ch)
						escaped = false
						continue
					}
					if ch == '\\' && inString {
						buf.WriteByte(ch)
						escaped = true
						continue
					}
					if ch == '"' {
						inString = !inString
					}
					if ch == '\n' && inString {
						buf.WriteString("\\n")
						continue
					}
					if ch == '\r' && inString {
						buf.WriteString("\\r")
						continue
					}
					if ch == '\t' && inString {
						buf.WriteString("\\t")
						continue
					}
					buf.WriteByte(ch)
				}
				sanitized = buf.String()
			}
			if err := json.Unmarshal([]byte(sanitized), &edits); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid edits JSON: %v", err)), nil
			}
		} else if args != nil {
			// Defense-in-depth: normalizer should convert raw arrays to JSON string,
			// but keep fallback for edge cases
			if rawEdits, ok := args["edits_json"]; ok {
				rawBytes, err := json.Marshal(rawEdits)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Invalid edits_json: %v", err)), nil
				}
				if err := json.Unmarshal(rawBytes, &edits); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Invalid edits JSON: %v", err)), nil
				}
			} else {
				return mcp.NewToolResultError("edits_json is required"), nil
			}
		} else {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid edits_json: %v", strErr)), nil
		}

		if len(edits) == 0 {
			return mcp.NewToolResultError("edits array cannot be empty"), nil
		}

		// Extract force parameter
		force := false
		if args != nil {
			if f, ok := args["force"].(bool); ok {
				force = f
			}
		}

		// Execute multi-edit
		result, err := engine.MultiEdit(ctx, path, edits, force)
		if err != nil {
			// Bug #27: If result is non-nil, this is an atomic rollback — include backup_id and details
			if result != nil && result.BackupID != "" {
				errMsg := fmt.Sprintf("Multi-edit ROLLED BACK (file unchanged): %v\n", err)
				errMsg += fmt.Sprintf("Applied: %d, Failed: %d, Skipped: %d of %d total\n",
					result.SuccessfulEdits, result.FailedEdits, result.SkippedEdits, result.TotalEdits)
				for _, detail := range result.EditDetails {
					switch detail.Status {
					case core.EditStatusApplied:
						errMsg += fmt.Sprintf("  edit %d: would apply (rolled back)\n", detail.Index+1)
					case core.EditStatusFailed:
						errMsg += fmt.Sprintf("  edit %d: FAILED — %s\n", detail.Index+1, detail.Error)
					case core.EditStatusAlreadyPresent:
						errMsg += fmt.Sprintf("  edit %d: already present\n", detail.Index+1)
					}
				}
				errMsg += fmt.Sprintf("Backup: %s (original file is safe)\n", result.BackupID)
				errMsg += "Fix the failing old_text and retry. Use read_file to get exact text."
				return mcp.NewToolResultError(errMsg), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("Multi-edit error: %v", err)), nil
		}

		// Format result (Bug #17: added SkippedEdits and EditDetails)
		if engine.IsCompactMode() {
			msg := ""
			applied := result.SuccessfulEdits
			skipped := result.SkippedEdits
			failed := result.FailedEdits
			total := result.TotalEdits

			if failed > 0 {
				msg = fmt.Sprintf("OK: %d/%d edits, %d lines", applied+skipped, total, result.LinesAffected)
			} else if skipped > 0 {
				msg = fmt.Sprintf("OK: %d edits (%d applied, %d already present), %d lines",
					total, applied, skipped, result.LinesAffected)
			} else {
				msg = fmt.Sprintf("OK: %d edits, %d lines", applied, result.LinesAffected)
			}
			if result.BackupID != "" {
				msg += fmt.Sprintf(" [backup:%s | UNDO: backup(action:\"restore\", backup_id:\"%s\")]", result.BackupID, result.BackupID)
			}
			if result.RiskWarning != "" {
				msg += result.RiskWarning
			}
			return mcp.NewToolResultText(msg), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Multi-edit completed on %s\n", path))
		sb.WriteString(fmt.Sprintf("Total edits: %d\n", result.TotalEdits))
		sb.WriteString(fmt.Sprintf("Applied: %d\n", result.SuccessfulEdits))
		if result.SkippedEdits > 0 {
			sb.WriteString(fmt.Sprintf("Already present: %d\n", result.SkippedEdits))
		}
		if result.FailedEdits > 0 {
			sb.WriteString(fmt.Sprintf("Failed: %d\n", result.FailedEdits))
		}
		sb.WriteString(fmt.Sprintf("Lines affected: %d\n", result.LinesAffected))
		sb.WriteString(fmt.Sprintf("Confidence: %s\n", result.MatchConfidence))
		if result.BackupID != "" {
			sb.WriteString(fmt.Sprintf("Backup ID: %s\nUNDO: backup(action:\"restore\", backup_id:\"%s\")\n", result.BackupID, result.BackupID))
		}

		if len(result.EditDetails) > 0 {
			sb.WriteString("\nEdit details:\n")
			for _, detail := range result.EditDetails {
				switch detail.Status {
				case core.EditStatusApplied:
					sb.WriteString(fmt.Sprintf("  edit %d: applied (confidence: %s)\n", detail.Index+1, detail.MatchConfidence))
				case core.EditStatusAlreadyPresent:
					sb.WriteString(fmt.Sprintf("  edit %d: already present (subsumed by prior edit)\n", detail.Index+1))
				case core.EditStatusFailed:
					sb.WriteString(fmt.Sprintf("  edit %d: FAILED - %s\n", detail.Index+1, detail.Error))
				}
			}
		}

		if len(result.Errors) > 0 {
			sb.WriteString("\nErrors:\n")
			for _, errMsg := range result.Errors {
				sb.WriteString(fmt.Sprintf("  - %s\n", errMsg))
			}
		}

		if result.RiskWarning != "" {
			sb.WriteString(result.RiskWarning)
		}

		return mcp.NewToolResultText(sb.String()), nil
	}))

	// ============================================================================
	// 13. batch_operations — Batch operations (enhanced: + pipeline_json + rename_json)
	// ============================================================================
	batchOpsTool := mcp.NewTool("batch_operations",
		mcp.WithTitleAnnotation("Batch Operations"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("Bulk file operations, pipelines, and batch rename in one call. Use request_json, pipeline_json, or rename_json. "+
			"Other tools: edit_file (single edit), multi_edit (multiple edits one file), search_files, backup, analyze_operation, copy_file, move_file, delete_file."),
		mcp.WithString("request_json", mcp.Description("JSON with operations array and options. Fields: operations (array), atomic (bool), create_backup (bool), validate_only (bool)")),
		mcp.WithString("pipeline_json", mcp.Description("JSON-encoded pipeline definition with name, steps, and optional flags (dry_run, force, stop_on_error, create_backup, verbose, parallel)")),
		mcp.WithString("rename_json", mcp.Description("JSON with batch rename parameters. Fields: path, mode, find, replace, prefix, suffix, pattern, extension, start_number, padding, recursive, file_pattern, preview, case_sensitive")),
	)
	s.AddTool(batchOpsTool, auditWrap(engine, "batch_operations", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pipelineJSON := ""
		renameJSON := ""
		requestJSON := ""

		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if pj, ok := args["pipeline_json"].(string); ok {
				pipelineJSON = pj
			}
			if rj, ok := args["rename_json"].(string); ok {
				renameJSON = rj
			}
			if rq, ok := args["request_json"].(string); ok {
				requestJSON = rq
			}
		}

		// If pipeline_json is provided, dispatch to pipeline executor
		if pipelineJSON != "" {
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

			if !result.Success {
				return mcp.NewToolResultError(responseText), nil
			}

			return mcp.NewToolResultText(responseText), nil
		}

		// If rename_json is provided, dispatch to batch rename
		if renameJSON != "" {
			var batchRenameReq core.BatchRenameRequest
			if err := json.Unmarshal([]byte(renameJSON), &batchRenameReq); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid rename JSON: %v", err)), nil
			}

			result, err := engine.BatchRenameFiles(ctx, batchRenameReq)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Batch rename error: %v", err)), nil
			}

			resultText := core.FormatBatchRenameResult(result, engine.IsCompactMode())
			if !result.Success && !result.Preview {
				return mcp.NewToolResultError(resultText), nil
			}
			return mcp.NewToolResultText(resultText), nil
		}

		// Default: existing batch operations via request_json
		if requestJSON == "" {
			return mcp.NewToolResultError("One of request_json, pipeline_json, or rename_json is required"), nil
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
		batchManager.SetEngine(engine)
		result := batchManager.ExecuteBatch(batchReq)

		// Format result
		resultText := formatBatchResult(result)

		if !result.Success {
			return mcp.NewToolResultError(resultText), nil
		}

		return mcp.NewToolResultText(resultText), nil
	}))

	// ============================================================================
	// 14. backup — Backup and recovery (enhanced: + restore action)
	// ============================================================================
	backupTool := mcp.NewTool("backup",
		mcp.WithTitleAnnotation("Backup & Restore"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("Manage backups. Actions: list, info, compare, cleanup, restore, undo_last. Auto-created before every edit. "+
			"Other tools: edit_file (auto-backup), multi_edit, batch_operations, search_files, read_file, analyze_operation."),
		mcp.WithString("action", mcp.Description("Action: list (default), info, compare, cleanup, restore, undo_last")),
		mcp.WithString("backup_id", mcp.Description("Backup ID (required for info, compare, restore)")),
		mcp.WithString("file_path", mcp.Description("File path for compare or selective restore")),
		mcp.WithNumber("limit", mcp.Description("Max backups to return for list (default: 20)")),
		mcp.WithString("filter_operation", mcp.Description("Filter by operation: edit, delete, batch, all")),
		mcp.WithString("filter_path", mcp.Description("Filter by file path (substring match)")),
		mcp.WithNumber("newer_than_hours", mcp.Description("Only backups newer than N hours")),
		mcp.WithNumber("older_than_days", mcp.Description("For cleanup: delete backups older than N days (default: 7)")),
		mcp.WithBoolean("dry_run", mcp.Description("For cleanup/restore: preview without executing (default: true for cleanup, false for restore)")),
		mcp.WithBoolean("preview", mcp.Description("For restore: show diff without restoring (default: false)")),
	)
	s.AddTool(backupTool, auditWrap(engine, "backup", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if engine.GetBackupManager() == nil {
			return mcp.NewToolResultError("Backup system not available"), nil
		}

		action := "list"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if a, ok := args["action"].(string); ok && a != "" {
				action = a
			}
		}

		switch action {
		case "list":
			limit := 20
			filterOp := "all"
			filterPath := ""
			newerThan := 0

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
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
			}

			backups, err := engine.GetBackupManager().ListBackups(limit, filterOp, filterPath, newerThan)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to list backups: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("Available Backups (%d)\n", len(backups)))
			output.WriteString("---\n\n")

			for _, backup := range backups {
				output.WriteString(fmt.Sprintf("# %s\n", backup.BackupID))
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
				output.WriteString("Use backup(action:\"restore\", backup_id:\"...\") to restore files\n")
				output.WriteString("Use backup(action:\"info\", backup_id:\"...\") for detailed information\n")
			}

			return mcp.NewToolResultText(output.String()), nil

		case "info":
			backupID, err := request.RequireString("backup_id")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("backup_id is required: %v", err)), nil
			}

			info, err := engine.GetBackupManager().GetBackupInfo(backupID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to get backup info: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("Backup Details: %s\n", info.BackupID))
			output.WriteString("---\n\n")
			output.WriteString(fmt.Sprintf("Timestamp: %s (%s)\n", info.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(info.Timestamp)))
			output.WriteString(fmt.Sprintf("Operation: %s\n", info.Operation))
			if info.UserContext != "" {
				output.WriteString(fmt.Sprintf("Context: %s\n", info.UserContext))
			}
			output.WriteString(fmt.Sprintf("Total Size: %s\n", core.FormatSize(info.TotalSize)))
			output.WriteString(fmt.Sprintf("Files: %d\n\n", len(info.Files)))

			output.WriteString("Files in backup:\n")
			for i, file := range info.Files {
				if i >= 10 {
					output.WriteString(fmt.Sprintf("   ... and %d more files\n", len(info.Files)-10))
					break
				}
				output.WriteString(fmt.Sprintf("   - %s (%s)\n", file.OriginalPath, core.FormatSize(file.Size)))
			}

			output.WriteString(fmt.Sprintf("\nBackup Location: %s\n", engine.GetBackupManager().GetBackupPath(backupID)))

			return mcp.NewToolResultText(output.String()), nil

		case "compare":
			backupID, err := request.RequireString("backup_id")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("backup_id is required: %v", err)), nil
			}

			filePath := ""
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if f, ok := args["file_path"].(string); ok {
					filePath = f
				}
			}
			if filePath == "" {
				return mcp.NewToolResultError("file_path is required for compare action"), nil
			}

			diff, err := engine.GetBackupManager().CompareWithBackup(backupID, filePath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Comparison failed: %v", err)), nil
			}

			return mcp.NewToolResultText(diff), nil

		case "cleanup":
			olderThanDays := 7
			dryRun := true

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if d, ok := args["older_than_days"].(float64); ok {
					olderThanDays = int(d)
				}
				if dr, ok := args["dry_run"].(bool); ok {
					dryRun = dr
				}
			}

			deletedCount, freedSpace, err := engine.GetBackupManager().CleanupOldBackups(olderThanDays, dryRun)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Cleanup failed: %v", err)), nil
			}

			var output strings.Builder
			if dryRun {
				output.WriteString("Dry Run - Preview of cleanup\n\n")
				output.WriteString(fmt.Sprintf("Would delete: %d backup(s)\n", deletedCount))
				output.WriteString(fmt.Sprintf("Would free: %s\n\n", core.FormatSize(freedSpace)))
				output.WriteString("Run with dry_run: false to actually delete backups\n")
			} else {
				output.WriteString("Cleanup completed\n\n")
				output.WriteString(fmt.Sprintf("Deleted: %d backup(s)\n", deletedCount))
				output.WriteString(fmt.Sprintf("Freed: %s\n", core.FormatSize(freedSpace)))
			}

			return mcp.NewToolResultText(output.String()), nil

		case "restore":
			backupID, err := request.RequireString("backup_id")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("backup_id is required: %v", err)), nil
			}

			filePath := ""
			preview := false
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if f, ok := args["file_path"].(string); ok {
					filePath = f
				}
				if p, ok := args["preview"].(bool); ok {
					preview = p
				}
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

				return mcp.NewToolResultText(fmt.Sprintf("Preview - Changes to be restored:\n\n%s", diff)), nil
			}

			// Actual restore
			restoredFiles, err := engine.GetBackupManager().RestoreBackup(backupID, filePath, true)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to restore: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString("Restore completed successfully\n\n")
			output.WriteString(fmt.Sprintf("Restored %d file(s):\n", len(restoredFiles)))
			for _, file := range restoredFiles {
				output.WriteString(fmt.Sprintf("   - %s\n", file))
			}
			output.WriteString("\nA backup of the current state was created before restoring\n")

			return mcp.NewToolResultText(output.String()), nil

		case "undo_last":
			// Find the most recent backup
			backups, err := engine.GetBackupManager().ListBackups(1, "all", "", 0)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to list backups: %v", err)), nil
			}
			if len(backups) == 0 {
				return mcp.NewToolResultError("No backups found. Nothing to undo."), nil
			}

			lastBackup := backups[0]

			// Check for preview mode
			preview := false
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if p, ok := args["preview"].(bool); ok {
					preview = p
				}
			}

			if preview {
				var output strings.Builder
				output.WriteString(fmt.Sprintf("Preview — Last backup: %s\n", lastBackup.BackupID))
				output.WriteString(fmt.Sprintf("Time: %s (%s)\n", lastBackup.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(lastBackup.Timestamp)))
				output.WriteString(fmt.Sprintf("Operation: %s\n", lastBackup.Operation))
				output.WriteString(fmt.Sprintf("Files: %d\n\n", len(lastBackup.Files)))
				for _, file := range lastBackup.Files {
					output.WriteString(fmt.Sprintf("   - %s\n", file.OriginalPath))
				}
				output.WriteString("\nRun backup(action:\"undo_last\") without preview to restore these files\n")
				return mcp.NewToolResultText(output.String()), nil
			}

			// Restore the last backup
			restoredFiles, err := engine.GetBackupManager().RestoreBackup(lastBackup.BackupID, "", true)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to restore: %v", err)), nil
			}

			var output strings.Builder
			output.WriteString(fmt.Sprintf("UNDO completed — restored backup %s\n\n", lastBackup.BackupID))
			output.WriteString(fmt.Sprintf("Operation undone: %s\n", lastBackup.Operation))
			output.WriteString(fmt.Sprintf("Time of original edit: %s (%s)\n\n", lastBackup.Timestamp.Format("2006-01-02 15:04:05"), core.FormatAge(lastBackup.Timestamp)))
			output.WriteString(fmt.Sprintf("Restored %d file(s):\n", len(restoredFiles)))
			for _, file := range restoredFiles {
				output.WriteString(fmt.Sprintf("   - %s\n", file))
			}
			output.WriteString("\nA backup of the current state was created before restoring\n")
			return mcp.NewToolResultText(output.String()), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown action: %s. Valid: list, info, compare, cleanup, restore, undo_last", action)), nil
		}
	}))

	// ============================================================================
	// 15. wsl — WSL/Windows integration (consolidated: wsl_sync + wsl_status + configure_autosync + autosync_status)
	// ============================================================================
	wslTool := mcp.NewTool("wsl",
		mcp.WithTitleAnnotation("WSL Integration"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("WSL/Windows file sync and path conversion. Actions: sync, status, autosync_config, autosync_status. "+
			"Other tools: read_file, edit_file, copy_file, search_files, batch_operations."),
		mcp.WithString("action", mcp.Description("Action: sync (default), status, autosync_config, autosync_status")),
		// sync params
		mcp.WithString("wsl_path", mcp.Description("Source WSL path for sync")),
		mcp.WithString("windows_path", mcp.Description("Destination or source Windows path for sync")),
		mcp.WithString("direction", mcp.Description("Sync direction: wsl_to_windows, windows_to_wsl, or bidirectional")),
		mcp.WithBoolean("create_dirs", mcp.Description("Create destination directories (default: true)")),
		mcp.WithString("filter_pattern", mcp.Description("Optional file filter pattern for workspace sync")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview changes without executing (default: false)")),
		// autosync_config params
		mcp.WithBoolean("enabled", mcp.Description("Enable/disable auto-sync (for autosync_config)")),
		mcp.WithBoolean("sync_on_write", mcp.Description("Auto-sync on write operations (default: true)")),
		mcp.WithBoolean("sync_on_edit", mcp.Description("Auto-sync on edit operations (default: true)")),
		mcp.WithBoolean("silent", mcp.Description("Silent mode for auto-sync (default: false)")),
	)
	s.AddTool(wslTool, auditWrap(engine, "wsl", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := "sync"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if a, ok := args["action"].(string); ok && a != "" {
				action = a
			}
		}

		switch action {
		case "status":
			status, err := engine.GetWSLWindowsStatus(ctx)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to get status: %v", err)), nil
			}

			if engine.IsCompactMode() {
				env := status["environment"].(string)
				isWSL := status["is_wsl"].(bool)
				return mcp.NewToolResultText(fmt.Sprintf("Env: %s, WSL: %v", env, isWSL)), nil
			}

			var output strings.Builder
			output.WriteString("WSL / Windows Integration Status\n")
			output.WriteString("---\n\n")

			output.WriteString(fmt.Sprintf("Environment: %s\n", status["environment"]))
			output.WriteString(fmt.Sprintf("Running in WSL: %v\n", status["is_wsl"]))

			if winUser, ok := status["windows_user"].(string); ok && winUser != "" {
				output.WriteString(fmt.Sprintf("Windows User: %s\n", winUser))
			}

			output.WriteString("\nPaths:\n")
			output.WriteString(fmt.Sprintf("  WSL Home: %s\n", status["wsl_home"]))
			if wslWinHome, ok := status["windows_home_wsl_style"].(string); ok && wslWinHome != "" {
				output.WriteString(fmt.Sprintf("  Windows Home (WSL style): %s\n", wslWinHome))
			}
			if winHome, ok := status["windows_home_windows_style"].(string); ok && winHome != "" {
				output.WriteString(fmt.Sprintf("  Windows Home (Windows style): %s\n", winHome))
			}

			output.WriteString(fmt.Sprintf("\nSystem:\n"))
			output.WriteString(fmt.Sprintf("  Path Separator: %s\n", status["path_separator"]))

			if interopAvail, ok := status["windows_interop_available"].(bool); ok {
				output.WriteString(fmt.Sprintf("  Windows Interop: %v\n", interopAvail))
			}

			if dirStatus, ok := status["directory_status"].(map[string]bool); ok && len(dirStatus) > 0 {
				output.WriteString("\nDirectory Status:\n")
				for dir, exists := range dirStatus {
					marker := "NO"
					if exists {
						marker = "OK"
					}
					output.WriteString(fmt.Sprintf("  [%s] %s\n", marker, dir))
				}
			}

			return mcp.NewToolResultText(output.String()), nil

		case "autosync_config":
			enabledVal := false
			hasEnabled := false
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if e, ok := args["enabled"].(bool); ok {
					enabledVal = e
					hasEnabled = true
				}
			}
			if !hasEnabled {
				return mcp.NewToolResultError("enabled parameter is required for autosync_config"), nil
			}

			asCfg := engine.GetAutoSyncConfig()
			asCfg.Enabled = enabledVal

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if syncOnWrite, ok := args["sync_on_write"].(bool); ok {
					asCfg.SyncOnWrite = syncOnWrite
				}
				if syncOnEdit, ok := args["sync_on_edit"].(bool); ok {
					asCfg.SyncOnEdit = syncOnEdit
				}
				if silent, ok := args["silent"].(bool); ok {
					asCfg.Silent = silent
				}
			}

			if err := engine.SetAutoSyncConfig(asCfg); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to configure auto-sync: %v", err)), nil
			}

			isWSL, _ := core.DetectEnvironment()
			if !isWSL && enabledVal {
				return mcp.NewToolResultText("Auto-sync enabled, but not running in WSL. Auto-sync only works in WSL environment."), nil
			}

			if enabledVal {
				if engine.IsCompactMode() {
					return mcp.NewToolResultText("Auto-sync enabled"), nil
				}
				return mcp.NewToolResultText("Auto-sync enabled!\n\nFiles written/edited in WSL will be automatically copied to Windows.\nYou can disable it anytime with: wsl(action:\"autosync_config\", enabled:false)"), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText("Auto-sync disabled"), nil
			}
			return mcp.NewToolResultText("Auto-sync disabled. Files will not be automatically synced."), nil

		case "autosync_status":
			asStatus := engine.GetAutoSyncStatus()

			if engine.IsCompactMode() {
				enabled := asStatus["enabled"].(bool)
				isWSL := asStatus["is_wsl"].(bool)
				return mcp.NewToolResultText(fmt.Sprintf("Enabled: %v, WSL: %v", enabled, isWSL)), nil
			}

			var output strings.Builder
			output.WriteString("Auto-Sync Status\n")
			output.WriteString("---\n\n")

			enabled := asStatus["enabled"].(bool)
			isWSL := asStatus["is_wsl"].(bool)

			if enabled {
				output.WriteString("Status: ENABLED\n")
			} else {
				output.WriteString("Status: DISABLED\n")
			}

			output.WriteString(fmt.Sprintf("Environment: %s\n", map[bool]string{true: "WSL", false: "Native"}[isWSL]))

			if winUser, ok := asStatus["windows_user"].(string); ok && winUser != "" {
				output.WriteString(fmt.Sprintf("Windows User: %s\n", winUser))
			}

			output.WriteString("\nConfiguration:\n")
			output.WriteString(fmt.Sprintf("  Sync on Write: %v\n", asStatus["sync_on_write"]))
			output.WriteString(fmt.Sprintf("  Sync on Edit: %v\n", asStatus["sync_on_edit"]))
			output.WriteString(fmt.Sprintf("  Sync on Delete: %v\n", asStatus["sync_on_delete"]))

			if configPath, ok := asStatus["config_path"].(string); ok && configPath != "" {
				output.WriteString(fmt.Sprintf("\nConfig File: %s\n", configPath))
			}

			if !enabled && isWSL {
				output.WriteString("\nTo enable auto-sync, run:\n")
				output.WriteString("   wsl(action:\"autosync_config\", enabled:true)\n")
			}

			if enabled && !isWSL {
				output.WriteString("\nAuto-sync is enabled but you're not in WSL.\n")
				output.WriteString("   Auto-sync only works when running in WSL environment.\n")
			}

			return mcp.NewToolResultText(output.String()), nil

		default:
			// Default: sync behavior
			// Determine if this is a single file copy or workspace sync
			wslPath := ""
			windowsPath := ""
			direction := ""
			createDirs := true
			filterPattern := ""
			dryRun := false

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if wp, ok := args["wsl_path"].(string); ok {
					wslPath = wp
				}
				if winp, ok := args["windows_path"].(string); ok {
					windowsPath = winp
				}
				if d, ok := args["direction"].(string); ok {
					direction = d
				}
				if cd, ok := args["create_dirs"].(bool); ok {
					createDirs = cd
				}
				if fp, ok := args["filter_pattern"].(string); ok {
					filterPattern = fp
				}
				if dr, ok := args["dry_run"].(bool); ok {
					dryRun = dr
				}
			}

			// Workspace sync mode (when direction is specified)
			if direction != "" {
				syncResult, err := engine.SyncWorkspace(ctx, direction, filterPattern, dryRun)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Sync failed: %v", err)), nil
				}

				if engine.IsCompactMode() {
					syncCount := syncResult["synced_count"].(int)
					errorCount := syncResult["error_count"].(int)
					return mcp.NewToolResultText(fmt.Sprintf("OK: %d files synced, %d errors", syncCount, errorCount)), nil
				}

				var output strings.Builder
				output.WriteString("Workspace Sync Results\n")
				output.WriteString("---\n\n")
				output.WriteString(fmt.Sprintf("Direction: %s\n", syncResult["direction"]))
				if filterPattern != "" {
					output.WriteString(fmt.Sprintf("Filter: %s\n", syncResult["filter_pattern"]))
				}
				if dryRun {
					output.WriteString("Mode: DRY RUN (preview only)\n")
				}
				output.WriteString("\n")

				syncedFiles := syncResult["synced_files"].([]string)
				syncCount := syncResult["synced_count"].(int)
				errorCount := syncResult["error_count"].(int)

				if syncCount > 0 {
					output.WriteString(fmt.Sprintf("Files synced: %d\n", syncCount))
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
					output.WriteString("No files to sync\n")
				}

				if errorCount > 0 {
					syncErrors := syncResult["errors"].([]string)
					output.WriteString(fmt.Sprintf("\nErrors: %d\n", errorCount))
					if errorCount <= 10 {
						for _, errMsg := range syncErrors {
							output.WriteString(fmt.Sprintf("  - %s\n", errMsg))
						}
					} else {
						for i := 0; i < 5; i++ {
							output.WriteString(fmt.Sprintf("  - %s\n", syncErrors[i]))
						}
						output.WriteString(fmt.Sprintf("  ... and %d more errors\n", errorCount-5))
					}
				}

				return mcp.NewToolResultText(output.String()), nil
			}

			// Single file copy mode
			if wslPath != "" {
				// WSL to Windows copy
				err := engine.WSLWindowsCopy(ctx, wslPath, windowsPath, createDirs)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Copy failed: %v", err)), nil
				}
				if windowsPath == "" {
					windowsPath, _ = core.WSLToWindows(wslPath)
				}
				if engine.IsCompactMode() {
					return mcp.NewToolResultText(fmt.Sprintf("OK: Copied to %s", windowsPath)), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Successfully copied from WSL to Windows:\n  Source: %s\n  Destination: %s", wslPath, windowsPath)), nil
			}

			if windowsPath != "" {
				// Windows to WSL copy
				wslDest := ""
				if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
					if wp, ok := args["wsl_path"].(string); ok {
						wslDest = wp
					}
				}
				err := engine.WSLWindowsCopy(ctx, windowsPath, wslDest, createDirs)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Copy failed: %v", err)), nil
				}
				if wslDest == "" {
					wslDest, _ = core.WindowsToWSL(windowsPath)
				}
				if engine.IsCompactMode() {
					return mcp.NewToolResultText(fmt.Sprintf("OK: Copied to %s", wslDest)), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Successfully copied from Windows to WSL:\n  Source: %s\n  Destination: %s", windowsPath, wslDest)), nil
			}

			return mcp.NewToolResultError("For sync: provide wsl_path, windows_path, or direction. Use action:\"status\" for integration status."), nil
		}
	}))

	// ============================================================================
	// 16. server_info — Server info (consolidated: stats + artifact + get_help)
	// ============================================================================
	serverInfoTool := mcp.NewTool("server_info",
		mcp.WithTitleAnnotation("Server Info"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("Server help, stats, and artifacts. action:help (usage topics), action:stats (performance), action:artifact (code capture). "+
			"Other tools: edit_file, multi_edit, search_files, batch_operations, backup, read_file, analyze_operation."),
		mcp.WithString("action", mcp.Description("Action: help (default), stats, artifact")),
		// help params
		mcp.WithString("topic", mcp.Description("Help topic: overview, workflow, tools, read, write, edit, search, batch, errors, examples, tips, all")),
		// artifact params
		mcp.WithString("sub_action", mcp.Description("For artifact: capture, write, info")),
		mcp.WithString("content", mcp.Description("Artifact content to capture")),
		mcp.WithString("path", mcp.Description("Path for writing artifact")),
	)
	s.AddTool(serverInfoTool, auditWrap(engine, "server_info", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := "help"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if a, ok := args["action"].(string); ok && a != "" {
				action = a
			}
		}

		switch action {
		case "stats":
			stats := engine.GetPerformanceStats()

			// Also include edit telemetry
			summary := engine.GetEditTelemetrySummary()
			data, _ := json.MarshalIndent(summary, "", "  ")
			telemetry := fmt.Sprintf("\n\nEdit Telemetry:\n%s", string(data))

			// Include backup system info
			backupInfo := ""
			if bm := engine.GetBackupManager(); bm != nil {
				backups, _ := bm.ListBackups(1, "all", "", 0)
				maxCount, maxAge := bm.GetBackupLimits()
				backupInfo = fmt.Sprintf("\n\nBackup System:\n  Directory: %s\n  Max count: %d\n  Max age: %d days",
					bm.GetBackupDir(), maxCount, maxAge)
				if len(backups) > 0 {
					backupInfo += fmt.Sprintf("\n  Latest: %s (%s, %s)",
						backups[0].BackupID, backups[0].Operation, core.FormatAge(backups[0].Timestamp))
				}
				allBackups, _ := bm.ListBackups(9999, "all", "", 0)
				backupInfo += fmt.Sprintf("\n  Total backups: %d", len(allBackups))
				backupInfo += fmt.Sprintf("\n  UNDO last edit: backup(action:\"undo_last\")")
			}

			return mcp.NewToolResultText(stats + telemetry + backupInfo), nil

		case "artifact":
			subAction := "info"
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if sa, ok := args["sub_action"].(string); ok && sa != "" {
					subAction = sa
				}
			}

			switch subAction {
			case "capture":
				content := ""
				if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
					if c, ok := args["content"].(string); ok {
						content = c
					}
				}
				if content == "" {
					return mcp.NewToolResultError("content parameter is required for artifact capture"), nil
				}

				err := engine.CaptureLastArtifact(ctx, content)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
				}

				lines := strings.Count(content, "\n") + 1
				return mcp.NewToolResultText(fmt.Sprintf("Captured artifact: %d bytes, %d lines", len(content), lines)), nil

			case "write":
				artifactPath := ""
				if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
					if p, ok := args["path"].(string); ok {
						artifactPath = p
					}
				}
				if artifactPath == "" {
					return mcp.NewToolResultError("path parameter is required for artifact write"), nil
				}

				err := engine.WriteLastArtifact(ctx, artifactPath)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Wrote last artifact to: %s", artifactPath)), nil

			case "info":
				info := engine.GetLastArtifactInfo()
				return mcp.NewToolResultText(info), nil

			default:
				return mcp.NewToolResultError(fmt.Sprintf("Unknown sub_action: %s. Valid: capture, write, info", subAction)), nil
			}

		case "help":
			topic := "overview"
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if t, ok := args["topic"].(string); ok && t != "" {
					topic = strings.ToLower(t)
				}
			}

			help := getHelpContent(topic, engine.IsCompactMode())
			return mcp.NewToolResultText(help), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown action: %s. Valid: help, stats, artifact", action)), nil
		}
	}))

	// ============================================================================
	// Official MCP server compatibility aliases
	// For clients trained on modelcontextprotocol/servers filesystem
	// ============================================================================

	s.AddTool(mcp.NewTool("read_text_file",
		mcp.WithTitleAnnotation("Read File (alias)"),
		mcp.WithDescription("Read file contents (alias for read_file). To MODIFY this file use edit_file or the edit alias. Other tools: search_files, multi_edit, write_file, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file")),
		mcp.WithNumber("start_line", mcp.Description("Starting line number (1-indexed)")),
		mcp.WithNumber("end_line", mcp.Description("Ending line number (inclusive)")),
	), readFileHandler)

	s.AddTool(mcp.NewTool("search",
		mcp.WithTitleAnnotation("Search (alias)"),
		mcp.WithDescription("Search files by name or content with regex support (alias for search_files). To edit found files use edit_file or the edit alias. Other tools: read_file, multi_edit, batch_operations."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Base directory")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
		mcp.WithBoolean("include_content", mcp.Description("Include file content search")),
		mcp.WithString("file_types", mcp.Description("Comma-separated extensions")),
	), searchFilesHandler)

	s.AddTool(mcp.NewTool("edit",
		mcp.WithTitleAnnotation("Edit File (alias)"),
		mcp.WithDescription("Edit and modify existing files — search-and-replace text in files with auto-backup. Supports exact match, regex, and occurrence-based replacement. Use this instead of write_file to change existing files."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file")),
		mcp.WithString("old_text", mcp.Description("Text to be replaced")),
		mcp.WithString("new_text", mcp.Description("New text to replace with")),
		mcp.WithString("old_str", mcp.Description("Alias for old_text")),
		mcp.WithString("new_str", mcp.Description("Alias for new_text")),
		mcp.WithString("mode", mcp.Description("Edit mode: \"replace\" (default), \"search_replace\", \"regex\"")),
		mcp.WithNumber("occurrence", mcp.Description("Which occurrence to replace")),
		mcp.WithBoolean("force", mcp.Description("Force operation even if CRITICAL risk")),
	), editFileHandler)

	s.AddTool(mcp.NewTool("write",
		mcp.WithTitleAnnotation("Write File (alias)"),
		mcp.WithDescription("Write content to a file — create new files or overwrite existing ones. Supports text and base64 binary. For modifying existing files prefer edit_file or the edit alias instead."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path where to write")),
		mcp.WithString("content", mcp.Description("Text content to write")),
		mcp.WithString("content_base64", mcp.Description("Base64-encoded binary content")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" for base64 content")),
	), writeFileHandler)

	s.AddTool(mcp.NewTool("create_file",
		mcp.WithTitleAnnotation("Create File (alias)"),
		mcp.WithDescription("Create a new file with content (alias for write_file). For modifying existing files use edit_file or the edit alias instead."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path for the new file")),
		mcp.WithString("content", mcp.Description("Text content to write")),
		mcp.WithString("content_base64", mcp.Description("Base64-encoded binary content")),
		mcp.WithString("encoding", mcp.Description("Set to \"base64\" for base64 content")),
	), writeFileHandler)

	s.AddTool(mcp.NewTool("directory_tree",
		mcp.WithTitleAnnotation("Directory Tree (alias)"),
		mcp.WithDescription("Alias for list_directory — compatibility with the official MCP filesystem server."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to directory")),
	), listDirHandler)

	// ============================================================================
	// 17. help — Standalone help for Claude Desktop tool discovery
	// ============================================================================
	helpTool := mcp.NewTool("help",
		mcp.WithTitleAnnotation("Server Help"),
		mcp.WithDescription("IMPORTANT: Call this tool FIRST in every conversation to discover ALL 16 tools. "+
			"Covers: edit_file (modify/change/replace text), write_file (create), read_file, multi_edit, search_files, "+
			"list_directory, copy_file, move_file, delete_file, create_directory, get_file_info, batch_operations, "+
			"backup (restore/undo), analyze_operation, wsl, server_info. Without calling help you will miss most tools."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
	s.AddTool(helpTool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(serverInstructions), nil
	})

	log.Printf("Registered 23 tools for v4.1.3")

	return nil
}

// getHelpContent returns help content for the specified topic
func getHelpContent(topic string, compactMode bool) string {
	var sb strings.Builder

	switch topic {
	case "overview":
		sb.WriteString(`# MCP Filesystem Ultra v4.1.3 - Quick Start

## CRITICAL RULE
Always use MCP tools (read_file, write_file, edit_file) instead of native file tools.
These auto-convert paths between WSL (/mnt/c/) and Windows (C:\).

## THE GOLDEN RULE
Surgical edits save 98% tokens:
BAD:  read_file(large) -> write_file(large) = 250k tokens
GOOD: search_files -> read_file(start_line/end_line) -> edit_file = 2k tokens

## AVAILABLE TOPICS
Call server_info(topic) with:
- "workflow" - The 4-step efficient workflow
- "tools"    - Complete list of 16 tools
- "read"     - Reading files efficiently
- "write"    - Writing and creating files
- "edit"     - Editing files (most important!)
- "search"   - Finding content in files
- "batch"    - Multiple operations at once
- "errors"   - Common errors and fixes
- "examples" - Code examples
- "tips"     - Pro tips for efficiency
- "recovery" - Disaster recovery from bad edits
- "all"      - Everything (long output)
`)

	case "workflow":
		sb.WriteString(`# THE 4-STEP EFFICIENT WORKFLOW

Use this workflow for ANY file >1000 lines:

## Step 1: LOCATE
search_files(file, "function_name")
-> Returns: "Found at lines 45-67"
-> Cost: ~500 tokens

## Step 2: READ (Only what you need)
read_file(file, start_line=45, end_line=67)
-> Returns: Only those 22 lines
-> Cost: ~1000 tokens

## Step 3: EDIT (Surgically)
edit_file(file, "old_text", "new_text")
-> Returns: "OK: 1 changes"
-> Cost: ~500 tokens

## Step 4: VERIFY (Optional)
server_info(action:"stats")
-> Goal: >80% targeted_edits

## FILE SIZE RULES
<1000 lines  -> read_file() is OK
1000-5000    -> MUST use this workflow
>5000 lines  -> CRITICAL - never read entire file

## TOTAL COST: ~2k tokens vs 250k (98% savings!)
`)

	case "tools":
		sb.WriteString(`# COMPLETE TOOL LIST (16 Tools)

## Core (5)
read_file      - Read file (supports line ranges, base64, head/tail)
write_file     - Atomic write (supports text and base64 binary)
edit_file      - Smart edit with modes: replace, search_replace, regex
list_directory - Cached directory listing
search_files   - Search files/content (supports count_only mode)

## Edit+ (1)
multi_edit     - Multiple edits to one file atomically

## Files (4)
move_file        - Move or rename file/directory
copy_file        - Copy file/directory
delete_file      - Delete (soft by default, permanent:true for hard)
create_directory - Create directory with parents

## Analysis (2)
get_file_info     - File/directory information
analyze_operation - Dry-run analysis (file, optimize, write, edit, delete)

## Batch (1)
batch_operations  - Batch ops, pipelines, and batch rename

## Backup (1)
backup           - List, info, compare, cleanup, restore backups

## WSL (1)
wsl              - WSL/Windows sync, status, autosync config

## Info (1)
server_info      - Help, stats, artifact management
`)

	case "read":
		sb.WriteString(`# READING FILES EFFICIENTLY

## Quick Reference
| File Size    | How to Read                    |
|--------------|-------------------------------|
| <1000 lines  | read_file(path)               |
| >1000 lines  | read_file(path, start_line=N, end_line=M) |
| Binary file  | read_file(path, encoding:"base64") |

## Best Practice: Line Range
# Read only lines 100-150 of a large file
read_file(path, start_line=100, end_line=150)

## Why This Matters
5000-line file:
- read_file: ~125,000 tokens
- read_file with start_line/end_line (50 lines): ~2,500 tokens
- Savings: 98%!

## Workflow
1. search_files(file, pattern) -> find line numbers
2. read_file(file, start_line=N, end_line=M) -> read only those
3. Never read more than you need!
`)

	case "write":
		sb.WriteString(`# WRITING FILES

## Quick Reference
| Task           | How                          |
|----------------|------------------------------|
| Create file    | write_file(path, content)    |
| Overwrite file | write_file(path, content)    |
| Binary file    | write_file(path, content_base64="...") |

## Examples

# Create or overwrite a file
write_file("/path/to/file.txt", content="content here")

# Write binary from base64
write_file("/path/to/image.png", content_base64="iVBOR...")

## IMPORTANT
- write_file OVERWRITES the entire file
- For small changes, use edit_file instead!
- write_file also creates parent directories automatically

## Path Handling
All tools auto-convert paths:
/mnt/c/Users/... -> C:\Users\... (on Windows)
C:\Users\... -> /mnt/c/Users/... (on WSL)
`)

	case "edit":
		sb.WriteString(`# EDITING FILES (MOST IMPORTANT!)

## THE GOLDEN RULE
Use edit_file for changes, NOT write_file!

## Quick Reference
| Task                    | How                                          |
|-------------------------|----------------------------------------------|
| Single replacement      | edit_file(path, old_text, new_text)          |
| Multiple replacements   | multi_edit(path, edits_json)                 |
| Replace specific match  | edit_file(path, old_text, new_text, occurrence=N) |
| Regex transformation    | edit_file(path, mode:"regex", patterns_json) |
| Search & replace all    | edit_file(path, mode:"search_replace", pattern, replacement) |

## Examples

# Simple edit
edit_file("/path/file.py", old_text="old_function()", new_text="new_function()")

# Multiple edits in one file (EFFICIENT!)
multi_edit("/path/file.py", edits_json='[
  {"old_text": "foo", "new_text": "bar"},
  {"old_text": "baz", "new_text": "qux"}
]')

# Replace only the LAST occurrence
edit_file("/path/file.py", old_text="TODO", new_text="DONE", occurrence=-1)
# occurrence: 1=first, 2=second, -1=last, -2=second-to-last

## COMMON ERRORS

"no match found"
-> Text doesn't exist exactly. Check whitespace!
-> Use search_files first to verify

"context validation failed"
-> File changed since you read it
-> Re-run search_files + read_file with start_line/end_line

"multiple matches found"
-> Use occurrence=N to target specific one

## PRO TIP
For files >1000 lines, ALWAYS:
1. search_files first
2. read_file with start_line/end_line to see context
3. edit_file with exact text
`)

	case "search":
		sb.WriteString(`# SEARCHING FILES

## Quick Reference
| Task                  | How                                          |
|-----------------------|----------------------------------------------|
| Find location         | search_files(path, pattern)                  |
| Count matches         | search_files(path, pattern, count_only=true) |
| Search with context   | search_files(path, pattern, include_context=true) |
| Find and replace all  | edit_file(path, mode:"search_replace", pattern, replacement) |

## Examples

# Find where a function is defined
search_files("/path/to/project", "def my_function")
-> Returns: "Found at lines 45-67 in file.py"

# Count how many TODOs exist
search_files("/path/file.py", "TODO", count_only=true)
-> Returns: "15 matches at lines: 10, 25, 48, ..."

# Search with surrounding context
search_files("/path", "error", include_context=true, context_lines=3)

# Replace all occurrences in multiple files
edit_file("/path/to/project", mode:"search_replace", pattern:"old_name", replacement:"new_name")

## WORKFLOW TIP
Always search BEFORE editing large files:
1. search_files -> find exact location
2. read_file with start_line/end_line -> see the context
3. edit_file -> make the change
`)

	case "batch":
		sb.WriteString(`# BATCH OPERATIONS

## When to Use
- Multiple file operations that should succeed or fail together
- Creating multiple files at once
- Complex refactoring across files
- Pipeline transformations

## Example: Batch Operations
batch_operations(request_json='{
  "operations": [
    {"type": "write", "path": "file1.txt", "content": "..."},
    {"type": "write", "path": "file2.txt", "content": "..."},
    {"type": "copy", "source": "file1.txt", "destination": "backup.txt"},
    {"type": "edit", "path": "file3.txt", "old_text": "x", "new_text": "y"}
  ],
  "atomic": true,
  "create_backup": true
}')

## Example: Pipeline
batch_operations(pipeline_json='{
  "name": "refactor",
  "steps": [
    {"id": "find", "action": "search", "params": {"pattern": "old"}},
    {"id": "fix", "action": "edit", "input_from": "find", "params": {"old_text": "old", "new_text": "new"}}
  ]
}')

## Example: Batch Rename
batch_operations(rename_json='{
  "path": "/dir", "mode": "find_replace",
  "find": "old", "replace": "new", "preview": true
}')

## Batch Operation Types
- write, edit, copy, move, delete, create_directory

## Pipeline Actions
- search, read_ranges, count_occurrences, edit, multi_edit
- regex_transform, copy, rename, delete, aggregate, diff, merge

## Options
- atomic: true = All succeed or all rollback
- create_backup: true = Backup before changes
- validate_only: true = Dry run (no changes)
`)

	case "errors":
		sb.WriteString(`# COMMON ERRORS AND FIXES

## "no match found for old_text"
CAUSE: The exact text doesn't exist in the file
FIXES:
1. Use search_files to verify the text exists
2. Check for whitespace/indentation differences
3. Copy the EXACT text from read_file output

## "context validation failed"
CAUSE: File was modified since you read it
FIX: Re-run search_files + read_file with start_line/end_line to get fresh content

## "multiple matches found"
CAUSE: Same text appears multiple times
FIX: Use edit_file with occurrence=N:
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
FIX: All tools auto-convert paths:
- /mnt/c/Users/... <-> C:\Users\...

## "Tool not found: create_file"
FIX: Use write_file instead (it creates files too)
`)

	case "examples":
		sb.WriteString(`# PRACTICAL EXAMPLES

## Example 1: Edit a function in a large file
# Step 1: Find where the function is
search_files("src/app.py", "def calculate_total")
# -> "Found at lines 234-256"

# Step 2: Read only those lines
read_file("src/app.py", start_line=234, end_line=256)

# Step 3: Make the edit
edit_file("src/app.py",
  old_text="def calculate_total(items):",
  new_text="def calculate_total(items, tax_rate=0.1):")

## Example 2: Multiple edits in one file
multi_edit("src/config.py", edits_json='[
  {"old_text": "DEBUG = True", "new_text": "DEBUG = False"},
  {"old_text": "VERSION = \"1.0\"", "new_text": "VERSION = \"1.1\""},
  {"old_text": "API_URL = \"http://dev\"", "new_text": "API_URL = \"http://prod\""}
]')

## Example 3: Replace only the last TODO
edit_file("src/main.py", old_text="TODO", new_text="DONE", occurrence=-1)

## Example 4: Create multiple files atomically
batch_operations(request_json='{
  "operations": [
    {"type": "create_directory", "path": "src/components"},
    {"type": "write", "path": "src/components/Button.tsx", "content": "..."},
    {"type": "write", "path": "src/components/Input.tsx", "content": "..."}
  ],
  "atomic": true
}')

## Example 5: Count before replacing
# First, see how many matches
search_files("src/legacy.py", "old_api_call", count_only=true)
# -> "47 matches"

# If too many, be more specific or use occurrence=N
`)

	case "tips":
		sb.WriteString(`# PRO TIPS FOR EFFICIENCY

## 1. Never Read Large Files Entirely
GOOD: search_files -> read_file(start_line, end_line) -> edit_file
BAD:  read_file on 5000-line files (wastes 125k tokens!)

## 2. Use multi_edit for Multiple Changes
GOOD: One multi_edit call with 5 edits
BAD:  Five separate edit_file calls (5x slower)

## 3. Search Before Editing
GOOD: search_files first, then edit
BAD:  Guessing line numbers or text

## 4. Use count_only Before Bulk Replace
GOOD: search_files(count_only=true) to check how many matches first
BAD:  Blind replace that affects unexpected locations

## 5. Check Your Efficiency
server_info(action:"stats")
-> Goal: >80% targeted_edits
-> If <50%, you're not using the workflow correctly

## 6. Use occurrence for Precision
GOOD: edit_file with occurrence=1 or occurrence=-1
BAD:  edit_file when there are multiple matches

## 7. Batch Operations for Atomicity
GOOD: batch_operations with atomic=true
BAD:  Multiple operations that could partially fail

## 8. Dry Run Before Destructive Operations
GOOD: analyze_operation(operation:"edit") first
BAD:  delete_file without checking

## 9. Monitor Performance
server_info(action:"stats") -> See cache hit rate, ops/sec
-> Goal: >95% cache hit rate

## 10. Use Regex for Complex Transforms
edit_file(mode:"regex", patterns_json='[{"pattern":"(\\w+)Error","replacement":"${1}Exception"}]')
`)

	case "recovery":
		sb.WriteString(`# DISASTER RECOVERY

## Quick Undo (last edit)
backup(action:"undo_last")
-> Restores the most recent backup automatically
-> Use preview:true first to see what will be restored

## Undo Specific Edit
Every edit_file/multi_edit response includes:
  UNDO: backup(action:"restore", backup_id:"...")
Copy that command to restore.

## Find Backups for a File
backup(action:"list", filter_path:"filename.cs")
-> Shows all backups containing that file
-> Then: backup(action:"restore", backup_id:"...")

## Compare Before Restoring
backup(action:"compare", backup_id:"...", file_path:"path/to/file")
-> Shows diff between backup and current file

## Check System Status
server_info(action:"stats")
-> Shows backup directory, count, and latest backup

## Before Repairing Damage
1. backup(action:"list", filter_path:"broken-file") -> find clean backup
2. copy_file(source, "/tmp/manual-backup") -> extra safety copy
3. get_file_info(path) -> note file size as reference
4. search_files(path, pattern, count_only:true) -> count key elements
5. THEN start fixing

## Known Issues
- read_file with only start_line (no end_line) now reads to end of file (fixed in v4.1.2)
- If file seems truncated, verify with mode:"tail" before editing
- If >15% of file changes in one edit, STOP and verify

## Golden Rule
If edits make things WORSE, STOP editing and RESTORE from backup.
Repeated edits on a broken file make recovery harder.
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
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("recovery", compactMode))

	default:
		sb.WriteString(fmt.Sprintf(`# Unknown topic: "%s"

Available topics:
- overview  - Quick start guide
- workflow  - The 4-step efficient workflow
- tools     - Complete list of 16 tools
- read      - Reading files efficiently
- write     - Writing and creating files
- edit      - Editing files (most important!)
- search    - Finding content in files
- batch     - Multiple operations at once
- errors    - Common errors and fixes
- examples  - Code examples
- tips      - Pro tips for efficiency
- all       - Everything (long output)

Example: server_info(topic:"edit")
`, topic))
	}

	return sb.String()
}

// formatChangeAnalysis formats a ChangeAnalysis struct as human-readable text
func formatChangeAnalysis(analysis *core.ChangeAnalysis) string {
	var result strings.Builder

	// Header
	result.WriteString("Change Analysis (Plan Mode - Dry Run)\n")
	result.WriteString("---\n\n")

	// Basic info
	result.WriteString(fmt.Sprintf("File: %s\n", analysis.FilePath))
	result.WriteString(fmt.Sprintf("Operation: %s\n", analysis.OperationType))
	result.WriteString(fmt.Sprintf("File exists: %v\n", analysis.FileExists))

	// Risk assessment
	result.WriteString(fmt.Sprintf("\nRisk Level: %s\n", strings.ToUpper(analysis.RiskLevel)))

	// Risk factors
	if len(analysis.RiskFactors) > 0 {
		result.WriteString("\nRisk Factors:\n")
		for _, factor := range analysis.RiskFactors {
			result.WriteString(fmt.Sprintf("  - %s\n", factor))
		}
	}

	// Changes summary
	result.WriteString("\nChanges Summary:\n")
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
	result.WriteString(fmt.Sprintf("\nImpact: %s\n", analysis.Impact))

	// Preview
	if analysis.Preview != "" {
		result.WriteString(fmt.Sprintf("\nPreview:\n%s\n", analysis.Preview))
	}

	// Suggestions
	if len(analysis.Suggestions) > 0 {
		result.WriteString("\nSuggestions:\n")
		for _, suggestion := range analysis.Suggestions {
			result.WriteString(fmt.Sprintf("  - %s\n", suggestion))
		}
	}

	// Additional info
	result.WriteString("\nAdditional Info:\n")
	result.WriteString(fmt.Sprintf("  - Backup would be created: %v\n", analysis.WouldCreateBackup))
	result.WriteString(fmt.Sprintf("  - Estimated time: %s\n", analysis.EstimatedTime))

	result.WriteString("\n---\n")
	result.WriteString("This is a DRY RUN - no changes were made\n")

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
		sb.WriteString("Batch Validation Results\n")
		sb.WriteString("---\n\n")
		if result.Success {
			sb.WriteString(fmt.Sprintf("All %d operations validated successfully\n", result.TotalOps))
			sb.WriteString("Ready to execute\n")
		} else {
			sb.WriteString("Validation failed\n")
			sb.WriteString(fmt.Sprintf("Errors: %v\n", result.Errors))
		}
		return sb.String()
	}

	// Execution results
	if result.Success {
		sb.WriteString("Batch Operations Completed Successfully\n")
	} else {
		sb.WriteString("Batch Operations Failed\n")
	}
	sb.WriteString("---\n\n")

	sb.WriteString("Summary:\n")
	sb.WriteString(fmt.Sprintf("  Total operations: %d\n", result.TotalOps))
	sb.WriteString(fmt.Sprintf("  Completed: %d\n", result.CompletedOps))
	sb.WriteString(fmt.Sprintf("  Failed: %d\n", result.FailedOps))
	sb.WriteString(fmt.Sprintf("  Execution time: %s\n", result.ExecutionTime))

	if result.BackupPath != "" {
		sb.WriteString(fmt.Sprintf("  Backup created: %s\n", result.BackupPath))
	}

	if result.RollbackDone {
		sb.WriteString("\nRollback performed - all changes reverted\n")
	}

	// Individual operation results
	sb.WriteString("\nOperation Details:\n")
	for _, opResult := range result.Results {
		status := "OK"
		if !opResult.Success {
			status = "FAIL"
		} else if opResult.Skipped {
			status = "SKIP"
		}

		sb.WriteString(fmt.Sprintf("  [%s] [%d] %s: %s", status, opResult.Index, opResult.Type, opResult.Path))

		if opResult.BytesAffected > 0 {
			sb.WriteString(fmt.Sprintf(" (%s)", formatSize(opResult.BytesAffected)))
		}

		if opResult.Error != "" {
			sb.WriteString(fmt.Sprintf(" - Error: %s", opResult.Error))
		}

		sb.WriteString("\n")
	}

	if len(result.Errors) > 0 {
		sb.WriteString("\nErrors:\n")
		for _, err := range result.Errors {
			sb.WriteString(fmt.Sprintf("  - %s\n", err))
		}
	}

	return sb.String()
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
		// Include error details from failed steps (Bug #21: silent failures)
		errorInfo := ""
		if !result.Success {
			for _, sr := range result.Results {
				if sr.Error != "" {
					errorInfo = fmt.Sprintf(" | %s:%s: %s", sr.StepID, sr.Action, sr.Error)
					break // Show first error only in compact mode
				}
			}
			if errorInfo == "" && result.RollbackPerformed {
				errorInfo = " | rolled back"
			}
		}
		return fmt.Sprintf("%s: %d/%d steps | %d files | %d edits%s%s",
			status, result.CompletedSteps, result.TotalSteps,
			len(result.FilesAffected), result.TotalEdits, riskInfo, errorInfo)
	}

	// Verbose mode: detailed output
	var output strings.Builder

	if result.DryRun {
		output.WriteString("DRY RUN - No changes made\n")
	}

	output.WriteString("---\n")
	output.WriteString(fmt.Sprintf("Pipeline: %s\n", result.Name))

	if result.Success {
		output.WriteString("Success: true\n")
	} else {
		output.WriteString("Success: false\n")
	}

	output.WriteString(fmt.Sprintf("Steps: %d/%d completed\n", result.CompletedSteps, result.TotalSteps))
	output.WriteString(fmt.Sprintf("Duration: %v\n", result.TotalDuration))

	if result.BackupID != "" {
		output.WriteString(fmt.Sprintf("Backup: %s\n", result.BackupID))
	}

	if result.RollbackPerformed {
		output.WriteString("Rollback: performed\n")
	}

	output.WriteString("\nStep Results:\n\n")

	for i, stepResult := range result.Results {
		stepNum := i + 1
		status := "OK"
		if !stepResult.Success {
			status = "FAIL"
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
			output.WriteString(fmt.Sprintf("   Risk: %s\n", stepResult.RiskLevel))
		}

		if stepResult.Error != "" {
			output.WriteString(fmt.Sprintf("   Error: %s\n", stepResult.Error))
		}

		output.WriteString("\n")
	}

	output.WriteString("---\n")
	output.WriteString(fmt.Sprintf("Files affected: %d\n", len(result.FilesAffected)))
	output.WriteString(fmt.Sprintf("Total edits: %d\n", result.TotalEdits))

	if result.OverallRiskLevel != "" {
		output.WriteString(fmt.Sprintf("Overall risk: %s\n", result.OverallRiskLevel))
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
	log.Printf("Running performance benchmark...")

	// This would run comprehensive benchmarks comparing:
	// 1. This ultra-fast server vs standard MCP
	// 2. Various cache sizes and parallel operation counts
	// 3. Different file sizes and operation types

	fmt.Printf("Benchmark results will be implemented in bench/ package\n")
	fmt.Printf("Run: cd bench && go run benchmark.go\n")
}
