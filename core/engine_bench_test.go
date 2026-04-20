package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
)

// setupBenchEngine builds a fresh engine rooted at b.TempDir() with an in-process cache.
// Mirrors setupTestEngine but targets *testing.B so benchmarks don't share state.
func setupBenchEngine(b *testing.B, cacheBytes int64) (*UltraFastEngine, string) {
	b.Helper()
	tempDir := b.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(cacheBytes)
	if err != nil {
		b.Fatalf("cache init: %v", err)
	}
	engine, err := NewUltraFastEngine(&Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  4,
	})
	if err != nil {
		b.Fatalf("engine init: %v", err)
	}
	b.Cleanup(func() { engine.Close() })
	return engine, tempDir
}

// makeBenchFile writes a deterministic file of approximately `size` bytes.
func makeBenchFile(b *testing.B, dir, name string, size int) string {
	b.Helper()
	line := "the quick brown fox jumps over the lazy dog 0123456789\n" // 56 bytes
	var sb strings.Builder
	sb.Grow(size + len(line))
	for sb.Len() < size {
		sb.WriteString(line)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		b.Fatalf("write %s: %v", path, err)
	}
	return path
}

// ─── Read benchmarks ──────────────────────────────────────────────────────────

func BenchmarkReadFile_Small(b *testing.B)  { benchReadFile(b, 4*1024) }          // 4 KB
func BenchmarkReadFile_Medium(b *testing.B) { benchReadFile(b, 200*1024) }        // 200 KB
func BenchmarkReadFile_Large(b *testing.B)  { benchReadFile(b, 2*1024*1024) }     // 2 MB

func benchReadFile(b *testing.B, size int) {
	engine, dir := setupBenchEngine(b, 32*1024*1024)
	path := makeBenchFile(b, dir, "read.txt", size)
	ctx := context.Background()

	b.ResetTimer()
	b.SetBytes(int64(size))
	for i := 0; i < b.N; i++ {
		if _, err := engine.ReadFileContent(ctx, path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadFile_CacheHit measures steady-state read latency once content is warm.
func BenchmarkReadFile_CacheHit(b *testing.B) {
	engine, dir := setupBenchEngine(b, 32*1024*1024)
	path := makeBenchFile(b, dir, "warm.txt", 50*1024)
	ctx := context.Background()

	// Warm the cache.
	if _, err := engine.ReadFileContent(ctx, path); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.SetBytes(50 * 1024)
	for i := 0; i < b.N; i++ {
		if _, err := engine.ReadFileContent(ctx, path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadFileRange verifies the partial-read fast path pays off on large files.
func BenchmarkReadFileRange(b *testing.B) {
	engine, dir := setupBenchEngine(b, 32*1024*1024)
	path := makeBenchFile(b, dir, "range.txt", 1*1024*1024) // 1 MB, ~18k lines
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.ReadFileRange(ctx, path, 100, 200); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Write benchmarks ─────────────────────────────────────────────────────────

func BenchmarkWriteFile_Small(b *testing.B) { benchWriteFile(b, 4*1024) }
func BenchmarkWriteFile_Large(b *testing.B) { benchWriteFile(b, 1*1024*1024) }

func benchWriteFile(b *testing.B, size int) {
	engine, dir := setupBenchEngine(b, 32*1024*1024)
	content := strings.Repeat("x", size)
	ctx := context.Background()

	b.ResetTimer()
	b.SetBytes(int64(size))
	for i := 0; i < b.N; i++ {
		path := filepath.Join(dir, fmt.Sprintf("w-%d.txt", i))
		if err := engine.WriteFileContent(ctx, path, content); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Edit benchmark ───────────────────────────────────────────────────────────

// BenchmarkEditFile measures the full edit path (read → patch → write + backup).
// Resets the file between iterations so each edit targets the same known state.
func BenchmarkEditFile(b *testing.B) {
	engine, dir := setupBenchEngine(b, 32*1024*1024)
	ctx := context.Background()

	base := strings.Repeat("foo bar baz\n", 500) // ~6 KB
	path := filepath.Join(dir, "edit.txt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := os.WriteFile(path, []byte(base), 0644); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		_, err := engine.EditFile(ctx, path, "foo bar baz", "foo BAR baz", true, false)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Parallel read scalability ────────────────────────────────────────────────

// BenchmarkParallelReads exercises cache + semaphore contention.
// Run with:  go test ./core/ -bench=BenchmarkParallelReads -cpu=1,2,4,8,16
func BenchmarkParallelReads(b *testing.B) {
	engine, dir := setupBenchEngine(b, 64*1024*1024)
	ctx := context.Background()

	// Pre-create a small pool of files so parallel callers don't all hit the same key.
	paths := make([]string, 8)
	for i := range paths {
		paths[i] = makeBenchFile(b, dir, fmt.Sprintf("p-%d.txt", i), 20*1024)
		// Warm the cache once.
		if _, err := engine.ReadFileContent(ctx, paths[i]); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.SetBytes(20 * 1024)
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := engine.ReadFileContent(ctx, paths[i%len(paths)]); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
