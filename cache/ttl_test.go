package cache

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseFileTTL(t *testing.T) {
	d, err := ParseFileTTL("3m")
	if err != nil || d != 3*time.Minute {
		t.Fatalf("3m: %v %v", d, err)
	}
	d, err = ParseFileTTL("10m")
	if err != nil || d != 10*time.Minute {
		t.Fatalf("10m: %v %v", d, err)
	}
	if _, err := ParseFileTTL("0"); err == nil {
		t.Fatal("zero must fail")
	}
	if _, err := ParseFileTTL("500ms"); err == nil {
		t.Fatal("sub-second must fail")
	}
	if _, err := ParseFileTTL("-1s"); err == nil {
		t.Fatal("negative must fail")
	}
	if _, err := ParseFileTTL("nope"); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestNewIntelligentCacheTTL_DefaultAndExplicit(t *testing.T) {
	c := newTestCache(t)
	if c.FileTTL() != DefaultFileTTL {
		t.Fatalf("default TTL=%s", c.FileTTL())
	}
	c2, err := NewIntelligentCacheTTL(4*1024*1024, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c2.Close() })
	if c2.FileTTL() != 10*time.Minute {
		t.Fatalf("explicit TTL=%s", c2.FileTTL())
	}
	if _, err := NewIntelligentCacheTTL(4*1024*1024, 0); err == nil {
		t.Fatal("zero TTL must fail")
	}
}

func TestFileTTL_ExpirationMiss(t *testing.T) {
	c, err := NewIntelligentCacheTTL(4*1024*1024, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)
	if _, hit := c.GetFileFresh(path); !hit {
		t.Fatal("expected hit before TTL")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("expected miss after TTL")
	}
}
