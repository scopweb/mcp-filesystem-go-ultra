package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *IntelligentCache {
	t.Helper()
	c, err := NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestGetFileFresh_RequiresMeta(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")

	if err := c.fileCache.Set(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("content without metadata must miss")
	}
	if _, hit := c.fileBytes(path); hit {
		t.Fatal("stale content without metadata must be dropped")
	}
}

func TestGetFileFresh_HitWhenUnchanged(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")

	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatalf("capture err=%v stable=%v", err, stable)
	}
	if !c.SetFileIfGeneration(path, content, meta, c.CaptureGeneration(path)) {
		t.Fatal("set")
	}
	got, hit := c.GetFileFresh(path)
	if !hit || string(got) != "hello" {
		t.Fatalf("hit=%v got=%q", hit, got)
	}
}

func TestGetFileFresh_MissOnMtimeAdvance(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)

	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("advanced mtime must miss")
	}
}

func TestGetFileFresh_MissOnMtimeRewind(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)

	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("rewound mtime must miss")
	}
}

func TestGetFileFresh_MissOnSameSizeDifferentMtime(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "aaaa")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)

	writeFile(t, path, "bbbb")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("same-size rewrite must miss")
	}
}

func TestGetFileFresh_MissOnDeleted(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "hello")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	c.SetFile(path, content, meta)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("deleted original must miss")
	}
}

func TestSetFileIfGeneration_SkipsAfterInvalidate(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "old")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	gen := c.CaptureGeneration(path)
	c.InvalidateFile(path)
	if c.SetFileIfGeneration(path, content, meta, gen) {
		t.Fatal("stale generation must not repopulate")
	}
	if _, hit := c.fileBytes(path); hit {
		t.Fatal("content must stay absent")
	}
}

func TestSetFileIfGeneration_SkipsAfterFlush(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "old")
	content, meta, stable, err := ReadFileStable(path)
	if err != nil || !stable {
		t.Fatal(err)
	}
	gen := c.CaptureGeneration(path)
	c.Flush()
	if c.SetFileIfGeneration(path, content, meta, gen) {
		t.Fatal("flush must invalidate in-flight capture")
	}
}

func TestReadFileStable_RetryThenCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "first")

	n := 0
	SetCaptureHooksForTest(nil, func(p string) {
		if p != path {
			return
		}
		n++
		if n == 1 {
			writeFile(t, path, "second")
		}
	})
	t.Cleanup(func() { SetCaptureHooksForTest(nil, nil) })

	content, meta, stable, err := ReadFileStable(path)
	if err != nil {
		t.Fatal(err)
	}
	if !stable {
		t.Fatal("expected stable capture after retry")
	}
	if string(content) != "second" {
		t.Fatalf("got %q", content)
	}
	if !meta.Valid || meta.Size != int64(len(content)) {
		t.Fatalf("meta=%+v", meta)
	}
	if n < 2 {
		t.Fatalf("expected retry, mutate calls=%d", n)
	}
}

func TestReadFileStable_UnstableNotCachedByCaller(t *testing.T) {
	c := newTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "xxxx")

	n := 0
	SetCaptureHooksForTest(nil, func(p string) {
		if p != path {
			return
		}
		n++
		writeFile(t, path, string(make([]byte, n+8)))
	})
	t.Cleanup(func() { SetCaptureHooksForTest(nil, nil) })

	content, meta, stable, err := ReadFileStable(path)
	if err != nil {
		t.Fatal(err)
	}
	if stable {
		t.Fatal("continuous mutation must not report stable")
	}
	_ = content
	_ = meta
	if _, hit := c.GetFileFresh(path); hit {
		t.Fatal("unstable capture must not be stored")
	}
}
