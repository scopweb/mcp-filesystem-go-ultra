package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/panjf2000/ants/v2"
)

// Config holds configuration for the ultra-fast engine
type Config struct {
	Cache            *cache.IntelligentCache
	ParallelOps      int
	VSCodeAPIEnabled bool
	DebugMode        bool
	AllowedPaths     []string
	BinaryThreshold  int64
	CompactMode      bool   // Enable compact responses
	MaxResponseSize  int64  // Max response size
	MaxSearchResults int    // Max search results
	MaxListItems     int    // Max list items
	HooksConfigPath  string // Path to hooks configuration file
	HooksEnabled     bool   // Enable hooks system

	// Backup configuration
	BackupDir      string // Directory for backup storage
	BackupMaxAge   int    // Max age of backups in days
	BackupMaxCount int    // Max number of backups to keep

	// Risk thresholds
	RiskThresholdMedium   float64 // % change for medium risk
	RiskThresholdHigh     float64 // % change for high risk
	RiskOccurrencesMedium int     // Occurrences for medium risk
	RiskOccurrencesHigh   int     // Occurrences for high risk

	// Logging
	LogDir string // Directory for audit logs and metrics snapshots (empty = disabled)

	// Normalizer
	NormalizerRulesPath string // Path to external normalizer rules JSON file (optional)
}

// UltraFastEngine implements all filesystem operations with maximum performance
type UltraFastEngine struct {
	config *Config
	cache  *cache.IntelligentCache

	// Performance monitoring
	metrics *PerformanceMetrics

	// Parallel operation management
	semaphore  chan struct{}
	workerPool *ants.Pool

	// Artifact buffer
	lastArtifact  string
	artifactMutex sync.RWMutex

	// Claude Desktop optimizer
	optimizer *ClaudeDesktopOptimizer

	// Hook manager
	hookManager *HookManager

	// Auto-sync manager for WSL<->Windows
	autoSyncManager *AutoSyncManager

	// Buffer pool for memory-efficient I/O operations
	bufferPool *sync.Pool

	// Backup manager for file protection
	backupManager *BackupManager

	// Backup chain for step-through undo (path → current backupID in chain)
	backupChain map[string]string
	backupChainMu sync.RWMutex

	// Risk thresholds for impact analysis
	riskThresholds RiskThresholds

	// Environment detection cache (WSL/Windows detection)
	// Caches the result of DetectEnvironment() to avoid repeated /proc/version reads
	envCache struct {
		isWSL       bool
		windowsUser string
		cachedAt    time.Time
		mu          sync.RWMutex
	}

	// Regex compilation cache
	// Caches compiled regex patterns to avoid repeated compilation in hot paths
	regexCache struct {
		cache map[string]*regexp.Regexp
		mu    sync.RWMutex
	}

	// Pre-resolved allowed paths (resolved once at startup via Abs + EvalSymlinks + norm)
	// Only base allowed paths are cached; target paths are still resolved per-call for security.
	resolvedAllowedPaths []string

	// Audit logger for operation tracking (nil if --log-dir not set)
	auditLogger *AuditLogger

	// Request normalizer for parameter aliasing and coercion
	normalizer *Normalizer

	// Session tracking: groups consecutive operations into conversation sessions.
	// A new session starts when > sessionInactivityTimeout elapses between operations.
	session struct {
		mu        sync.Mutex
		id        string
		lastOpAt  time.Time
	}
}

const sessionInactivityTimeout = 5 * time.Minute

// PerformanceMetrics tracks real-time performance statistics
type PerformanceMetrics struct {
	mu                  sync.RWMutex
	OperationsTotal     int64
	OperationsPerSecond float64
	CacheHitRate        float64
	AverageResponseTime time.Duration
	MemoryUsage         int64
	LastUpdateTime      time.Time

	// Operation-specific metrics
	ReadOperations   int64
	WriteOperations  int64
	ListOperations   int64
	SearchOperations int64

	// Telemetry: Track edit operations
	// Used to detect full-file rewrites vs targeted edits
	EditOperations      int64   // Total edit operations
	TargetedEdits       int64   // Edits with small old_text (<100 bytes)
	FullFileRewrites    int64   // Edits with large old_text (>1000 bytes)
	LastEditOperation   string  // Description of last edit
	LastEditBytesSent   int64   // Bytes sent in last edit operation
	AverageBytesPerEdit float64 // Running average of bytes per edit
}

// LogOperationMetric records detailed operation metrics for analysis
type LogOperationMetric struct {
	Timestamp      time.Time
	Operation      string // "edit", "write", "read", "search"
	FilePath       string
	BytesProcessed int64
	Duration       time.Duration
	IsFullRewrite  bool // true if full-file rewrite detected
	IsTargetedEdit bool // true if surgical edit (<100 bytes old_text)
}

// EditResult holds the result of an edit operation
// MOVED to edit_operations.go

