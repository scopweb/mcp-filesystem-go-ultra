package core

import (
	"fmt"
	"os"
	"sync"
)

// MmapCache manages memory-mapped files for ultra-fast reading
type MmapCache struct {
	cache    map[string]*mmapEntry
	maxFiles int
	mu       sync.RWMutex
}

// mmapEntry represents a memory-mapped file
type mmapEntry struct {
	data     []byte
	file     *os.File
	size     int64
	refCount int
}

// NewMmapCache creates a new memory-mapped file cache
func NewMmapCache(maxFiles int) (*MmapCache, error) {
	return &MmapCache{
		cache:    make(map[string]*mmapEntry),
		maxFiles: maxFiles,
	}, nil
}

// ReadFile reads a file using memory mapping for maximum performance
func (mc *MmapCache) ReadFile(path string) ([]byte, error) {
	mc.mu.RLock()
	if entry, exists := mc.cache[path]; exists {
		entry.refCount++
		mc.mu.RUnlock()
		return entry.data, nil
	}
	mc.mu.RUnlock()

	// Not in cache, need to mmap the file
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists := mc.cache[path]; exists {
		entry.refCount++
		return entry.data, nil
	}

	// Open file
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat file: %v", err)
	}

	size := stat.Size()
	if size == 0 {
		file.Close()
		return []byte{}, nil
	}

	// Memory map the file
	data, err := mmapFile(file, size)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to mmap file: %v", err)
	}

	// Check if we need to evict old entries
	if len(mc.cache) >= mc.maxFiles {
		mc.evictLRU()
	}

	// Cache the entry
	entry := &mmapEntry{
		data:     data,
		file:     file,
		size:     size,
		refCount: 1,
	}
	mc.cache[path] = entry

	return data, nil
}

// evictLRU evicts the least recently used entry
func (mc *MmapCache) evictLRU() {
	// Simple eviction: remove first entry with refCount 0
	for path, entry := range mc.cache {
		if entry.refCount == 0 {
			mc.removeEntry(path, entry)
			return
		}
	}

	// If no entry with refCount 0, force evict the first one
	for path, entry := range mc.cache {
		mc.removeEntry(path, entry)
		return
	}
}

// removeEntry removes an entry from cache and cleans up resources
func (mc *MmapCache) removeEntry(path string, entry *mmapEntry) {
	delete(mc.cache, path)
	munmapFile(entry.data)
	entry.file.Close()
}

// InvalidateFile removes a file from the mmap cache
func (mc *MmapCache) InvalidateFile(path string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if entry, exists := mc.cache[path]; exists {
		mc.removeEntry(path, entry)
	}
}

// Close closes the mmap cache and cleans up all resources
func (mc *MmapCache) Close() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for path, entry := range mc.cache {
		mc.removeEntry(path, entry)
	}

	return nil
}

// Platform-specific memory mapping functions
// Windows fallback: use regular file reading instead of mmap

// mmapFile reads a file into memory (Windows fallback)
func mmapFile(file *os.File, size int64) ([]byte, error) {
	// For Windows, we'll use regular file reading instead of mmap
	data := make([]byte, size)
	_, err := file.ReadAt(data, 0)
	if err != nil {
		return nil, fmt.Errorf("file read failed: %v", err)
	}

	return data, nil
}

// munmapFile is a no-op for Windows fallback
func munmapFile(data []byte) error {
	// No-op for Windows fallback since we're not using actual mmap
	return nil
}

// GetStats returns cache statistics
func (mc *MmapCache) GetStats() map[string]interface{} {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	totalSize := int64(0)
	activeFiles := 0

	for _, entry := range mc.cache {
		totalSize += entry.size
		if entry.refCount > 0 {
			activeFiles++
		}
	}

	return map[string]interface{}{
		"total_files":  len(mc.cache),
		"active_files": activeFiles,
		"total_size":   totalSize,
		"max_files":    mc.maxFiles,
	}
}
