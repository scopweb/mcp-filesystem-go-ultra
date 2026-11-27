package core

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read_perf_test.go — the cost structure of the read path.
//
// The end-to-end numbers (read_file ~2 ms for a small file, ~22 ms for a
// 5 MB file) hide where the time actually goes. These benchmarks isolate the
// pieces so a change can be measured against the piece it claims to speed
// up.
//
// Run: go test ./core/ -run XXX -bench 'ReadPath' -benchmem

func newBenchFile(b *testing.B, size int) (string, *UltraFastEngine) {
	b.Helper()
	dir := b.TempDir()
	p := filepath.Join(dir, "data.bin")
	line := []byte("BENCH_READ_LINE_0123456789 padding\n")
	content := bytes.Repeat(line, size/len(line)+1)[:size]
	if err := os.WriteFile(p, content, 0o600); err != nil {
		b.Fatal(err)
	}
	e := newTestEngine(dir)
	b.Cleanup(func() { _ = e.Close() })
	return p, e
}

// BenchmarkReadPath_CacheGet isolates the content-cache lookup, which is
// what a warm read pays before the hash (now memoized).
func BenchmarkReadPath_CacheGet(b *testing.B) {
	for _, size := range []int{2 * 1024, 5 * 1024 * 1024} {
		p, e := newBenchFile(b, size)
		if _, err := e.ReadFileSnapshot(context.Background(), p); err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data, _, hit := e.cache.GetFileFreshWithMeta(p)
				if !hit || len(data) == 0 {
					b.Fatal("expected a cache hit")
				}
			}
		})
	}
}

// BenchmarkReadPath_Snapshot measures the whole engine read: authorization,
// path resolution, cache lookup, disk read, hash and snapshot assembly.
//
// Warm = the file is in the content cache (an agent re-reading a file it
// already read). Cold = the cache entry is dropped before every iteration.
func BenchmarkReadPath_Snapshot(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{"2KB", 2 * 1024},
		{"5MB", 5 * 1024 * 1024},
	} {
		for _, mode := range []string{"warm", "cold"} {
			b.Run(tc.name+"/"+mode, func(b *testing.B) {
				p, e := newBenchFile(b, tc.size)
				ctx := context.Background()
				if _, err := e.ReadFileSnapshot(ctx, p); err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if mode == "cold" {
						e.cache.InvalidateFile(p)
					}
					snap, err := e.ReadFileSnapshot(ctx, p)
					if err != nil {
						b.Fatal(err)
					}
					if len(snap.Bytes) == 0 || snap.Hash == "" {
						b.Fatal("empty snapshot")
					}
				}
			})
		}
	}
}

// BenchmarkReadPath_Validation isolates the path checks, which run on every
// read no matter how the file is cached.
func BenchmarkReadPath_Validation(b *testing.B) {
	p, e := newBenchFile(b, 2*1024)
	b.Run("IsPathAllowed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if !e.IsPathAllowed(p) {
				b.Fatal("not allowed")
			}
		}
	})
	b.Run("ResolveAndAuthorize", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := e.ResolveAndAuthorize("read", p); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("CanonicalPath", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if CanonicalPath(p) == "" {
				b.Fatal("empty canonical path")
			}
		}
	})
	b.Run("Authorize", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := e.Authorize(OpRead, p); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Stat", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := os.Stat(p); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ResolveSymlinks", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, _, err := ResolveSymlinks(p); err != nil {
				b.Fatal(err)
			}
		}
	})
	// The exact validation sequence the read path runs, in order.
	b.Run("ValidationSequence", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if !e.IsPathAllowed(p) {
				b.Fatal("not allowed")
			}
			resolved, err := e.ResolveAndAuthorize("read", p)
			if err != nil {
				b.Fatal(err)
			}
			if CanonicalPath(resolved) == "" {
				b.Fatal("empty canonical path")
			}
		}
	})
}

// BenchmarkReadPath_ValidationSequence compares the validation the read path
// runs today (resolve the path four times) with a variant that resolves it
// once and reuses the result.
//
//	current : IsPathAllowed (resolve) + ResolveAndAuthorize (resolve + allow
//	          check that resolves again) + CanonicalPath (resolve again)
//	proposed: ResolveSymlinks once + containment check on the resolved path
//	          (string comparison, no filesystem access)
func BenchmarkReadPath_ValidationSequence(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{"2KB", 2 * 1024},
		{"5MB", 5 * 1024 * 1024},
	} {
		b.Run(tc.name+"/current", func(b *testing.B) {
			p, e := newBenchFile(b, tc.size)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !e.IsPathAllowed(p) {
					b.Fatal("not allowed")
				}
				resolved, err := e.ResolveAndAuthorize("read", p)
				if err != nil {
					b.Fatal(err)
				}
				if CanonicalPath(resolved) == "" {
					b.Fatal("empty canonical path")
				}
			}
		})
		b.Run(tc.name+"/proposed", func(b *testing.B) {
			p, e := newBenchFile(b, tc.size)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resolved, _, err := ResolveSymlinks(p)
				if err != nil {
					b.Fatal(err)
				}
				if !containmentOnly(e, resolved) {
					b.Fatal("not allowed")
				}
			}
		})
	}
}

// containmentOnly is the containment half of IsPathAllowed applied to a path
// that has ALREADY been resolved: same bases, same boundary-aware prefix
// rule, but no second filesystem walk.
func containmentOnly(e *UltraFastEngine, resolved string) bool {
	empty, bases := e.allowedBases()
	if empty || len(bases) == 0 {
		return true
	}
	target := filepath.Clean(resolved)
	if os.PathSeparator == '\\' {
		target = strings.ToLower(target)
	}
	for _, base := range bases {
		if target == base {
			return true
		}
		rel, err := filepath.Rel(base, target)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// BenchmarkReadPath_Hash measures the OCC token computation. The current
// implementation converts the whole file to a string first, which copies
// every byte before hashing it.
func BenchmarkReadPath_Hash(b *testing.B) {
	for _, size := range []int{2 * 1024, 5 * 1024 * 1024} {
		raw := bytes.Repeat([]byte("x"), size)
		b.Run(fmt.Sprintf("%d/string", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if contentHashFNV(string(raw)) == "" {
					b.Fatal("empty hash")
				}
			}
		})
		b.Run(fmt.Sprintf("%d/bytes", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				h := fnv.New32a()
				h.Write(raw)
				if fmt.Sprintf("%08x", h.Sum32()) == "" {
					b.Fatal("empty hash")
				}
			}
		})
	}
}

// BenchmarkReadPath_StringCopy isolates the cost of materialising the file
// bytes as a string, which the read handler does before projecting lines.
func BenchmarkReadPath_StringCopy(b *testing.B) {
	for _, size := range []int{2 * 1024, 5 * 1024 * 1024} {
		raw := bytes.Repeat([]byte("x"), size)
		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				s := string(raw)
				if len(s) != size {
					b.Fatal("bad copy")
				}
			}
		})
	}
}