// NewUltraFastEngine creates a new ultra-fast filesystem engine
func NewUltraFastEngine(config *Config) (*UltraFastEngine, error) {
	// Set defaults for missing config values
	if config.MaxSearchResults == 0 {
		config.MaxSearchResults = MaxSearchResults // 1000
	}
	if config.MaxListItems == 0 {
		config.MaxListItems = MaxListItems // 10000
	}

	engine := &UltraFastEngine{
		config:    config,
		cache:     config.Cache,
		metrics:   &PerformanceMetrics{},
		semaphore: make(chan struct{}, config.ParallelOps),
		backupChain: make(map[string]string),
	}

	// Initialize buffer pool for memory-efficient I/O operations
	// Using 64KB buffers for optimal performance
	engine.bufferPool = &sync.Pool{
		New: func() interface{} {
			// Allocate 64KB buffer (optimal for most I/O operations)
			buf := make([]byte, 64*1024)
			return &buf
		},
	}

	// Initialize regex cache for compiled patterns
	engine.regexCache.cache = make(map[string]*regexp.Regexp)

	// Log if allowed paths are configured
	if len(config.AllowedPaths) > 0 {
		slog.Info("Access control enabled", "allowed_paths_count", len(config.AllowedPaths))
		engine.resolveAllowedPaths()
	} else {
		slog.Warn("Access control disabled - full filesystem access allowed")
	}

	// Initialize worker pool for parallel operations
	workerPool, err := ants.NewPool(config.ParallelOps, ants.WithPreAlloc(true))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize worker pool: %w", err)
	}
	engine.workerPool = workerPool

	slog.Info("Ultra-fast engine initialized", "parallel_ops", config.ParallelOps, "buffer", "64KB")

	// Initialize Claude Desktop optimizer
	engine.optimizer = NewClaudeDesktopOptimizer(engine)
	slog.Info("Claude Desktop optimizer initialized")

	// Initialize hook manager
	engine.hookManager = NewHookManager()
	if config.HooksEnabled && config.HooksConfigPath != "" {
		if err := engine.hookManager.LoadConfig(config.HooksConfigPath); err != nil {
			slog.Warn("Failed to load hooks config", "error", err, "status", "disabled")
		} else {
			engine.hookManager.SetEnabled(true)
			engine.hookManager.SetDebugMode(config.DebugMode)
			slog.Info("Hook system initialized")
		}
	}

	// Initialize auto-sync manager
	engine.autoSyncManager = NewAutoSyncManager()
	if engine.autoSyncManager.IsEnabled() {
		slog.Info("WSL auto-sync enabled")
	} else {
		isWSL, _ := DetectEnvironment()
		if isWSL {
			slog.Info("WSL detected - auto-sync disabled", "enable_command", "configure_autosync or MCP_WSL_AUTOSYNC=true")
		}
	}

	// Initialize backup manager
	backupMaxAge := config.BackupMaxAge
	if backupMaxAge <= 0 {
		backupMaxAge = 7 // Default: 7 days
	}
	backupMaxCount := config.BackupMaxCount
	if backupMaxCount <= 0 {
		backupMaxCount = 100 // Default: 100 backups
	}

	backupManager, err := NewBackupManager(config.BackupDir, backupMaxCount, backupMaxAge)
	if err != nil {
		slog.Warn("Failed to initialize backup manager", "error", err, "status", "disabled")
	} else {
		engine.backupManager = backupManager
		slog.Info("Backup manager initialized", "backup_dir", backupManager.backupDir,
			"max_age_days", backupMaxAge, "max_count", backupMaxCount)
	}

	// Initialize risk thresholds
	if config.RiskThresholdMedium > 0 {
		engine.riskThresholds.MediumPercentage = config.RiskThresholdMedium
	} else {
		engine.riskThresholds.MediumPercentage = 20.0
	}
	if config.RiskThresholdHigh > 0 {
		engine.riskThresholds.HighPercentage = config.RiskThresholdHigh
	} else {
		engine.riskThresholds.HighPercentage = 75.0
	}
	if config.RiskOccurrencesMedium > 0 {
		engine.riskThresholds.MediumOccurrences = config.RiskOccurrencesMedium
	} else {
		engine.riskThresholds.MediumOccurrences = 50
	}
	if config.RiskOccurrencesHigh > 0 {
		engine.riskThresholds.HighOccurrences = config.RiskOccurrencesHigh
	} else {
		engine.riskThresholds.HighOccurrences = 100
	}

	// Initialize audit logger if log directory is configured
	if config.LogDir != "" {
		auditLogger, err := NewAuditLogger(config.LogDir)
		if err != nil {
			slog.Warn("Failed to initialize audit logger", "error", err, "status", "disabled")
		} else {
			engine.auditLogger = auditLogger
			slog.Info("Audit logger initialized", "log_dir", config.LogDir)

			// Start periodic metrics snapshot writer (every 30 seconds)
			go engine.metricsSnapshotLoop(config.LogDir)
		}
	}

	// Initialize request normalizer (always active — built-in rules have zero overhead if no match)
	normalizer, normErr := NewNormalizer(config.NormalizerRulesPath, config.LogDir)
	if normErr != nil {
		slog.Warn("Failed to load external normalizer rules, using built-in only", "error", normErr)
		normalizer, _ = NewNormalizer("", config.LogDir)
	}
	engine.normalizer = normalizer
	slog.Info("Request normalizer initialized", "rules", normalizer.RulesCount())

	return engine, nil
}

// resolveAllowedPaths pre-resolves all AllowedPaths using Abs + EvalSymlinks + normalization.
// Called once at engine initialization. Avoids repeated EvalSymlinks syscalls in isPathAllowed().
func (e *UltraFastEngine) resolveAllowedPaths() {
	norm := func(p string) string {
		p = filepath.Clean(p)
		if os.PathSeparator == '\\' {
			return strings.ToLower(p)
		}
		return p
	}

	e.resolvedAllowedPaths = make([]string, 0, len(e.config.AllowedPaths))
	for _, allowed := range e.config.AllowedPaths {
		baseAbs, err := filepath.Abs(allowed)
		if err != nil {
			slog.Warn("Failed to resolve allowed path", "path", allowed, "error", err)
			continue
		}
		if baseResolved, err := filepath.EvalSymlinks(baseAbs); err == nil {
			baseAbs = baseResolved
		}
		e.resolvedAllowedPaths = append(e.resolvedAllowedPaths, norm(baseAbs))
	}
}

// Close gracefully shuts down the engine
func (e *UltraFastEngine) Close() error {
	if e.workerPool != nil {
		e.workerPool.Release()
	}
	if e.auditLogger != nil {
		e.auditLogger.Close()
	}
	return nil
}

// Audit logs an operation entry to the audit log file (no-op if audit logger not configured)
func (e *UltraFastEngine) Audit(entry AuditEntry) {
	if e.auditLogger != nil {
		e.auditLogger.Log(entry)
	}
}

// AuditEnabled returns true when the audit logger is active (--log-dir was set)
func (e *UltraFastEngine) AuditEnabled() bool {
	return e.auditLogger != nil
}

// CurrentSessionID returns the active session identifier, starting a new session
// if more than sessionInactivityTimeout has elapsed since the last operation.
// Sessions group operations belonging to the same Claude conversation.
func (e *UltraFastEngine) CurrentSessionID() string {
	e.session.mu.Lock()
	defer e.session.mu.Unlock()

	now := time.Now()
	if e.session.id == "" || now.Sub(e.session.lastOpAt) > sessionInactivityTimeout {
		// Generate 8-byte random hex ID (16 chars) — short but collision-resistant per session
		var b [8]byte
		rand.Read(b[:])
		e.session.id = hex.EncodeToString(b[:])
	}
	e.session.lastOpAt = now
	return e.session.id
}

