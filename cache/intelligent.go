package cache

import (
	"fmt"
	"os"
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
	maxSize       int64
	fileTTL       time.Duration
	residentBytes int64
	fileBytesLen  map[string]int64
	mu            sync.RWMutex

	accessPattern   map[string]int64
	prefetchQueue   chan string
	prefetchPending map[string]struct{}
	patternMu       sync.RWMutex
	prefetchMu      sync.Mutex
	authorizer      PrefetchAuthorizer
	authMu          sync.Mutex
	prefetchWG      sync.WaitGroup
	closed          bool
	closeOnce       sync.Once

	globalGen uint64
	pathGen   map[string]uint64
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

	StaleMisses     int64
	PrefetchHits    int64
	PrefetchMisses  int64
	PrefetchFills   int64
	CoalescedReads  int64
	DiskLoads       int64
	InternalLookups int64

	LastAccess    time.Time
	TotalAccesses int64
}

const DefaultFileTTL = 3 * time.Minute

func NewIntelligentCache(maxSize int64) (*IntelligentCache, error) {
	return NewIntelligentCacheTTL(maxSize, DefaultFileTTL)
}

func NewIntelligentCacheTTL(maxSize int64, fileTTL time.Duration) (*IntelligentCache, error) {
	if fileTTL < time.Second {
		return nil, fmt.Errorf("cache file TTL must be at least 1s")
	}
	clean := fileTTL / 3
	if clean < time.Second {
		clean = time.Second
	}
	if clean > time.Minute {
		clean = time.Minute
	}
	bigConfig := bigcache.Config{
		Shards:             256,
		LifeWindow:         fileTTL,
		CleanWindow:        clean,
		MaxEntriesInWindow: 1000 * 10 * 1024,
		MaxEntrySize:       1024 * 1024,
		Verbose:            false,
		HardMaxCacheSize:   int(maxSize / (1024 * 1024)),
	}
	bigConfig.MaxEntriesInWindow = int((maxSize / 2) / int64(bigConfig.MaxEntrySize))
	fileCache, err := bigcache.NewBigCache(bigConfig)
	if err != nil {
		return nil, err
	}

	dirCache := gocache.New(3*time.Minute, 1*time.Minute)
	metaCache := gocache.New(10*time.Minute, 2*time.Minute)

	cache := &IntelligentCache{
		fileCache:       fileCache,
		dirCache:        dirCache,
		metaCache:       metaCache,
		stats:           &CacheStats{},
		maxSize:         maxSize,
		fileTTL:         fileTTL,
		fileBytesLen:    make(map[string]int64),
		accessPattern:   make(map[string]int64),
		prefetchQueue:   make(chan string, prefetchQueueSize),
		prefetchPending: make(map[string]struct{}),
		pathGen:         make(map[string]uint64),
	}

	// Set up eviction callbacks (bigcache doesn't have direct OnEvicted, but we can track via stats)
	dirCache.OnEvicted(cache.onDirEvicted)
	metaCache.OnEvicted(cache.onMetaEvicted)

	cache.prefetchWG.Add(1)
	go cache.prefetchWorker()

	return cache, nil
}

func fileStatKey(path string) string { return "fstat:" + path }

func (c *IntelligentCache) GetFile(path string) ([]byte, bool) {
	return c.fileBytes(path)
}

func (c *IntelligentCache) fileBytes(path string) ([]byte, bool) {
	item, err := c.fileCache.Get(path)
	if err != nil {
		return nil, false
	}
	return item, true
}

func (c *IntelligentCache) fileMeta(path string) (FileStatMeta, bool) {
	item, ok := c.metaCache.Get(fileStatKey(path))
	if !ok {
		return FileStatMeta{}, false
	}
	meta, ok := item.(FileStatMeta)
	if !ok || !meta.Valid {
		return FileStatMeta{}, false
	}
	return meta, true
}

// GetFileFresh serves cached bytes only when capture metadata is present and
// still matches the original (size and mtime equality; file identity when the
// OS Stat exposes it). Missing metadata, Stat errors, and mismatches are a
// miss: the caller must read the original. Size+mtime cannot detect every
// in-place rewrite that preserves both; this is not an atomic snapshot against
// an arbitrary concurrent writer.
func (c *IntelligentCache) GetFileFresh(path string) ([]byte, bool) {
	return c.lookupFresh(path, lookupDemand)
}

func (c *IntelligentCache) PeekFileFresh(path string) ([]byte, bool) {
	return c.lookupFresh(path, lookupInternal)
}

type lookupClass int

const (
	lookupDemand lookupClass = iota
	lookupInternal
	lookupPrefetch
)

