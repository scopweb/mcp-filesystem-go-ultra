package core

import (
	"context"
	"fmt"
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
	CompactMode      bool  // Enable compact responses
	MaxResponseSize  int64 // Max response size
	MaxSearchResults int   // Max search results
	MaxListItems     int   // Max list items
	HooksConfigPath  string // Path to hooks configuration file
	HooksEnabled     bool   // Enable hooks system
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

	// Log if allowed paths are configured
	if len(config.AllowedPaths) > 0 {
		log.Printf("🔒 Access control enabled with %d allowed paths", len(config.AllowedPaths))
	} else {
		log.Printf("⚠️ Access control disabled - full filesystem access allowed")
	}

	// Initialize worker pool for parallel operations
	workerPool, err := ants.NewPool(config.ParallelOps, ants.WithPreAlloc(true))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize worker pool: %v", err)
	}
	engine.workerPool = workerPool

	log.Printf("🔧 Ultra-fast engine initialized with %d parallel operations", config.ParallelOps)

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

	return engine, nil
}

// Close gracefully shuts down the engine
func (e *UltraFastEngine) Close() error {
	if e.workerPool != nil {
		e.workerPool.Release()
	}
	return nil
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
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "read"); err != nil {
		return "", err
	}

	start := time.Now()
	defer e.releaseOperation("read", start)

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return "", fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
		}
	}

	// Try cache first
	if cached, hit := e.cache.GetFile(path); hit {
		if e.config.DebugMode {
			log.Printf("📦 Cache hit for %s", path)
		}
		return string(cached), nil
	}

	// Read from disk
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("file read error: %v", err)
	}

	// Cache the content
	e.cache.SetFile(path, content)

	return string(content), nil
}

// WriteFileContent implements atomic file writing
func (e *UltraFastEngine) WriteFileContent(ctx context.Context, path, content string) error {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "write"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("write", start)

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
		}
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
		return fmt.Errorf("pre-write hook denied operation: %v", err)
	}

	// Use modified content if hook provided it (e.g., formatted code)
	finalContent := content
	if hookResult.ModifiedContent != "" {
		finalContent = hookResult.ModifiedContent
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Atomic write using temp file
	tmpPath := path + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())

	// Write to temporary file
	if err := os.WriteFile(tmpPath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("failed to rename temp file: %v", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	// Execute post-write hooks
	hookCtx.Event = HookPostWrite
	hookCtx.Content = finalContent
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostWrite, hookCtx)

	return nil
}

// ListDirectoryContent implements intelligent directory listing with caching
func (e *UltraFastEngine) ListDirectoryContent(ctx context.Context, path string) (string, error) {
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
		return "", fmt.Errorf("failed to read directory: %v", err)
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
func (e *UltraFastEngine) IntelligentEdit(ctx context.Context, path, oldText, newText string) (*EditResult, error) {
	return e.optimizer.IntelligentEdit(ctx, path, oldText, newText)
}

// GetOptimizationReport wraps optimizer's GetPerformanceReport method
func (e *UltraFastEngine) GetOptimizationReport() string {
	return e.optimizer.GetPerformanceReport()
}

// AutoRecoveryEdit wraps optimizer's AutoRecoveryEdit method
func (e *UltraFastEngine) AutoRecoveryEdit(ctx context.Context, path, oldText, newText string) (*EditResult, error) {
	return e.optimizer.AutoRecoveryEdit(ctx, path, oldText, newText)
}

// GetOptimizationSuggestion wraps optimizer's GetOptimizationSuggestion method
func (e *UltraFastEngine) GetOptimizationSuggestion(ctx context.Context, path string) (string, error) {
	return e.optimizer.GetOptimizationSuggestion(ctx, path)
}