// GetNormalizer returns the request normalizer
func (e *UltraFastEngine) GetNormalizer() *Normalizer {
	return e.normalizer
}

// metricsSnapshotLoop writes a metrics.json snapshot every 30 seconds
func (e *UltraFastEngine) metricsSnapshotLoop(logDir string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		e.metrics.mu.RLock()
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		snapshot := MetricsSnapshot{
			UpdatedAt:    time.Now(),
			OpsTotal:     e.metrics.OperationsTotal,
			OpsPerSec:    e.metrics.OperationsPerSecond,
			CacheHitRate: e.metrics.CacheHitRate,
			MemoryMB:     float64(memStats.Alloc) / (1024 * 1024),
			Reads:        e.metrics.ReadOperations,
			Writes:       e.metrics.WriteOperations,
			Lists:        e.metrics.ListOperations,
			Searches:     e.metrics.SearchOperations,
			Edits: MetricsEditSummary{
				Total:    e.metrics.EditOperations,
				Targeted: e.metrics.TargetedEdits,
				Rewrites: e.metrics.FullFileRewrites,
				AvgBytes: e.metrics.AverageBytesPerEdit,
			},
		}
		e.metrics.mu.RUnlock()

		if err := WriteMetricsSnapshot(logDir, snapshot); err != nil {
			slog.Debug("Failed to write metrics snapshot", "error", err)
		}
	}
}

// GetEnvironment returns the cached WSL/Windows environment detection result
// Uses a 5-minute cache to avoid repeated /proc/version reads on every path normalization
func (e *UltraFastEngine) GetEnvironment() (isWSL bool, windowsUser string) {
	e.envCache.mu.RLock()
	// Check if cache is still valid (5-minute TTL)
	if !e.envCache.cachedAt.IsZero() && time.Since(e.envCache.cachedAt) < 5*time.Minute {
		defer e.envCache.mu.RUnlock()
		return e.envCache.isWSL, e.envCache.windowsUser
	}
	e.envCache.mu.RUnlock()

	// Cache miss or expired - detect environment
	e.envCache.mu.Lock()
	defer e.envCache.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have updated it)
	if !e.envCache.cachedAt.IsZero() && time.Since(e.envCache.cachedAt) < 5*time.Minute {
		return e.envCache.isWSL, e.envCache.windowsUser
	}

	// Perform actual detection
	isWSL, windowsUser = DetectEnvironment()
	e.envCache.isWSL = isWSL
	e.envCache.windowsUser = windowsUser
	e.envCache.cachedAt = time.Now()

	return isWSL, windowsUser
}

// CompileRegex returns a cached compiled regex pattern or compiles and caches a new one
// Avoids expensive regex.Compile calls in hot paths
func (e *UltraFastEngine) CompileRegex(pattern string) (*regexp.Regexp, error) {
	// Check cache first (read lock)
	e.regexCache.mu.RLock()
	if re, exists := e.regexCache.cache[pattern]; exists {
		e.regexCache.mu.RUnlock()
		return re, nil
	}
	e.regexCache.mu.RUnlock()

	// Cache miss - compile and store
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	// Store in cache (write lock)
	e.regexCache.mu.Lock()
	defer e.regexCache.mu.Unlock()

	// Simple LRU: clear cache if it grows too large (>100 patterns)
	if len(e.regexCache.cache) > 100 {
		e.regexCache.cache = make(map[string]*regexp.Regexp)
	}

	e.regexCache.cache[pattern] = re
	return re, nil
}

// StartMonitoring starts real-time performance monitoring
func (e *UltraFastEngine) StartMonitoring(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.updateMetrics()
		}
	}
}

// updateMetrics calculates and updates performance metrics
func (e *UltraFastEngine) updateMetrics() {
	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()

	now := time.Now()
	if !e.metrics.LastUpdateTime.IsZero() {
		duration := now.Sub(e.metrics.LastUpdateTime).Seconds()
		if duration > 0 {
			e.metrics.OperationsPerSecond = float64(e.metrics.OperationsTotal) / duration
		}
	}

	// Update cache hit rate
	if e.cache != nil {
		e.metrics.CacheHitRate = e.cache.GetHitRate()
	}

	// Update memory usage
	e.metrics.MemoryUsage = e.cache.GetMemoryUsage()
	e.metrics.LastUpdateTime = now
}

// acquireOperation gets semaphore slot for rate limiting
func (e *UltraFastEngine) acquireOperation(ctx context.Context, opType string) error {
	select {
	case e.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return context.Canceled
	}
}

// releaseOperation releases semaphore slot and updates metrics
func (e *UltraFastEngine) releaseOperation(opType string, start time.Time) {
	// Update metrics
	e.metrics.mu.Lock()
	e.metrics.OperationsTotal++
	duration := time.Since(start)
	e.metrics.AverageResponseTime = (e.metrics.AverageResponseTime + duration) / 2

	// Update operation-specific counters
	switch opType {
	case "read":
		e.metrics.ReadOperations++
	case "write":
		e.metrics.WriteOperations++
	case "list":
		e.metrics.ListOperations++
	case "search":
		e.metrics.SearchOperations++
	}
	e.metrics.mu.Unlock()

	// Release semaphore slot
	<-e.semaphore
}