func (c *IntelligentCache) lookupFresh(path string, kind lookupClass) ([]byte, bool) {
	if kind == lookupDemand {
		c.updateAccessStats()
	}
	cached, hasBytes := c.fileBytes(path)
	meta, hasMeta := c.fileMeta(path)
	if !hasBytes || !hasMeta {
		if hasBytes || hasMeta {
			c.InvalidateFile(path)
		} else {
			c.dropResident(path)
		}
		c.recordLookup(kind, false, false)
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil {
		c.InvalidateFile(path)
		c.recordLookup(kind, false, false)
		return nil, false
	}
	live := MetaFromInfo(info)
	if !meta.sameVersion(live) || int64(len(cached)) != meta.Size {
		c.InvalidateFile(path)
		c.recordLookup(kind, false, true)
		return nil, false
	}
	c.recordLookup(kind, true, false)
	return cached, true
}

func (c *IntelligentCache) recordLookup(kind lookupClass, hit, stale bool) {
	c.stats.mu.Lock()
	defer c.stats.mu.Unlock()
	switch kind {
	case lookupInternal:
		c.stats.InternalLookups++
	case lookupPrefetch:
		if hit {
			c.stats.PrefetchHits++
		} else {
			c.stats.PrefetchMisses++
		}
	default:
		if hit {
			c.stats.FileHits++
		} else {
			c.stats.FileMisses++
			if stale {
				c.stats.StaleMisses++
			}
		}
	}
}

func (c *IntelligentCache) CaptureGeneration(path string) FileGen {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return FileGen{Global: c.globalGen, Local: c.pathGen[path]}
}

func (c *IntelligentCache) SetFile(path string, content []byte, meta FileStatMeta) {
	c.SetFileIfGeneration(path, content, meta, c.CaptureGeneration(path))
}

func (c *IntelligentCache) SetFileIfGeneration(path string, content []byte, meta FileStatMeta, gen FileGen) bool {
	if !meta.Valid || int64(len(content)) != meta.Size {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.globalGen != gen.Global || c.pathGen[path] != gen.Local {
		return false
	}
	if err := c.fileCache.Set(path, content); err != nil {
		return false
	}
	n := int64(len(content))
	if old, ok := c.fileBytesLen[path]; ok {
		c.residentBytes -= old
	}
	c.fileBytesLen[path] = n
	c.residentBytes += n
	c.metaCache.Set(fileStatKey(path), meta, c.fileTTL)
	return true
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

func (c *IntelligentCache) InvalidateFile(path string) {
	c.mu.Lock()
	c.pathGen[path]++
	if n, ok := c.fileBytesLen[path]; ok {
		c.residentBytes -= n
		delete(c.fileBytesLen, path)
	}
	c.mu.Unlock()
	_ = c.fileCache.Delete(path)
	c.metaCache.Delete(fileStatKey(path))
}

func (c *IntelligentCache) dropResident(path string) {
	c.mu.Lock()
	if n, ok := c.fileBytesLen[path]; ok {
		c.residentBytes -= n
		delete(c.fileBytesLen, path)
		c.stats.mu.Lock()
		c.stats.Evictions++
		c.stats.mu.Unlock()
	}
	c.mu.Unlock()
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

// GetHitMiss returns demand lookup counts after the freshness verdict
// (file) plus directory and metadata lookups. Unit: events.
// Prefetch and internal singleflight peeks are excluded.
func (c *IntelligentCache) GetHitMiss() (hits, misses int64) {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()
	hits = c.stats.FileHits + c.stats.DirHits + c.stats.MetaHits
	misses = c.stats.FileMisses + c.stats.DirMisses + c.stats.MetaMisses
	return
}

// GetHitRate is hits/(hits+misses) from GetHitMiss. Zero if no demand lookups.
func (c *IntelligentCache) GetHitRate() float64 {
	hits, misses := c.GetHitMiss()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

type MemoryStats struct {
	ResidentBytes int64
	CapacityBytes int64
}

func (c *IntelligentCache) Memory() MemoryStats {
	c.mu.RLock()
	resident := c.residentBytes
	c.mu.RUnlock()
	capBytes := int64(0)
	if c.fileCache != nil {
		capBytes = int64(c.fileCache.Capacity())
	}
	return MemoryStats{ResidentBytes: resident, CapacityBytes: capBytes}
}

// GetMemoryUsage returns tracked resident file-content bytes (not process RSS,
// not BigCache reserved capacity). Unit: bytes.
func (c *IntelligentCache) GetMemoryUsage() int64 {
	return c.Memory().ResidentBytes
}

func (c *IntelligentCache) GetStats() CacheStats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()
	return CacheStats{
		FileHits:        c.stats.FileHits,
		FileMisses:      c.stats.FileMisses,
		DirHits:         c.stats.DirHits,
		DirMisses:       c.stats.DirMisses,
		MetaHits:        c.stats.MetaHits,
		MetaMisses:      c.stats.MetaMisses,
		Evictions:       c.stats.Evictions,
		StaleMisses:     c.stats.StaleMisses,
		PrefetchHits:    c.stats.PrefetchHits,
		PrefetchMisses:  c.stats.PrefetchMisses,
		PrefetchFills:   c.stats.PrefetchFills,
		CoalescedReads:  c.stats.CoalescedReads,
		DiskLoads:       c.stats.DiskLoads,
		InternalLookups: c.stats.InternalLookups,
		LastAccess:      c.stats.LastAccess,
		TotalAccesses:   c.stats.TotalAccesses,
	}
}

func (c *IntelligentCache) RecordCoalescedRead() {
	c.stats.mu.Lock()
	c.stats.CoalescedReads++
	c.stats.mu.Unlock()
}

func (c *IntelligentCache) RecordDiskLoad() {
	c.stats.mu.Lock()
	c.stats.DiskLoads++
	c.stats.mu.Unlock()
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
	c.residentBytes = 0
	c.fileBytesLen = make(map[string]int64)
	c.globalGen++
	c.pathGen = make(map[string]uint64)
}

func (c *IntelligentCache) GetAccessStats() map[string]int64 {
	c.patternMu.RLock()
	defer c.patternMu.RUnlock()

	stats := make(map[string]int64, len(c.accessPattern))
	for k, v := range c.accessPattern {
		stats[k] = v
	}
	return stats
}
