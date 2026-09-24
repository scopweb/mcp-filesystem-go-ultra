package cache

import (
	"os"
	"path/filepath"
)

const (
	prefetchQueueSize       = 100
	prefetchMaxSiblings     = 3
	prefetchMaxBytes        = 100 * 1024
	prefetchAccessThreshold = 3
	maxAccessPatterns       = 4096
)

type PrefetchAuthorizer func(path string) (resolved string, ok bool)

func (c *IntelligentCache) SetPrefetchAuthorizer(fn PrefetchAuthorizer) {
	c.authMu.Lock()
	c.authorizer = fn
	c.authMu.Unlock()
}

func (c *IntelligentCache) authorizePrefetch(path string) (string, bool) {
	c.authMu.Lock()
	fn := c.authorizer
	c.authMu.Unlock()
	if fn == nil {
		return "", false
	}
	return fn(path)
}

func (c *IntelligentCache) isClosed() bool {
	c.prefetchMu.Lock()
	defer c.prefetchMu.Unlock()
	return c.closed
}

func (c *IntelligentCache) enqueuePrefetch(path string) {
	c.prefetchMu.Lock()
	defer c.prefetchMu.Unlock()
	if c.closed {
		return
	}
	if _, dup := c.prefetchPending[path]; dup {
		return
	}
	select {
	case c.prefetchQueue <- path:
		c.prefetchPending[path] = struct{}{}
	default:
	}
}

func (c *IntelligentCache) dequeuePending(path string) {
	c.prefetchMu.Lock()
	delete(c.prefetchPending, path)
	c.prefetchMu.Unlock()
}

func (c *IntelligentCache) prefetchWorker() {
	defer c.prefetchWG.Done()
	for path := range c.prefetchQueue {
		c.dequeuePending(path)
		if c.isClosed() {
			continue
		}
		c.runPrefetch(path)
	}
}

func (c *IntelligentCache) runPrefetch(path string) {
	resolved, ok := c.authorizePrefetch(path)
	if !ok {
		return
	}
	if _, hit := c.lookupFresh(resolved, lookupPrefetch); hit {
		return
	}
	_ = c.prefetchFile(resolved)
}

func (c *IntelligentCache) prefetchFile(path string) error {
	content, meta, stable, err := ReadFileStable(path)
	if err != nil {
		return err
	}
	if !stable {
		return nil
	}
	if c.SetFileIfGeneration(path, content, meta, c.CaptureGeneration(path)) {
		c.stats.mu.Lock()
		c.stats.PrefetchFills++
		c.stats.mu.Unlock()
	}
	return nil
}

func (c *IntelligentCache) TrackAccess(path string) {
	c.patternMu.Lock()
	if _, exists := c.accessPattern[path]; !exists && len(c.accessPattern) >= maxAccessPatterns {
		c.patternMu.Unlock()
		return
	}
	c.accessPattern[path]++
	count := c.accessPattern[path]
	c.patternMu.Unlock()

	if count == prefetchAccessThreshold {
		c.suggestPrefetch(path)
	}
}

func (c *IntelligentCache) suggestPrefetch(path string) {
	if c.isClosed() {
		return
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	prefetched := 0
	for _, entry := range entries {
		if entry.IsDir() || prefetched >= prefetchMaxSiblings {
			continue
		}
		siblingPath := filepath.Join(dir, entry.Name())
		if siblingPath == path {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() > prefetchMaxBytes {
			continue
		}
		if _, ok := c.authorizePrefetch(siblingPath); !ok {
			continue
		}
		c.enqueuePrefetch(siblingPath)
		prefetched++
	}
}

func (c *IntelligentCache) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.prefetchMu.Lock()
		c.closed = true
		close(c.prefetchQueue)
		c.prefetchMu.Unlock()
		c.prefetchWG.Wait()
		if c.dirCache != nil {
			c.dirCache.stopSweep()
		}
		if c.metaCache != nil {
			c.metaCache.stopSweep()
		}
		c.Flush()
		closeErr = c.fileCache.Close()
	})
	return closeErr
}
