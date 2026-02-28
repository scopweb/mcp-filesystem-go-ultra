package cache

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/allegro/bigcache/v3"
	gocache "github.com/patrickmn/go-cache"
)

// IntelligentCache provides high-performance caching with intelligent eviction
type IntelligentCache struct {
	// File content cache using bigcache for better performance
	fileCache *bigcache.BigCache

	// Directory listing cache
	dirCache *gocache.Cache

	// Metadata cache (file info, stats, etc.)
	metaCache *gocache.Cache

	// Cache statistics
	stats *CacheStats

	// Configuration
	maxSize     int64
	currentSize int64
	mu          sync.RWMutex

	// Prefetch tracking for predictive caching
	accessPattern map[string]int64 // path -> access count
	prefetchQueue chan string      // paths to prefetch
	patternMu     sync.RWMutex
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	mu sync.RWMutex

	// Hit/miss counters
	FileHits   int64
	FileMisses int64
	DirHits    int64
	DirMisses  int64
	MetaHits   int64
	MetaMisses int64

	// Eviction counters
	Evictions int64

	// Timing stats
	LastAccess    time.Time
	TotalAccesses int64
}

// NewIntelligentCache creates a new intelligent cache system
func NewIntelligentCache(maxSize int64) (*IntelligentCache, error) {
	// Initialize bigcache for file content with optimized settings
	bigConfig := bigcache.Config{
		Shards:             256,                    // Reduced from 1024 for better overhead balance
		LifeWindow:         3 * time.Minute,        // Reduced from 10min for faster cache refresh
		CleanWindow:        1 * time.Minute,        // Balanced with LifeWindow
		MaxEntriesInWindow: 1000 * 10 * 1024,      // Adjust based on expected entries
		MaxEntrySize:       1024 * 1024,            // 1MB per entry (was 500 bytes!)
		Verbose:            false,
		HardMaxCacheSize:   int(maxSize / (1024 * 1024)), // Convert to MB
	}
	// Approximate max size: MaxEntriesInWindow * MaxEntrySize ≈ maxSize / 2
	bigConfig.MaxEntriesInWindow = int((maxSize / 2) / int64(bigConfig.MaxEntrySize))
	fileCache, err := bigcache.NewBigCache(bigConfig)
	if err != nil {
		return nil, err
	}

	// Shorter expiration for faster cache refresh - optimized for Claude Desktop
	dirCache := gocache.New(3*time.Minute, 1*time.Minute)
	metaCache := gocache.New(10*time.Minute, 2*time.Minute)

	cache := &IntelligentCache{
		fileCache:     fileCache,
		dirCache:      dirCache,
		metaCache:     metaCache,
		stats:         &CacheStats{},
		maxSize:       maxSize,
		currentSize:   0,
		accessPattern: make(map[string]int64),
		prefetchQueue: make(chan string, 100), // Buffer for prefetch requests
	}

	// Set up eviction callbacks (bigcache doesn't have direct OnEvicted, but we can track via stats)
	dirCache.OnEvicted(cache.onDirEvicted)
	metaCache.OnEvicted(cache.onMetaEvicted)

	// Start prefetch worker goroutine
	go cache.prefetchWorker()

	return cache, nil
}

// GetFile retrieves a file from cache
func (c *IntelligentCache) GetFile(path string) ([]byte, bool) {
	c.updateAccessStats()

	item, err := c.fileCache.Get(path)
	if err == nil {
		c.stats.mu.Lock()
		c.stats.FileHits++
		c.stats.mu.Unlock()
		return item, true
	}

	c.stats.mu.Lock()
	c.stats.FileMisses++
	c.stats.mu.Unlock()

	return nil, false
}

// SetFile stores a file in cache with intelligent size management
func (c *IntelligentCache) SetFile(path string, content []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Bigcache handles size and eviction automatically
	err := c.fileCache.Set(path, content)
	if err == nil {
		c.currentSize += int64(len(content)) // Approximate tracking
	}
}

// dirCacheEntry pairs a directory listing with the directory's mtime at cache time.
// The mtime is used by callers to detect external modifications (e.g. bash writes).
type dirCacheEntry struct {
	Listing string
	Mtime   time.Time
}

// GetDirectory retrieves a directory listing from cache.
// Returns the listing, the directory mtime recorded when the entry was cached,
// and whether it was a cache hit.
func (c *IntelligentCache) GetDirectory(path string) (string, time.Time, bool) {
	c.updateAccessStats()

	if item, found := c.dirCache.Get(path); found {
		c.stats.mu.Lock()
		c.stats.DirHits++
		c.stats.mu.Unlock()

		entry := item.(dirCacheEntry)
		// Refresh TTL without changing the stored mtime
		c.dirCache.Set(path, entry, gocache.DefaultExpiration)

		return entry.Listing, entry.Mtime, true
	}

	c.stats.mu.Lock()
	c.stats.DirMisses++
	c.stats.mu.Unlock()

	return "", time.Time{}, false
}

// SetDirectory stores a directory listing in cache together with the directory's
// current mtime so that stale entries can be detected on the next read.
func (c *IntelligentCache) SetDirectory(path string, listing string, mtime time.Time) {
	c.dirCache.Set(path, dirCacheEntry{Listing: listing, Mtime: mtime}, gocache.DefaultExpiration)
}

// GetMetadata retrieves metadata from cache
func (c *IntelligentCache) GetMetadata(key string) (interface{}, bool) {
	c.updateAccessStats()

	if item, found := c.metaCache.Get(key); found {
		c.stats.mu.Lock()
		c.stats.MetaHits++
		c.stats.mu.Unlock()

		return item, true
	}

	c.stats.mu.Lock()
	c.stats.MetaMisses++
	c.stats.mu.Unlock()

	return nil, false
}