// ReadFileContent implements ultra-fast file reading with intelligent caching
func (e *UltraFastEngine) ReadFileContent(ctx context.Context, path string) (string, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", &ContextError{Op: "read_file", Details: "operation cancelled before start"}
	}

	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore
	if err := e.acquireOperation(ctx, "read"); err != nil {
		return "", err
	}

	start := time.Now()
	defer e.releaseOperation("read", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return "", &PathError{Op: "read", Path: path, Err: fmt.Errorf("access denied")}
	}

	// Execute pre-read hook
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreRead,
		ToolName:   "read_file",
		FilePath:   path,
		Operation:  "read",
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreRead, hookCtx); err != nil {
		return "", fmt.Errorf("pre-read hook denied operation: %w", err)
	}

	// Try cache first
	if cached, hit := e.cache.GetFile(path); hit {
		if e.config.DebugMode {
			slog.Debug("Cache hit", "path", path)
		}
		// Track access for predictive prefetching
		e.cache.TrackAccess(path)
		return string(cached), nil
	}

	// Check context before disk I/O
	if err := ctx.Err(); err != nil {
		return "", &ContextError{Op: "read_file", Details: "operation cancelled before disk read"}
	}

	// Read from disk with context awareness
	type readResult struct {
		content []byte
		err     error
	}

	resultChan := make(chan readResult, 1)
	go func() {
		content, err := os.ReadFile(path)
		resultChan <- readResult{content, err}
	}()

	var result readResult
	select {
	case <-ctx.Done():
		return "", &ContextError{Op: "read_file", Details: "operation cancelled during disk read"}
	case result = <-resultChan:
		if result.err != nil {
			return "", &PathError{Op: "read", Path: path, Err: result.err}
		}
	}

	// Cache the content and track access
	e.cache.SetFile(path, result.content)
	e.cache.TrackAccess(path)

	// Execute post-read hook (best-effort)
	hookCtx.Event = HookPostRead
	hookCtx.Metadata = map[string]interface{}{"bytes": len(result.content)}
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostRead, hookCtx)

	return string(result.content), nil
}

// WriteFileContent implements atomic file writing
func (e *UltraFastEngine) WriteFileContent(ctx context.Context, path, content string) error {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return &ContextError{Op: "write_file", Details: "operation cancelled before start"}
	}

	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore
	if err := e.acquireOperation(ctx, "write"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("write", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return &PathError{Op: "write", Path: path, Err: fmt.Errorf("access denied")}
	}

	// Check context before proceeding with write
	if err := ctx.Err(); err != nil {
		return &ContextError{Op: "write_file", Details: "operation cancelled before write"}
	}

	// Execute pre-write hooks
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreWrite,
		ToolName:   "write_file",
		FilePath:   path,
		Operation:  "write",
		Content:    content,
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
	}

	hookResult, err := e.hookManager.ExecuteHooks(ctx, HookPreWrite, hookCtx)
	if err != nil {
		return fmt.Errorf("pre-write hook denied operation: %w", err)
	}

	// Use modified content if hook provided it (e.g., formatted code)
	finalContent := content
	if hookResult.ModifiedContent != "" {
		finalContent = hookResult.ModifiedContent
	}

	// EOL preservation (Bug #33): if the file already exists, detect its EOL
	// style and convert finalContent to match. For new files, leave content as-is
	// (let the caller decide the EOL style).
	if existing, statErr := os.Stat(path); statErr == nil && !existing.IsDir() {
		if existingBytes, readErr := os.ReadFile(path); readErr == nil {
			existingEOL := detectEOL(string(existingBytes))
			if existingEOL != "\n" {
				// Normalize finalContent to LF first (it may already be LF, or carry CRLF
				// from the LLM), then restore the file's original EOL.
				finalContent = restoreEOL(normalizeLineEndings(finalContent), existingEOL)
			}
		}
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Atomic write using temp file with secure random name
	tmpPath := path + ".tmp." + secureRandomSuffix()

	// Preserve original file permissions if file exists, otherwise use 0644
	fileMode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		fileMode = info.Mode()
	}

	// Write to temporary file
	if err := os.WriteFile(tmpPath, []byte(finalContent), fileMode); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	// Execute post-write hooks
	hookCtx.Event = HookPostWrite
	hookCtx.Content = finalContent
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostWrite, hookCtx)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterWrite(path)
	}

	return nil
}

// WriteFileBytes writes raw bytes to a file atomically.
// This is useful for binary files where content comes from base64 decoding.
// The path is normalized for WSL/Windows compatibility.
func (e *UltraFastEngine) WriteFileBytes(ctx context.Context, path string, data []byte) error {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return &ContextError{Op: "write_bytes", Details: "operation cancelled before start"}
	}

	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore
	if err := e.acquireOperation(ctx, "write"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("write", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return &PathError{Op: "write_bytes", Path: path, Err: fmt.Errorf("access denied")}
	}

	// Check context before proceeding with write
	if err := ctx.Err(); err != nil {
		return &ContextError{Op: "write_bytes", Details: "operation cancelled before write"}
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Atomic write using temp file with secure random name
	tmpPath := path + ".tmp." + secureRandomSuffix()

	// Preserve original file permissions if file exists, otherwise use 0644
	fileMode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		fileMode = info.Mode()
	}

	// Write to temporary file
	if err := os.WriteFile(tmpPath, data, fileMode); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterWrite(path)
	}

	return nil
}

// ReadFileBytes reads a file and returns its raw bytes.
// This is useful for binary files that need to be base64 encoded.
// The path is normalized for WSL/Windows compatibility.
func (e *UltraFastEngine) ReadFileBytes(ctx context.Context, path string) ([]byte, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, &ContextError{Op: "read_bytes", Details: "operation cancelled before start"}
	}

	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore
	if err := e.acquireOperation(ctx, "read"); err != nil {
		return nil, err
	}

	start := time.Now()
	defer e.releaseOperation("read", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return nil, &PathError{Op: "read_bytes", Path: path, Err: fmt.Errorf("access denied")}
	}

	// Check file info
	info, err := os.Stat(path)
	if err != nil {
		return nil, &PathError{Op: "read_bytes", Path: path, Err: err}
	}

	if info.IsDir() {
		return nil, &ValidationError{Field: "path", Value: path, Message: "path is a directory, not a file"}
	}

	// Read file bytes
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &PathError{Op: "read_bytes", Path: path, Err: err}
	}

	return data, nil
}

// WriteBase64 decodes base64 content and writes it to a file atomically.
// This enables copying binary files from environments where only text transfer is possible.
func (e *UltraFastEngine) WriteBase64(ctx context.Context, path, contentBase64 string) (int, error) {
	// Decode base64
	data, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return 0, &ValidationError{Field: "content_base64", Value: "(invalid)", Message: fmt.Sprintf("invalid base64: %v", err)}
	}

	// Write the decoded bytes
	if err := e.WriteFileBytes(ctx, path, data); err != nil {
		return 0, err
	}

	return len(data), nil
}

