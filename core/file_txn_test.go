package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanonicalOCCKey_WindowsCase(t *testing.T) {
	a := CanonicalOCCKey(`C:\Foo\bar.txt`)
	b := CanonicalOCCKey(`c:\foo\bar.txt`)
	if a == "" || b == "" {
		t.Fatal("empty key")
	}
}

func TestPathLock_IndependentParallel(t *testing.T) {
	m := newPathLockManager()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("a"), 0644)
	_ = os.WriteFile(b, []byte("b"), 0644)

	var started sync.WaitGroup
	started.Add(2)
	var overlapping atomic.Int32
	run := func(p string) {
		u, err := m.Acquire(context.Background(), CanonicalPath(p))
		if err != nil {
			t.Errorf("acquire: %v", err)
			started.Done()
			return
		}
		defer u()
		overlapping.Add(1)
		started.Done()
		started.Wait()
		if overlapping.Load() != 2 {
			t.Errorf("independent paths should overlap, got %d", overlapping.Load())
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); run(a) }()
	go func() { defer wg.Done(); run(b) }()
	wg.Wait()
}

func TestPathLock_Cancel(t *testing.T) {
	m := newPathLockManager()
	p := CanonicalPath(filepath.Join(t.TempDir(), "x.txt"))
	u, err := m.Acquire(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer u()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = m.Acquire(ctx, p)
	if err == nil {
		t.Fatal("expected cancel while lock held")
	}
}

func TestConcurrentEdit_OCCConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	original := "hello world\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)
	hash := contentHashFNV(original)

	start := make(chan struct{})
	var okN, failN atomic.Int32
	var wg sync.WaitGroup
	run := func(old, neu string) {
		defer wg.Done()
		<-start
		ctx := WithExpectedHash(context.Background(), hash)
		_, err := engine.EditFile(ctx, path, old, neu, false, false, false)
		if err != nil {
			failN.Add(1)
			return
		}
		okN.Add(1)
	}
	wg.Add(2)
	go run("hello", "HELLO")
	go run("world", "WORLD")
	close(start)
	wg.Wait()

	if okN.Load() < 1 {
		t.Fatal("expected at least one successful writer")
	}
	if okN.Load()+failN.Load() != 2 {
		t.Fatalf("ok=%d fail=%d", okN.Load(), failN.Load())
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if s != "HELLO world\n" && s != "hello WORLD\n" && s != "HELLO WORLD\n" {
		t.Fatalf("lost or mixed update: %q", s)
	}
	if okN.Load() == 2 && s != "HELLO WORLD\n" {
		t.Fatalf("two successes but file is not composition of both: %q", s)
	}
}

func TestReadSnapshot_HashMatchesBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.txt")
	body := "abc\ndef\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)
	snap, err := engine.ReadSnapshot(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if string(snap.Bytes) != body {
		t.Fatalf("bytes=%q", snap.Bytes)
	}
	if snap.Hash != contentHashFNV(body) {
		t.Fatalf("hash %s want %s", snap.Hash, contentHashFNV(body))
	}
}

func TestBeginFileTxn_MissingVsPermission(t *testing.T) {
	dir := t.TempDir()
	engine := newTestEngine(dir)
	missing := filepath.Join(dir, "nope.txt")
	_, txn, err := engine.BeginFileTxn(context.Background(), missing, false)
	if err == nil {
		txn.Release()
		t.Fatal("missing file without allowMissing must error")
	}
	if !isNotExistPathError(err) {
		t.Fatalf("want not-exist, got %v", err)
	}
	_, txn, err = engine.BeginFileTxn(context.Background(), missing, true)
	if err != nil {
		t.Fatal(err)
	}
	defer txn.Release()
	if txn.Snapshot().Exists {
		t.Fatal("allowMissing snapshot should not exist")
	}
}

func assertLockFree(t *testing.T, engine *UltraFastEngine, path string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, txn, err := engine.BeginFileTxn(ctx, path, true)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	txn.Release()
}

func TestFileTxn_ErrorPathsReleaseLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	body := "hello\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)
	defer engine.Close()

	t.Run("occ", func(t *testing.T) {
		ctx := WithExpectedHash(context.Background(), "deadbeef")
		_, txn, err := engine.BeginFileTxn(ctx, path, false)
		if err == nil {
			txn.Release()
			t.Fatal("expected OCC mismatch")
		}
		if _, ok := err.(*OCCMismatchError); !ok {
			t.Fatalf("want OCCMismatchError, got %T %v", err, err)
		}
		assertLockFree(t, engine, path)
	})

	t.Run("missing", func(t *testing.T) {
		missing := filepath.Join(dir, "gone.txt")
		_, txn, err := engine.BeginFileTxn(context.Background(), missing, false)
		if err == nil {
			txn.Release()
			t.Fatal("expected missing")
		}
		assertLockFree(t, engine, missing)
	})

	t.Run("edit_no_match", func(t *testing.T) {
		_, err := engine.EditFile(context.Background(), path, "NOPE", "x", false, false, false)
		if err == nil {
			t.Fatal("expected edit miss")
		}
		assertLockFree(t, engine, path)
	})

	t.Run("commit_write_fail", func(t *testing.T) {
		ctx, txn, err := engine.BeginFileTxn(context.Background(), path, false)
		if err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(path)
		if err := os.Mkdir(path, 0755); err != nil {
			txn.Release()
			t.Fatal(err)
		}
		_, err = txn.Commit(ctx, []byte("new\n"), false, "test", "")
		txn.Release()
		if err == nil {
			t.Fatal("expected commit write fail")
		}
		assertLockFree(t, engine, filepath.Join(dir, "other.txt"))
	})
}

func TestNestedTxn_NoDeadlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "n.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)
	ctx, txn, err := engine.BeginFileTxn(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer txn.Release()
	ctx2, txn2, err := engine.BeginFileTxn(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = ctx2
	txn2.Release()
}