// SetMetadata stores metadata in cache
func (c *IntelligentCache) SetMetadata(key string, value interface{}) {
	c.metaCache.Set(key, value, gocache.DefaultExpiration)
}

// InvalidateFile removes a file from cache
func (c *IntelligentCache) InvalidateFile(path string) {
	err := c.fileCache.Delete(path)
	if err == nil {
		// Approximate size update
		c.mu.Lock()
		// Note: Without exact size, we might need to adjust tracking
		c.currentSize -= 0 // Placeholder; bigcache doesn't provide evicted size
		c.mu.Unlock()
	}
}

// InvalidateDirectory removes a directory listing from cache
func (c *IntelligentCache) InvalidateDirectory(path string) {
	c.dirCache.Delete(path)
}

// InvalidateMetadata removes metadata from cache
func (c *IntelligentCache) InvalidateMetadata(key string) {
	c.metaCache.Delete(key)
}

// evictToMakeSpace is no longer needed with bigcache automatic eviction

// updateAccessStats updates access statistics
func (c *IntelligentCache) updateAccessStats() {
	c.stats.mu.Lock()
	c.stats.TotalAccesses++
	c.stats.LastAccess = time.Now()
	c.stats.mu.Unlock()
}

// GetHitRate calculates the overall cache hit rate
func (c *IntelligentCache) GetHitRate() float64 {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()

	totalHits := c.stats.FileHits + c.stats.DirHits + c.stats.MetaHits
	totalMisses := c.stats.FileMisses + c.stats.DirMisses + c.stats.MetaMisses
	total := totalHits + totalMisses

	if total == 0 {
		return 0.0
	}

	return float64(totalHits) / float64(total)
}

// GetMemoryUsage returns current memory usage in bytes (approximate for bigcache)
func (c *IntelligentCache) GetMemoryUsage() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize + int64(c.fileCache.Capacity()) // Use bigcache capacity as estimate
}

// GetStats returns detailed cache statistics (copy without mutex)
func (c *IntelligentCache) GetStats() CacheStats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()
	// Return a copy without the mutex
	return CacheStats{
		FileHits:      c.stats.FileHits,
		FileMisses:    c.stats.FileMisses,
		DirHits:       c.stats.DirHits,
		DirMisses:     c.stats.DirMisses,
		MetaHits:      c.stats.MetaHits,
		MetaMisses:    c.stats.MetaMisses,
		Evictions:     c.stats.Evictions,
		LastAccess:    c.stats.LastAccess,
		TotalAccesses: c.stats.TotalAccesses,
	}
}

// Eviction callbacks for non-bigcache caches

func (c *IntelligentCache) onDirEvicted(key string, value interface{}) {
	// Directory listings are typically small, but we still track evictions
	c.stats.mu.Lock()
	c.stats.Evictions++
	c.stats.mu.Unlock()
}

func (c *IntelligentCache) onMetaEvicted(key string, value interface{}) {
	// Metadata is typically small, but we still track evictions
	c.stats.mu.Lock()
	c.stats.Evictions++
	c.stats.mu.Unlock()
}

// Flush clears all caches
func (c *IntelligentCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.fileCache.Reset()
	c.dirCache.Flush()
	c.metaCache.Flush()
	c.currentSize = 0
}

// Close gracefully shuts down the cache
func (c *IntelligentCache) Close() error {
	close(c.prefetchQueue) // Signal prefetch worker to stop
	err := c.fileCache.Close()
	c.Flush()
	return err
}

// prefetchWorker runs in background to prefetch frequently accessed files
func (c *IntelligentCache) prefetchWorker() {
	for path := range c.prefetchQueue {
		// Only prefetch if not already cached
		if _, hit := c.GetFile(path); !hit {
			// Prefetch file content in background
			// This is a hint to the OS to read the file into page cache
			_ = c.prefetchFile(path)
		}
	}
}

// prefetchFile reads a file into cache in the background
func (c *IntelligentCache) prefetchFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c.SetFile(path, content)
	return nil
}

// TrackAccess records file access pattern for predictive prefetching
func (c *IntelligentCache) TrackAccess(path string) {
	c.patternMu.Lock()
	c.accessPattern[path]++
	count := c.accessPattern[path]
	c.patternMu.Unlock()

	// After 3 accesses, consider prefetching related files
	if count >= 3 {
		c.suggestPrefetch(path)
	}
}

// suggestPrefetch suggests files to prefetch based on access patterns
func (c *IntelligentCache) suggestPrefetch(path string) {
	// Get directory of accessed file
	dir := filepath.Dir(path)

	// Try to prefetch sibling files (common pattern in code editing)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Prefetch up to 3 related files
	prefetched := 0
	for _, entry := range entries {
		if entry.IsDir() || prefetched >= 3 {
			continue
		}

		siblingPath := filepath.Join(dir, entry.Name())
		if siblingPath == path {
			continue
		}

		// Check file size - only prefetch small files
		info, err := entry.Info()
		if err != nil || info.Size() > 100*1024 {
			continue
		}

		// Non-blocking send to prefetch queue
		select {
		case c.prefetchQueue <- siblingPath:
			prefetched++
		default:
			// Queue full, skip
		}
	}
}

// GetAccessStats returns access pattern statistics
func (c *IntelligentCache) GetAccessStats() map[string]int64 {
	c.patternMu.RLock()
	defer c.patternMu.RUnlock()

	// Return copy to avoid race conditions
	stats := make(map[string]int64, len(c.accessPattern))
	for k, v := range c.accessPattern {
		stats[k] = v
	}
	return stats
}
