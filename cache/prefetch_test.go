package cache

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func allowAll(path string) (string, bool) { return path, true }

func TestPrefetch_SkipsUnauthorizedSiblings(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	main := filepath.Join(dir, "main.go")
	secret := filepath.Join(dir, ".env")
	okFile := filepath.Join(dir, "ok.go")
	writeFile(t, main, "package main")
	writeFile(t, secret, "SECRET=1")
	writeFile(t, okFile, "package ok")

	c.SetPrefetchAuthorizer(func(path string) (string, bool) {
		if filepath.Base(path) == ".env" {
			return "", false
		}
		return path, true
	})

	for i := 0; i < prefetchAccessThreshold; i++ {
		c.TrackAccess(main)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, hit := c.PeekFileFresh(okFile); hit {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, hit := c.GetFileFresh(secret); hit {
		t.Fatal("secret sibling must not be prefetched")
	}
}

func TestPrefetch_RevalidatesAtRun(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	main := filepath.Join(dir, "a.go")
	sib := filepath.Join(dir, "b.go")
	writeFile(t, main, "a")
	writeFile(t, sib, "b")

	var n atomic.Int32
	c.SetPrefetchAuthorizer(func(path string) (string, bool) {
		if path == sib {
			if n.Add(1) == 1 {
				return path, true
			}
			return "", false
		}
		return path, true
	})

	for i := 0; i < prefetchAccessThreshold; i++ {
		c.TrackAccess(main)
	}
	time.Sleep(80 * time.Millisecond)
	if _, hit := c.GetFileFresh(sib); hit {
		t.Fatal("revoked path must not be filled")
	}
}

func TestPrefetch_QueueFullDoesNotPanic(t *testing.T) {
	c := newTestCache(t)
	block := make(chan struct{})
	c.SetPrefetchAuthorizer(func(path string) (string, bool) {
		<-block
		return path, true
	})
	dir := t.TempDir()
	for i := 0; i < prefetchQueueSize+50; i++ {
		c.enqueuePrefetch(filepath.Join(dir, "f"))
	}
	close(block)
}

func TestPrefetch_CloseIdempotentAndConcurrent(t *testing.T) {
	c := newTestCache(t)
	c.SetPrefetchAuthorizer(allowAll)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	writeFile(t, path, "x")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.TrackAccess(path)
			c.enqueuePrefetch(path)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = c.Close()
		_ = c.Close()
	}()
	wg.Wait()
	c.TrackAccess(path)
	c.enqueuePrefetch(path)
}

func TestPrefetch_NilAuthorizerDoesNotFill(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	main := filepath.Join(dir, "a.go")
	sib := filepath.Join(dir, "b.go")
	writeFile(t, main, "a")
	writeFile(t, sib, "b")
	for i := 0; i < prefetchAccessThreshold; i++ {
		c.TrackAccess(main)
	}
	time.Sleep(50 * time.Millisecond)
	if _, hit := c.GetFileFresh(sib); hit {
		t.Fatal("without authorizer prefetch must not fill")
	}
}
