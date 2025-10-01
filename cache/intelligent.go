package cache

import (
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
	maxSize int64
	currentSize int64
	mu sync.RWMutex
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	mu sync.RWMutex
	
	// Hit/miss counters
	FileHits     int64
	FileMisses   int64
	DirHits      int64
	DirMisses    int64
	MetaHits     int64
	MetaMisses   int64
	
	// Eviction counters
	Evictions    int64
	
	// Timing stats
	LastAccess   time.Time
	TotalAccesses int64
}

// NewIntelligentCache creates a new intelligent cache system
func NewIntelligentCache(maxSize int64) (*IntelligentCache, error) {
	// Initialize bigcache for file content
	bigConfig := bigcache.Config{
		Shards:             1024,
		LifeWindow:         10 * time.Minute,
		CleanWindow:        2 * time.Minute,
		MaxEntriesInWindow: 1000 * 10 * 1024, // Adjust based on expected entries
		MaxEntrySize:       500,              // Max size per entry in bytes, adjust
		Verbose:            false,
	}
	// Approximate max size: MaxEntriesInWindow * MaxEntrySize ≈ maxSize / 2
	bigConfig.MaxEntriesInWindow = int((maxSize / 2) / int64(bigConfig.MaxEntrySize))
	fileCache, err := bigcache.NewBigCache(bigConfig)
	if err != nil {
		return nil, err
	}
	
	// Default expiration: 5 minutes for directories, 15 for meta
	dirCache := gocache.New(5*time.Minute, 2*time.Minute)
	metaCache := gocache.New(15*time.Minute, 2*time.Minute)
	
	cache := &IntelligentCache{
		fileCache:   fileCache,
		dirCache:    dirCache,
		metaCache:   metaCache,
		stats:       &CacheStats{},
		maxSize:     maxSize,
		currentSize: 0,
	}
	
	// Set up eviction callbacks (bigcache doesn't have direct OnEvicted, but we can track via stats)
	dirCache.OnEvicted(cache.onDirEvicted)
	metaCache.OnEvicted(cache.onMetaEvicted)
	
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

// GetDirectory retrieves a directory listing from cache
func (c *IntelligentCache) GetDirectory(path string) (string, bool) {
	c.updateAccessStats()
	
	if item, found := c.dirCache.Get(path); found {
		c.stats.mu.Lock()
		c.stats.DirHits++
		c.stats.mu.Unlock()
		
		// Update access time
		c.dirCache.Set(path, item, gocache.DefaultExpiration)
		
		return item.(string), true
	}
	
	c.stats.mu.Lock()
	c.stats.DirMisses++
	c.stats.mu.Unlock()
	
	return "", false
}

// SetDirectory stores a directory listing in cache
func (c *IntelligentCache) SetDirectory(path string, listing string) {
	c.dirCache.Set(path, listing, gocache.DefaultExpiration)
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

// GetStats returns detailed cache statistics
func (c *IntelligentCache) GetStats() CacheStats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()
	return *c.stats
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
	err := c.fileCache.Close()
	c.Flush()
	return err
}
