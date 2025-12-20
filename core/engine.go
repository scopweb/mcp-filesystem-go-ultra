package core

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
}

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
	engine := &UltraFastEngine{
		config:    config,
		cache:     config.Cache,
		metrics:   &PerformanceMetrics{},
		semaphore: make(chan struct{}, config.ParallelOps),
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

	// Log if allowed paths are configured
	if len(config.AllowedPaths) > 0 {
		log.Printf("🔒 Access control enabled with %d allowed paths", len(config.AllowedPaths))
	} else {
		log.Printf("⚠️ Access control disabled - full filesystem access allowed")
	}

	// Initialize worker pool for parallel operations
	workerPool, err := ants.NewPool(config.ParallelOps, ants.WithPreAlloc(true))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize worker pool: %w", err)
	}
	engine.workerPool = workerPool

	log.Printf("🔧 Ultra-fast engine initialized with %d parallel operations (64KB buffer pool)", config.ParallelOps)

	// Initialize Claude Desktop optimizer
	engine.optimizer = NewClaudeDesktopOptimizer(engine)
	log.Printf("🧠 Claude Desktop optimizer initialized")

	// Initialize hook manager
	engine.hookManager = NewHookManager()
	if config.HooksEnabled && config.HooksConfigPath != "" {
		if err := engine.hookManager.LoadConfig(config.HooksConfigPath); err != nil {
			log.Printf("⚠️ Failed to load hooks config: %v (hooks disabled)", err)
		} else {
			engine.hookManager.SetEnabled(true)
			engine.hookManager.SetDebugMode(config.DebugMode)
			log.Printf("🪝 Hook system initialized")
		}
	}

	// Initialize auto-sync manager
	engine.autoSyncManager = NewAutoSyncManager()
	if engine.autoSyncManager.IsEnabled() {
		log.Printf("🔄 WSL auto-sync enabled")
	} else {
		isWSL, _ := DetectEnvironment()
		if isWSL {
			log.Printf("💡 WSL detected. Auto-sync is disabled. Enable with: configure_autosync or set MCP_WSL_AUTOSYNC=true")
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
		log.Printf("⚠️ Failed to initialize backup manager: %v (backups disabled)", err)
	} else {
		engine.backupManager = backupManager
		log.Printf("🔒 Backup manager initialized: %s (max age: %d days, max count: %d)",
			backupManager.backupDir, backupMaxAge, backupMaxCount)
	}

	// Initialize risk thresholds
	if config.RiskThresholdMedium > 0 {
		engine.riskThresholds.MediumPercentage = config.RiskThresholdMedium
	} else {
		engine.riskThresholds.MediumPercentage = 30.0
	}
	if config.RiskThresholdHigh > 0 {
		engine.riskThresholds.HighPercentage = config.RiskThresholdHigh
	} else {
		engine.riskThresholds.HighPercentage = 50.0
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

	return engine, nil
}

// Close gracefully shuts down the engine
func (e *UltraFastEngine) Close() error {
	if e.workerPool != nil {
		e.workerPool.Release()
	}
	return nil
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

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return "", &PathError{Op: "read", Path: path, Err: fmt.Errorf("access denied")}
		}
	}

	// Try cache first
	if cached, hit := e.cache.GetFile(path); hit {
		if e.config.DebugMode {
			log.Printf("📦 Cache hit for %s", path)
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

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return &PathError{Op: "write", Path: path, Err: fmt.Errorf("access denied")}
		}
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

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Atomic write using temp file
	tmpPath := path + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())

	// Write to temporary file
	if err := os.WriteFile(tmpPath, []byte(finalContent), 0644); err != nil {
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

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return "", fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
		}
	}

	// Try cache first
	if cached, hit := e.cache.GetDirectory(path); hit {
		if e.config.DebugMode {
			log.Printf("📦 Directory cache hit for %s", path)
		}
		return cached, nil
	}

	// Read directory
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	// Build response - compact or verbose mode
	var result strings.Builder

	if e.config.CompactMode {
		// Compact mode: minimal format
		result.WriteString(path)
		result.WriteString(": ")

		maxItems := e.config.MaxListItems
		count := 0
		for _, entry := range entries {
			if count >= maxItems {
				result.WriteString(fmt.Sprintf("... (%d more)", len(entries)-count))
				break
			}

			if count > 0 {
				result.WriteString(", ")
			}

			if entry.IsDir() {
				result.WriteString(entry.Name())
				result.WriteString("/")
			} else {
				info, err := entry.Info()
				if err == nil && info.Size() > 1024 {
					result.WriteString(fmt.Sprintf("%s(%s)", entry.Name(), formatSize(info.Size())))
				} else {
					result.WriteString(entry.Name())
				}
			}
			count++
		}
	} else {
		// Verbose mode: detailed format
		result.WriteString(fmt.Sprintf("Directory listing for: %s\n\n", path))

		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}

			entryType := "[FILE]"
			if entry.IsDir() {
				entryType = "[DIR] "
			}

			result.WriteString(fmt.Sprintf("%s %s (file://%s) - %d bytes\n",
				entryType, entry.Name(), filepath.Join(path, entry.Name()), info.Size()))
		}

		result.WriteString(fmt.Sprintf("\nDirectory: %s", path))
	}

	responseText := result.String()

	// Cache the result
	e.cache.SetDirectory(path, responseText)

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

// isPathAllowed checks if the given path is within one of the allowed base paths
func (e *UltraFastEngine) isPathAllowed(path string) bool {
	// Resolve to absolute, cleaned paths to prevent traversal and casing issues
	targetAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// On Windows, compare case-insensitively
	norm := func(p string) string {
		p = filepath.Clean(p)
		if os.PathSeparator == '\\' { // Windows
			return strings.ToLower(p)
		}
		return p
	}

	targetAbs = norm(targetAbs)

	for _, allowed := range e.config.AllowedPaths {
		baseAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		baseAbs = norm(baseAbs)

		// Quick equality check
		if targetAbs == baseAbs {
			return true
		}

		// Safe containment check using filepath.Rel; ensures boundary-aware prefix
		rel, err := filepath.Rel(baseAbs, targetAbs)
		if err == nil {
			// Not outside if rel doesn't start with .. or ..<sep>
			if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return true
			}
		}
	}

	if e.config.DebugMode {
		log.Printf("🚫 Access denied to path: %s (not in allowed paths: %v)", path, e.config.AllowedPaths)
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

	// For relative paths or already-normalized paths, just clean
	return filepath.Clean(path)
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
		log.Printf("[TELEMETRY] %s (avg: %.0f bytes/edit)", e.metrics.LastEditOperation, e.metrics.AverageBytesPerEdit)
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
			"if_high_rewrites": "Consider using smart_search + read_file_range + edit_file for surgical edits",
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
