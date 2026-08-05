package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestStreamingEditLargeFile_Regression covers perf v4.5.30: the large-file
// edit path was rewritten to two constant-memory passes. These tests pin
// the behavioral contract (occurrence count, replacement correctness,
// boundary safety, CRLF handling, no-match) without forcing us to actually
// allocate a 50MB file in the test runner.
func TestStreamingEditLargeFile_Regression(t *testing.T) {
	dir := t.TempDir()
	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{dir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	ctx := context.Background()

	// Helper: write content into the engine's allowed root and run
	// streaming-edit (maxFileSize=0 forces the streaming path even for tiny
	// files). Per-file isolation via unique names.
	run := func(t *testing.T, name, content, oldText, newText string) (string, int, error) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		res, err := engine.SmartEditFile(ctx, p, oldText, newText, false, 0)
		if err != nil {
			return "", 0, err
		}
		out, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("read back: %v", rerr)
		}
		return string(out), res.ReplacementCount, nil
	}

	t.Run("single occurrence LF", func(t *testing.T) {
		out, count, err := run(t, "single.txt", "hello world\n", "world", "earth")
		if err != nil || count != 1 || out != "hello earth\n" {
			t.Fatalf("got out=%q count=%d err=%v; want %q count=1", out, count, err, "hello earth\n")
		}
	})

	t.Run("zero matches returns no-match without writing", func(t *testing.T) {
		out, count, err := run(t, "zero.txt", "hello world\n", "absent", "X")
		if err != nil || count != 0 || out != "hello world\n" {
			t.Fatalf("got out=%q count=%d err=%v; want unchanged count=0", out, count, err)
		}
	})

	t.Run("empty old_text is rejected", func(t *testing.T) {
		_, _, err := run(t, "empty.txt", "anything", "", "X")
		if err == nil {
			t.Fatal("expected validation error for empty old_text, got nil")
		}
	})

	t.Run("CRLF normalized to LF", func(t *testing.T) {
		// CRLF input, LF search text — must still find matches and write LF.
		out, count, err := run(t, "crlf.txt", "a\r\nb\r\nc\r\n", "b", "B")
		if err != nil || count != 1 || out != "a\nB\nc\n" {
			t.Fatalf("got out=%q count=%d err=%v; want %q count=1", out, count, err, "a\nB\nc\n")
		}
	})

	t.Run("match spanning 64KB chunk boundary", func(t *testing.T) {
		// Build a file larger than the 64KB streaming window so the needle
		// is guaranteed to fall across a read boundary. Needle length = 32,
		// so the match spans a boundary regardless of buffer alignment.
		const window = 64 * 1024
		padding := strings.Repeat("x", window-8)
		needle := "ABCDEFGHIJ0123456789KLMNOPQRSTUV"
		content := padding + needle + padding + "\n"
		out, count, err := run(t, "boundary.txt", content, needle, "REPLACED")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if count != 1 {
			t.Errorf("expected exactly 1 occurrence (boundary match), got %d", count)
		}
		if !strings.Contains(out, "REPLACED") || strings.Contains(out, needle) {
			t.Errorf("replacement did not apply at the chunk boundary; got prefix: %q", out[:min(120, len(out))])
		}
	})

	t.Run("multi-line needle LF-normalized", func(t *testing.T) {
		// CRLF file with CRLF multi-line search text — normalize on both
		// sides must produce a single match.
		out, count, err := run(t, "multi.txt", "line1\r\nline2\r\nline3\r\n", "line1\r\nline2", "FUSED")
		if err != nil || count != 1 || out != "FUSED\nline3\n" {
			t.Fatalf("got out=%q count=%d err=%v; want %q count=1", out, count, err, "FUSED\nline3\n")
		}
	})
}