// ReadBase64 reads a file and returns its content as base64.
// This enables reading binary files in environments where only text transfer is possible.
func (e *UltraFastEngine) ReadBase64(ctx context.Context, path string) (string, int64, error) {
	data, err := e.ReadFileBytes(ctx, path)
	if err != nil {
		return "", 0, err
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return encoded, int64(len(data)), nil
}

// ListDirectoryContent implements intelligent directory listing with caching
func (e *UltraFastEngine) ListDirectoryContent(ctx context.Context, path string) (string, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore
	if err := e.acquireOperation(ctx, "list"); err != nil {
		return "", err
	}

	start := time.Now()
	defer e.releaseOperation("list", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return "", fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
	}

	// Stat the directory once; used both for existence check and mtime validation.
	dirInfo, statErr := os.Stat(path)

	// Try cache first, but validate against directory mtime to detect external writes
	// (e.g. files copied by bash/cp outside the MCP server's control).
	if cached, cachedMtime, hit := e.cache.GetDirectory(path); hit {
		if statErr == nil && !dirInfo.ModTime().After(cachedMtime) {
			if e.config.DebugMode {
				slog.Debug("Directory cache hit", "path", path)
			}
			return cached, nil
		}
		// Directory was modified externally since the cache was populated.
		if e.config.DebugMode {
			slog.Debug("Directory cache invalidated (external write detected)", "path", path)
		}
		e.cache.InvalidateDirectory(path)
	}

	// Read directory
	if statErr != nil {
		return "", fmt.Errorf("failed to read directory: %w", statErr)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	// Build response - compact or verbose mode
	var result strings.Builder

	if e.config.CompactMode {
		// Compact mode: ls-style, AI-friendly
		result.WriteString(fmt.Sprintf("%s |", path))

		maxItems := e.config.MaxListItems
		count := 0
		for _, entry := range entries {
			if count >= maxItems {
				result.WriteString(fmt.Sprintf(" | ...+%d", len(entries)-count))
				break
			}

			result.WriteString(" ")
			if entry.IsDir() {
				result.WriteString(entry.Name())
				result.WriteString("/")
			} else {
				info, err := entry.Info()
				if err == nil && info.Size() > 512 {
					result.WriteString(fmt.Sprintf("%s(%s)", entry.Name(), formatSize(info.Size())))
				} else {
					result.WriteString(entry.Name())
				}
			}
			count++
		}
		result.WriteString(fmt.Sprintf(" | %d/%d", count, len(entries)))
	} else {
		// Verbose mode: ls -la style, AI-friendly
		totalDirs, totalFiles := 0, 0
		for _, entry := range entries {
			if entry.IsDir() {
				totalDirs++
			} else {
				totalFiles++
			}
		}

		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}

			if entry.IsDir() {
				result.WriteString(fmt.Sprintf("DIR  %s/", entry.Name()))
			} else {
				result.WriteString(fmt.Sprintf("FILE %s %s", entry.Name(), formatSize(info.Size())))
			}
			result.WriteString(fmt.Sprintf(" | %s\n", path))
		}

		result.WriteString(fmt.Sprintf("--- | %d dirs, %d files | %s", totalDirs, totalFiles, path))
	}

	responseText := result.String()

	// Cache the result together with the directory's current mtime so future
	// reads can detect external modifications.
	e.cache.SetDirectory(path, responseText, dirInfo.ModTime())

	return responseText, nil
}

// EditFile implements intelligent file editing
// MOVED to core/edit_operations.go

// GetPerformanceStats returns performance statistics
func (e *UltraFastEngine) GetPerformanceStats() string {
	e.metrics.mu.RLock()
	defer e.metrics.mu.RUnlock()

	if e.config.CompactMode {
		// Compact format: key metrics only
		return fmt.Sprintf("ops/s:%.1f hit:%.1f%% mem:%s ops:%d",
			e.metrics.OperationsPerSecond,
			e.metrics.CacheHitRate*100,
			formatSize(e.metrics.MemoryUsage),
			e.metrics.OperationsTotal)
	}

	// Verbose format
	return fmt.Sprintf(`Performance Statistics:
Operations Total: %d
Operations/Second: %.2f
Cache Hit Rate: %.2f%%
Average Response Time: %v
Memory Usage: %s
Read Operations: %d
Write Operations: %d
List Operations: %d
Search Operations: %d`,
		e.metrics.OperationsTotal,
		e.metrics.OperationsPerSecond,
		e.metrics.CacheHitRate*100,
		e.metrics.AverageResponseTime,
		formatSize(e.metrics.MemoryUsage),
		e.metrics.ReadOperations,
		e.metrics.WriteOperations,
		e.metrics.ListOperations,
		e.metrics.SearchOperations)
}

// IsPathAllowed checks if the given path is within one of the allowed base paths.
// It resolves symlinks to prevent sandbox escape via symlink following.
// Exported so that BatchOperationManager and other subsystems can enforce access control.
//
// Security checks (NTFS ADS, Unicode control chars, Windows reserved names) are ALWAYS
// applied, even when --allowed-paths is not configured (open-access mode).
// The WSL blanket bypass has been removed: WSL paths must be explicitly included in
// --allowed-paths when access control is active.
func (e *UltraFastEngine) IsPathAllowed(path string) bool {
	// 1. Security-first: reject dangerous path patterns regardless of AllowedPaths config.
	if err := validatePathSecurity(path); err != nil {
		slog.Debug("Path rejected by security check", "path", path, "reason", err.Error())
		return false
	}

	// 2. When AllowedPaths is not configured, open-access mode — security checks above
	//    still apply, but containment is not enforced.
	if len(e.config.AllowedPaths) == 0 {
		return true
	}

	// 3. Re-resolve if AllowedPaths changed at runtime (e.g., tests append paths after init)
	if len(e.resolvedAllowedPaths) != len(e.config.AllowedPaths) {
		e.resolveAllowedPaths()
	}

	// Resolve to absolute, cleaned paths to prevent traversal and casing issues
	targetAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Resolve symlinks to prevent symlink-based sandbox escape.
	// If the path doesn't exist yet (e.g., write to new file or mkdir -p),
	// walk up the tree to find the first existing ancestor and resolve from there.
	resolved, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		// Walk up the directory tree to find the deepest existing ancestor
		current := targetAbs
		var suffix []string
		for {
			parent := filepath.Dir(current)
			suffix = append([]string{filepath.Base(current)}, suffix...)
			if parent == current {
				// Reached filesystem root without finding existing path
				break
			}
			if parentResolved, parentErr := filepath.EvalSymlinks(parent); parentErr == nil {
				resolved = parentResolved
				for _, s := range suffix {
					resolved = filepath.Join(resolved, s)
				}
				break
			}
			current = parent
		}
		if resolved == "" {
			return false
		}
	}
	targetAbs = resolved

	// On Windows, compare case-insensitively
	norm := func(p string) string {
		p = filepath.Clean(p)
		if os.PathSeparator == '\\' { // Windows
			return strings.ToLower(p)
		}
		return p
	}

	targetAbs = norm(targetAbs)

	for _, baseAbs := range e.resolvedAllowedPaths {
		// Quick equality check
		if targetAbs == baseAbs {
			return true
		}

		// Safe containment check using filepath.Rel; ensures boundary-aware prefix
		rel, err := filepath.Rel(baseAbs, targetAbs)
		if err == nil {
			if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return true
			}
		}
	}

	if e.config.DebugMode {
		slog.Debug("Access denied", "path", path)
	}
	return false
}

