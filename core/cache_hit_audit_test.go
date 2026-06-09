package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
)

// TestCacheHit_AuditEntry_Records verifies that improvement M3 (cache_hit in
// audit log) is wired correctly. The first read of a file should record
// CacheHit=false, the second read (warm cache) should record CacheHit=true.
func TestCacheHit_AuditEntry_Records(t *testing.T) {
	tempDir := t.TempDir()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}
	engine, err := NewUltraFastEngine(&Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatalf("engine init: %v", err)
	}
	t.Cleanup(func() { engine.Close() })

	// Create a test file
	path := filepath.Join(tempDir, "warm.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Helper to build a ctx with an AuditEntry (simulating auditWrap)
	makeCtx := func() (context.Context, *AuditEntry) {
		entry := &AuditEntry{Tool: "read_file"}
		return context.WithValue(context.Background(), AuditEntryKey{}, entry), entry
	}

	// ── First read: cache miss ───────────────────────────────────────
	ctx1, entry1 := makeCtx()
	if _, err := engine.ReadFileContent(ctx1, path); err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if entry1.CacheHit == nil {
		t.Fatal("expected CacheHit to be set on first read, got nil")
	}
	if *entry1.CacheHit != false {
		t.Errorf("expected CacheHit=false on first read (miss), got %v", *entry1.CacheHit)
	}

	// ── Second read: cache hit ───────────────────────────────────────
	ctx2, entry2 := makeCtx()
	if _, err := engine.ReadFileContent(ctx2, path); err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if entry2.CacheHit == nil {
		t.Fatal("expected CacheHit to be set on second read, got nil")
	}
	if *entry2.CacheHit != true {
		t.Errorf("expected CacheHit=true on second read (hit), got %v", *entry2.CacheHit)
	}
}

// TestSetCacheHit_NoOpWithoutEntry verifies that SetCacheHit is a no-op when
// no AuditEntry is present in the context (e.g., direct engine calls outside
// of auditWrap). It must NOT panic.
func TestSetCacheHit_NoOpWithoutEntry(t *testing.T) {
	ctx := context.Background()
	// Should not panic
	SetCacheHit(ctx, true)
	SetCacheHit(ctx, false)
}
