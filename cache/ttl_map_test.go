package cache

import (
	"testing"
	"time"
)

func TestDirectoryCacheExpiresAndRefresh(t *testing.T) {
	c, err := NewIntelligentCache(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	c.dirCache.stopSweep()
	c.dirCache = newTTLMap(30*time.Millisecond, 0, c.onDirEvicted)

	c.SetDirectory("dir", "listing", time.Now())
	if listing, _, ok := c.GetDirectory("dir"); !ok || listing != "listing" {
		t.Fatalf("want hit, got %q ok=%v", listing, ok)
	}
	time.Sleep(20 * time.Millisecond)
	if _, _, ok := c.GetDirectory("dir"); !ok {
		t.Fatal("sliding refresh should keep the directory entry")
	}
	time.Sleep(40 * time.Millisecond)
	if _, _, ok := c.GetDirectory("dir"); ok {
		t.Fatal("directory entry should expire")
	}
}
