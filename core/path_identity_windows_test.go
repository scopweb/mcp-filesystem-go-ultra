//go:build windows

package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/sys/windows"
)

func flipBaseCase(path string) string {
	base := filepath.Base(path)
	var b strings.Builder
	changed := false
	for _, r := range base {
		switch {
		case unicode.IsUpper(r):
			b.WriteRune(unicode.ToLower(r))
			changed = true
		case unicode.IsLower(r):
			b.WriteRune(unicode.ToUpper(r))
			changed = true
		default:
			b.WriteRune(r)
		}
	}
	flipped := b.String()
	if !changed || flipped == base {
		return ""
	}
	return filepath.Join(filepath.Dir(path), flipped)
}

func distinctShortDir(longDir string) (string, bool, string) {
	p, err := windows.UTF16PtrFromString(longDir)
	if err != nil {
		return "", false, err.Error()
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		return "", false, "GetShortPathNameW failed: " + errString(err)
	}
	if int(n) > len(buf) {
		return "", false, "GetShortPathNameW result does not fit the buffer"
	}
	short := windows.UTF16ToString(buf[:n])
	if short == "" || strings.EqualFold(short, longDir) {
		return short, false, "GetShortPathNameW returned " + short + " for " + longDir + " (not a distinct 8.3 alias)"
	}
	return short, true, ""
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func identityCasePair(t *testing.T, content []byte) (dir, onDisk, flipped string) {
	t.Helper()
	dir = t.TempDir()
	onDisk = filepath.Join(dir, "MixedCaseBaseline.txt")
	if err := os.WriteFile(onDisk, content, 0o644); err != nil {
		t.Fatal(err)
	}
	flipped = flipBaseCase(onDisk)
	if flipped == "" {
		t.Fatal("could not build a case variant")
	}
	requireSameFileDistinctClean(t, onDisk, flipped)
	return dir, onDisk, flipped
}

func identityShortPair(t *testing.T, content []byte) (root, longPath, shortPath string) {
	t.Helper()
	root = t.TempDir()
	longDir := filepath.Join(root, "LongDirectoryNameForPathIdentity")
	if err := os.Mkdir(longDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shortDir, ok, detail := distinctShortDir(longDir)
	if !ok {
		t.Skipf("volume did not provide a distinct 8.3 alias: %s", detail)
	}
	longPath = filepath.Join(longDir, "file.txt")
	if err := os.WriteFile(longPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	shortPath = filepath.Join(shortDir, "file.txt")
	if strings.EqualFold(longPath, shortPath) {
		t.Skipf("GetShortPathNameW differed only by case: %s vs %s", longPath, shortPath)
	}
	requireSameFileDistinctClean(t, longPath, shortPath)
	return root, longPath, shortPath
}

func TestPathIdentity_WindowsCaseOCCAndCache(t *testing.T) {
	dir, onDisk, flipped := identityCasePair(t, identityActual)
	t.Run("occ-store-flipped-query-ondisk", func(t *testing.T) {
		assertOCCBaselineViaAlias(t, dir, flipped, onDisk)
	})
	t.Run("occ-store-ondisk-query-flipped", func(t *testing.T) {
		assertOCCBaselineViaAlias(t, dir, onDisk, flipped)
	})

	cacheDir, cacheOnDisk, cacheFlipped := identityCasePair(t, []byte("one\ntwo\nthree\n"))
	t.Run("cache-warm-ondisk-range-flipped", func(t *testing.T) {
		assertRangeHitsWarmCache(t, cacheDir, cacheOnDisk, cacheFlipped)
	})
	t.Run("cache-warm-flipped-range-ondisk", func(t *testing.T) {
		assertRangeHitsWarmCache(t, cacheDir, cacheFlipped, cacheOnDisk)
	})
	t.Run("cache-warm-flipped-range-flipped", func(t *testing.T) {
		assertRangeHitsWarmCache(t, cacheDir, cacheFlipped, cacheFlipped)
	})
	t.Run("invalidate", func(t *testing.T) {
		assertAliasInvalidation(t, cacheDir, cacheOnDisk, cacheFlipped)
	})
	t.Run("external-change", func(t *testing.T) {
		assertExternalChangeVisible(t, cacheDir, cacheOnDisk, cacheFlipped)
	})
}

func TestPathIdentity_WindowsShortNameOCCAndCache(t *testing.T) {
	root, longPath, shortPath := identityShortPair(t, identityActual)
	t.Run("occ-store-short-query-long", func(t *testing.T) {
		assertOCCBaselineViaAlias(t, root, shortPath, longPath)
	})
	t.Run("occ-store-long-query-short", func(t *testing.T) {
		assertOCCBaselineViaAlias(t, root, longPath, shortPath)
	})

	cacheRoot, cacheLong, cacheShort := identityShortPair(t, []byte("one\ntwo\nthree\n"))
	t.Run("cache-warm-long-range-short", func(t *testing.T) {
		assertRangeHitsWarmCache(t, cacheRoot, cacheLong, cacheShort)
	})
	t.Run("cache-warm-short-range-long", func(t *testing.T) {
		assertRangeHitsWarmCache(t, cacheRoot, cacheShort, cacheLong)
	})
	t.Run("invalidate", func(t *testing.T) {
		assertAliasInvalidation(t, cacheRoot, cacheLong, cacheShort)
	})
}

func TestPathIdentity_WritePreservesOnDiskCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MixedCaseBaseline.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	flipped := flipBaseCase(path)
	if flipped == "" {
		t.Fatal("could not build a case variant")
	}
	engine := newIdentityEngine(t, dir)
	if err := engine.WriteFileContent(context.Background(), flipped, "new"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name() == "MixedCaseBaseline.txt" {
			found = true
		}
		if entry.Name() != "MixedCaseBaseline.txt" && strings.EqualFold(entry.Name(), "MixedCaseBaseline.txt") {
			t.Fatalf("write changed on-disk case to %s", entry.Name())
		}
	}
	if !found {
		t.Fatal("original case name missing after write")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("content=%q", body)
	}
}
