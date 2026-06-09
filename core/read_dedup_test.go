package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
)

func setupDedupTestEngine(t *testing.T) (*UltraFastEngine, string) {
	t.Helper()
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(32 * 1024 * 1024)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}
	engine, err := NewUltraFastEngine(&Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  8,
	})
	if err != nil {
		t.Fatalf("engine init: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine, tempDir
}

func TestReadFileBytesDeduped_ConcurrentSingleDiskRead(t *testing.T) {
	engine, dir := setupDedupTestEngine(t)
	path := filepath.Join(dir, "concurrent.txt")
	content := strings.Repeat("line\n", 100)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	diskReadCount.Store(0)
	ctx := context.Background()

	const workers = 12
	var wg sync.WaitGroup
	wg.Add(workers)
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			data, err := engine.readFileBytesDeduped(ctx, path)
			if err != nil {
				errCh <- err
				return
			}
			if string(data) != content {
				errCh <- fmt.Errorf("content mismatch")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	if got := diskReadCount.Load(); got != 1 {
		t.Fatalf("expected 1 disk read, got %d", got)
	}
}

func TestReadFileRange_UsesCacheAfterWarm(t *testing.T) {
	engine, dir := setupDedupTestEngine(t)
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", 10))
		sb.WriteString("\n")
	}
	path := filepath.Join(dir, "range_cache.txt")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if _, err := engine.ReadFileContent(ctx, path); err != nil {
		t.Fatal(err)
	}

	diskReadCount.Store(0)
	result, err := engine.ReadFileRange(ctx, path, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "line") {
		t.Fatalf("unexpected range result: %q", result)
	}
	if diskReadCount.Load() != 0 {
		t.Fatalf("ReadFileRange should not hit disk when cache is warm, got %d reads", diskReadCount.Load())
	}
}

func TestInvalidateCache_ForgetsDedupAndReloads(t *testing.T) {
	engine, dir := setupDedupTestEngine(t)
	path := filepath.Join(dir, "invalidate.txt")
	if err := os.WriteFile(path, []byte("version-1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	first, err := engine.ReadFileContent(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if first != "version-1\n" {
		t.Fatalf("got %q", first)
	}

	if err := os.WriteFile(path, []byte("version-2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	engine.InvalidateCache(path)

	second, err := engine.ReadFileContent(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if second != "version-2\n" {
		t.Fatalf("after invalidate expected version-2, got %q", second)
	}
}

func TestExtractLineRangeFromBytes_MatchesFooter(t *testing.T) {
	content := []byte("a\nb\nc\nd\ne")
	got, err := extractLineRangeFromBytes(content, "/tmp/sample.go", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "b\nc\nd") {
		t.Fatalf("missing range body: %q", got)
	}
	if !strings.Contains(got, "[Lines 2-4 of 5 total lines in sample.go") {
		t.Fatalf("missing footer: %q", got)
	}
}

func TestBytesLineScanner_MatchesBufioTrailingNewline(t *testing.T) {
	t.Parallel()

	const totalLines = 685
	var b strings.Builder
	for i := 1; i <= totalLines; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	content := []byte(b.String())

	scanner := newBytesLineScanner(content)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lineNum != totalLines {
		t.Fatalf("bytes scanner counted %d lines, want %d (bufio-compatible)", lineNum, totalLines)
	}

	got, err := extractLineRangeFromBytes(content, "FooterTest.go", 15, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, fmt.Sprintf("of %d total lines", totalLines)) {
		t.Fatalf("footer must report %d lines: %s", totalLines, got[len(got)-120:])
	}
	if strings.Contains(got, "of 686 total lines") {
		t.Fatal("trailing newline must not add an extra line to the footer total")
	}
}

func BenchmarkReadFile_ConcurrentCold(b *testing.B) {
	engine, dir := setupBenchEngine(b, 32*1024*1024)
	path := makeBenchFile(b, dir, "cold-concurrent.txt", 50*1024)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := engine.ReadFileContent(ctx, path); err != nil {
				b.Fatal(err)
			}
		}
	})
}