// IsAllowedPathRoot returns true if the given path resolves to one of the
// configured --allowed-paths roots. Destructive operations (delete, move)
// must reject these paths to prevent wiping out an entire allowed tree.
func (e *UltraFastEngine) IsAllowedPathRoot(path string) bool {
	if len(e.resolvedAllowedPaths) == 0 {
		return false
	}

	targetAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(targetAbs); err == nil {
		targetAbs = resolved
	}

	norm := func(p string) string {
		p = filepath.Clean(p)
		if os.PathSeparator == '\\' {
			return strings.ToLower(p)
		}
		return p
	}
	targetAbs = norm(targetAbs)

	for _, baseAbs := range e.resolvedAllowedPaths {
		if targetAbs == baseAbs {
			return true
		}
	}
	return false
}

// NormalizePath converts between WSL and Windows paths automatically
// This handles the common issue where Claude Code on Windows sends WSL-style paths
// Examples:
//   - /mnt/c/Users/... → C:\Users\... (when running on Windows)
//   - C:\Users\... → /mnt/c/Users/... (when running on WSL/Linux)
//   - /mnt/d/Projects/... → D:\Projects\...
func NormalizePath(path string) string {
	if path == "" {
		return path
	}

	// Detect WSL path format: /mnt/<drive>/<rest of path>
	if strings.HasPrefix(path, "/mnt/") && len(path) > 6 {
		// Extract drive letter (e.g., /mnt/c/ -> c)
		parts := strings.SplitN(path[5:], "/", 2)
		if len(parts) >= 1 && len(parts[0]) == 1 {
			driveLetter := strings.ToUpper(parts[0])

			// If running on Windows, convert to Windows path
			if os.PathSeparator == '\\' {
				remainder := ""
				if len(parts) > 1 {
					remainder = parts[1]
					// Convert forward slashes to backslashes
					remainder = filepath.FromSlash(remainder)
				}
				return driveLetter + ":\\" + remainder
			}
			// If running on Linux/WSL, keep as is
			return filepath.Clean(path)
		}
	}

	// Detect Windows absolute path format: C:\... or C:/...
	if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		driveLetter := strings.ToLower(string(path[0]))

		// If running on Linux/WSL, convert to WSL path
		if os.PathSeparator == '/' {
			remainder := path[3:]
			// Convert backslashes to forward slashes
			remainder = filepath.ToSlash(remainder)
			return "/mnt/" + driveLetter + "/" + remainder
		}
		// If running on Windows, normalize separators
		return filepath.Clean(path)
	}

	// Handle pure Linux/WSL paths (e.g. /tmp/..., /home/...) when running on Windows.
	// filepath.Clean() would corrupt them to \tmp\... which is not a valid Windows path.
	// Convert to a UNC path \\wsl.localhost\<distro>\<path> so the file is accessible.
	if os.PathSeparator == '\\' && len(path) > 0 && path[0] == '/' {
		if distro := getDefaultWSLDistro(); distro != "" {
			// filepath.FromSlash turns /tmp/foo into \tmp\foo; prepend the UNC prefix.
			return `\\wsl.localhost\` + distro + filepath.FromSlash(path)
		}
		// WSL not available: return unchanged so error messages stay meaningful
		// (e.g. "source does not exist: /tmp/..." instead of "\tmp\...").
		return path
	}

	// For relative paths or already-normalized paths, just clean
	return filepath.Clean(path)
}

// secureRandomSuffix generates a cryptographically random hex suffix for temp files.
// This prevents symlink attacks on predictable temp file names.
func secureRandomSuffix() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based if crypto/rand fails (should never happen)
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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

// CaptureLastArtifact stores the most recent artifact
func (e *UltraFastEngine) CaptureLastArtifact(ctx context.Context, content string) error {
	e.artifactMutex.Lock()
	defer e.artifactMutex.Unlock()

	e.lastArtifact = content
	return nil
}

// WriteLastArtifact writes the last captured artifact to specified path
func (e *UltraFastEngine) WriteLastArtifact(ctx context.Context, path string) error {
	e.artifactMutex.RLock()
	content := e.lastArtifact
	e.artifactMutex.RUnlock()

	if content == "" {
		return fmt.Errorf("no artifact captured")
	}

	return e.WriteFileContent(ctx, path, content)
}

// GetLastArtifactInfo returns info about the last captured artifact
func (e *UltraFastEngine) GetLastArtifactInfo() string {
	e.artifactMutex.RLock()
	defer e.artifactMutex.RUnlock()

	if e.lastArtifact == "" {
		return "No artifact captured"
	}

	lines := strings.Count(e.lastArtifact, "\n") + 1
	return fmt.Sprintf("Last artifact: %d bytes, %d lines", len(e.lastArtifact), lines)
}

// IsCompactMode returns whether compact mode is enabled
func (e *UltraFastEngine) IsCompactMode() bool {
	return e.config.CompactMode
}

// GetAllowedPaths returns the configured allowed paths
func (e *UltraFastEngine) GetAllowedPaths() []string {
	return e.config.AllowedPaths
}

// ListDirectoryTree returns a recursive JSON tree structure of a directory
func (e *UltraFastEngine) ListDirectoryTree(ctx context.Context, path string, maxDepth int) (string, error) {
	path = NormalizePath(path)

	if err := e.acquireOperation(ctx, "tree"); err != nil {
		return "", err
	}
	start := time.Now()
	defer e.releaseOperation("tree", start)

	if !e.IsPathAllowed(path) {
		return "", fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
	}

	type TreeNode struct {
		Name     string      `json:"name"`
		Type     string      `json:"type"`
		Size     int64       `json:"size,omitempty"`
		Children []*TreeNode `json:"children,omitempty"`
	}

	var buildTree func(dirPath string, depth int) (*TreeNode, error)
	buildTree = func(dirPath string, depth int) (*TreeNode, error) {
		if depth > maxDepth {
			return nil, nil
		}

		info, err := os.Stat(dirPath)
		if err != nil {
			return nil, err
		}

		node := &TreeNode{
			Name: filepath.Base(dirPath),
			Type: "file",
			Size: info.Size(),
		}

		if !info.IsDir() {
			return node, nil
		}

		node.Type = "directory"
		node.Size = 0

		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return node, nil
		}

		node.Children = make([]*TreeNode, 0, len(entries))
		for _, entry := range entries {
			childPath := filepath.Join(dirPath, entry.Name())
			if entry.IsDir() {
				child, err := buildTree(childPath, depth+1)
				if err == nil && child != nil {
					node.Children = append(node.Children, child)
				}
			} else {
				childInfo, err := entry.Info()
				if err == nil {
					node.Children = append(node.Children, &TreeNode{
						Name: entry.Name(),
						Type: "file",
						Size: childInfo.Size(),
					})
				}
			}
		}
		return node, nil
	}

	tree, err := buildTree(path, 0)
	if err != nil {
		return "", fmt.Errorf("failed to build directory tree: %w", err)
	}

	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal tree: %w", err)
	}

	return string(data), nil
}

// GetMaxResponseSize returns max response size
func (e *UltraFastEngine) GetMaxResponseSize() int64 {
	return e.config.MaxResponseSize
}

// GetMaxSearchResults returns max search results
func (e *UltraFastEngine) GetMaxSearchResults() int {
	return e.config.MaxSearchResults
}

// GetMaxListItems returns max list items
func (e *UltraFastEngine) GetMaxListItems() int {
	return e.config.MaxListItems
}

// IntelligentWrite wraps optimizer's IntelligentWrite method
func (e *UltraFastEngine) IntelligentWrite(ctx context.Context, path, content string) error {
	return e.optimizer.IntelligentWrite(ctx, path, content)
}

// IntelligentRead wraps optimizer's IntelligentRead method
func (e *UltraFastEngine) IntelligentRead(ctx context.Context, path string) (string, error) {
	return e.optimizer.IntelligentRead(ctx, path)
}

// IntelligentEdit wraps optimizer's IntelligentEdit method
func (e *UltraFastEngine) IntelligentEdit(ctx context.Context, path, oldText, newText string, force bool) (*EditResult, error) {
	return e.optimizer.IntelligentEdit(ctx, path, oldText, newText, force)
}

// CopyFileWithBuffer copies a file using the buffer pool for optimal performance
// Uses io.CopyBuffer with a 64KB buffer from the pool
func (e *UltraFastEngine) CopyFileWithBuffer(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return &PathError{Op: "copy", Path: src, Err: err}
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return &PathError{Op: "copy", Path: dst, Err: err}
	}
	defer destFile.Close()

	// Get buffer from pool
	bufPtr := e.bufferPool.Get().(*[]byte)
	defer e.bufferPool.Put(bufPtr)

	// Use io.CopyBuffer with pooled buffer for efficient copying
	_, err = io.CopyBuffer(destFile, sourceFile, *bufPtr)
	if err != nil {
		return &PathError{Op: "copy", Path: dst, Err: err}
	}

	return nil
}

// GetOptimizationReport wraps optimizer's GetPerformanceReport method
func (e *UltraFastEngine) GetOptimizationReport() string {
	return e.optimizer.GetPerformanceReport()
}

// AutoRecoveryEdit wraps optimizer's AutoRecoveryEdit method
func (e *UltraFastEngine) AutoRecoveryEdit(ctx context.Context, path, oldText, newText string, force bool) (*EditResult, error) {
	return e.optimizer.AutoRecoveryEdit(ctx, path, oldText, newText, force)
}

// GetOptimizationSuggestion wraps optimizer's GetOptimizationSuggestion method
func (e *UltraFastEngine) GetOptimizationSuggestion(ctx context.Context, path string) (string, error) {
	return e.optimizer.GetOptimizationSuggestion(ctx, path)
}

// LogEditTelemetry records telemetry about edit operations to help identify
// full-file rewrites vs targeted edits. This helps optimize Claude's usage
// of the tools.
func (e *UltraFastEngine) LogEditTelemetry(oldTextSize, newTextSize int64, filePath string) {
	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()

	e.metrics.EditOperations++
	e.metrics.LastEditBytesSent = oldTextSize

	// Detect edit pattern
	isTargeted := oldTextSize < 100 && oldTextSize > 0         // Small targeted edit
	isFullRewrite := oldTextSize > 1000 || newTextSize > 10000 // Large or new content

	if isTargeted {
		e.metrics.TargetedEdits++
		e.metrics.LastEditOperation = fmt.Sprintf("✅ Targeted edit: %d bytes", oldTextSize)
	} else if isFullRewrite {
		e.metrics.FullFileRewrites++
		e.metrics.LastEditOperation = fmt.Sprintf("⚠️ Full rewrite detected: %d bytes", oldTextSize)
	} else {
		e.metrics.LastEditOperation = fmt.Sprintf("📝 Standard edit: %d bytes", oldTextSize)
	}

	// Update running average
	if e.metrics.EditOperations == 1 {
		e.metrics.AverageBytesPerEdit = float64(oldTextSize)
	} else {
		e.metrics.AverageBytesPerEdit = (e.metrics.AverageBytesPerEdit +
			float64(oldTextSize)) / 2
	}

	// Log to debug if verbose
	if e.config.DebugMode {
		slog.Debug("Edit telemetry", "operation", e.metrics.LastEditOperation, "avg_bytes_per_edit", e.metrics.AverageBytesPerEdit)
	}
}

// GetEditTelemetrySummary returns a summary of edit patterns for analysis
func (e *UltraFastEngine) GetEditTelemetrySummary() map[string]interface{} {
	e.metrics.mu.RLock()
	defer e.metrics.mu.RUnlock()

	totalEdits := e.metrics.EditOperations
	if totalEdits == 0 {
		return map[string]interface{}{
			"message": "No edit operations recorded yet",
		}
	}

	targetedPercent := float64(e.metrics.TargetedEdits) / float64(totalEdits) * 100
	rewritePercent := float64(e.metrics.FullFileRewrites) / float64(totalEdits) * 100

	return map[string]interface{}{
		"total_edits":            e.metrics.EditOperations,
		"targeted_edits":         e.metrics.TargetedEdits,
		"targeted_percent":       fmt.Sprintf("%.1f%%", targetedPercent),
		"full_rewrites":          e.metrics.FullFileRewrites,
		"full_rewrite_percent":   fmt.Sprintf("%.1f%%", rewritePercent),
		"average_bytes_per_edit": fmt.Sprintf("%.0f", e.metrics.AverageBytesPerEdit),
		"last_operation":         e.metrics.LastEditOperation,
		"recommendation": map[string]string{
			"if_high_rewrites": "Consider using search_files + read_file + edit_file for surgical edits",
			"if_low_targeted":  "Good! Edits are efficient",
			"next_step":        "Monitor these metrics to optimize Claude's tool usage",
		},
	}
}

// GetAutoSyncConfig returns the current auto-sync configuration
func (e *UltraFastEngine) GetAutoSyncConfig() AutoSyncConfig {
	if e.autoSyncManager == nil {
		return *DefaultAutoSyncConfig()
	}
	return e.autoSyncManager.GetConfig()
}

// SetAutoSyncConfig updates the auto-sync configuration
func (e *UltraFastEngine) SetAutoSyncConfig(config AutoSyncConfig) error {
	if e.autoSyncManager == nil {
		return fmt.Errorf("auto-sync manager not initialized")
	}
	return e.autoSyncManager.UpdateConfig(&config)
}

// GetAutoSyncStatus returns the current auto-sync status
func (e *UltraFastEngine) GetAutoSyncStatus() map[string]interface{} {
	if e.autoSyncManager == nil {
		return map[string]interface{}{
			"enabled": false,
			"error":   "auto-sync manager not initialized",
		}
	}
	return e.autoSyncManager.GetStatus()
}

// GetBackupManager returns the backup manager instance
func (e *UltraFastEngine) GetBackupManager() *BackupManager {
	return e.backupManager
}

// GetCurrentBackupID returns the current backup ID in the undo chain for a file
func (e *UltraFastEngine) GetCurrentBackupID(path string) string {
	e.backupChainMu.RLock()
	defer e.backupChainMu.RUnlock()
	return e.backupChain[path]
}

// SetCurrentBackupID updates the current backup ID in the undo chain for a file
func (e *UltraFastEngine) SetCurrentBackupID(path, backupID string) {
	e.backupChainMu.Lock()
	defer e.backupChainMu.Unlock()
	e.backupChain[path] = backupID
}

// ClearBackupID removes the backup chain entry for a file
func (e *UltraFastEngine) ClearBackupID(path string) {
	e.backupChainMu.Lock()
	defer e.backupChainMu.Unlock()
	delete(e.backupChain, path)
}

// FileIntegrityResult holds the result of a file integrity verification
type FileIntegrityResult struct {
	OK           bool   `json:"ok"`
	Lines        int    `json:"lines"`
	SizeBytes    int64  `json:"size_bytes"`
	Readable     bool   `json:"readable"`
	Hash         string `json:"hash,omitempty"`
	Warning      string `json:"warning,omitempty"`
	Verification string `json:"verification"` // "OK", "WARNING", "ERROR"
}

// VerifyFileIntegrity performs a lightweight integrity check after HIGH/CRITICAL edits
// Checks: file is readable, size is reasonable, line count is reasonable
func (e *UltraFastEngine) VerifyFileIntegrity(path string, expectedChangePct float64) *FileIntegrityResult {
	result := &FileIntegrityResult{
		Verification: "OK",
	}

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		result.OK = false
		result.Verification = "ERROR"
		result.Warning = fmt.Sprintf("Cannot stat file: %v", err)
		return result
	}

	result.SizeBytes = info.Size()
	result.Readable = true

	// Read file content to verify it's intact and get line count
	content, err := os.ReadFile(path)
	if err != nil {
		result.OK = false
		result.Verification = "ERROR"
		result.Warning = fmt.Sprintf("Cannot read file: %v", err)
		return result
	}

	result.Lines = strings.Count(string(content), "\n")
	if !strings.HasSuffix(string(content), "\n") {
		result.Lines++
	}

	// Basic hash for reference (CRC32 for speed)
	result.Hash = fmt.Sprintf("%x", crc32.ChecksumIEEE(content))

	// Check for suspiciously small file after high-change operation
	if expectedChangePct > 50 && info.Size() < 100 {
		result.OK = false
		result.Verification = "WARNING"
		result.Warning = fmt.Sprintf("File is only %d bytes after a %.0f%% change — verify content", info.Size(), expectedChangePct)
		return result
	}

	// Check for truncation (empty file after non-trivial edit)
	if expectedChangePct > 30 && info.Size() == 0 {
		result.OK = false
		result.Verification = "ERROR"
		result.Warning = "File is empty after a non-trivial edit"
		return result
	}

	result.OK = true
	return result
}

// ExecutePipeline executes a multi-step file transformation pipeline
// This is a convenience wrapper around PipelineExecutor
func (e *UltraFastEngine) ExecutePipeline(ctx context.Context, request PipelineRequest) (*PipelineResult, error) {
	executor := NewPipelineExecutor(e)
	return executor.Execute(ctx, request)
}
