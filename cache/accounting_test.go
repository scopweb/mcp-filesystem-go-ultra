package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccounting_FreshHitAndStaleMiss(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)

	if _, hit := c.GetFileFresh(path); !hit {
		t.Fatal("expected demand hit")
	}
	st := c.GetStats()
	if st.FileHits != 1 || st.FileMisses != 0 || st.StaleMisses != 0 {
		t.Fatalf("after hit: %+v", st)
	}

	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("expected stale miss")
	}
	st = c.GetStats()
	if st.FileHits != 1 || st.FileMisses != 1 || st.StaleMisses != 1 {
		t.Fatalf("after stale: %+v", st)
	}
}

func TestAccounting_PeekAndPrefetchExcludedFromDemand(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)

	if _, hit := c.PeekFileFresh(path); !hit {
		t.Fatal("peek")
	}
	if _, hit := c.lookupFresh(path, lookupPrefetch); !hit {
		t.Fatal("prefetch lookup")
	}
	st := c.GetStats()
	if st.FileHits != 0 || st.FileMisses != 0 {
		t.Fatalf("demand polluted: %+v", st)
	}
	if st.InternalLookups != 1 || st.PrefetchHits != 1 {
		t.Fatalf("class counts: %+v", st)
	}
}

func TestAccounting_ReplaceInvalidateFlush(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "aa")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)
	if got := c.GetMemoryUsage(); got != 2 {
		t.Fatalf("resident after set: %d", got)
	}

	writeFile(t, path, "bbbbbb")
	content, meta, stable, err = ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)
	if got := c.GetMemoryUsage(); got != 6 {
		t.Fatalf("resident after replace: %d", got)
	}
	mem := c.Memory()
	if mem.ResidentBytes != 6 {
		t.Fatalf("Memory.ResidentBytes=%d", mem.ResidentBytes)
	}
	if mem.CapacityBytes <= mem.ResidentBytes && mem.CapacityBytes != 0 {
		t.Fatalf("capacity should be reserved estimate, got %d", mem.CapacityBytes)
	}
	if c.GetMemoryUsage() == mem.ResidentBytes+mem.CapacityBytes {
		t.Fatal("GetMemoryUsage must not sum resident+capacity")
	}

	c.InvalidateFile(path)
	if got := c.GetMemoryUsage(); got != 0 {
		t.Fatalf("resident after invalidate: %d", got)
	}

	c.SetFile(path, content, meta)
	c.Flush()
	if got := c.GetMemoryUsage(); got != 0 {
		t.Fatalf("resident after flush: %d", got)
	}
}

func TestAccounting_EvictionDropsResident(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)
	if err := c.fileCache.Delete(path); err != nil {
		t.Fatal(err)
	}
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("evicted content must miss")
	}
	if got := c.GetMemoryUsage(); got != 0 {
		t.Fatalf("resident after eviction: %d", got)
	}
}
