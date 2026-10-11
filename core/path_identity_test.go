package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
)

var (
	identityBaseline = []byte("one\ntwo\nthree\n")
	identityActual   = []byte("one\nTWO\nthree\n")
)

func newIdentityEngine(t *testing.T, allowed string) *UltraFastEngine {
	t.Helper()
	cacheInstance, err := cache.NewIntelligentCache(8 << 20)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewUltraFastEngine(&Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowed},
		ParallelOps:  2,
		BackupDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

func requireSameFileDistinctClean(t *testing.T, a, b string) {
	t.Helper()
	infoA, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat %s: %v", a, err)
	}
	infoB, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat %s: %v", b, err)
	}
	if !os.SameFile(infoA, infoB) {
		t.Fatalf("not the same file:\n  %s\n  %s", a, b)
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		t.Fatalf("paths do not differ after Clean:\n  %s\n  %s", a, b)
	}
}

func logPathIdentity(t *testing.T, engine *UltraFastEngine, store, query string) {
	t.Helper()
	t.Logf("store=%s", store)
	t.Logf("query=%s", query)
	t.Logf("NormalizePath store=%s", NormalizePath(store))
	t.Logf("NormalizePath query=%s", NormalizePath(query))
	for _, item := range []struct {
		label string
		path  string
	}{
		{"store", store},
		{"query", query},
	} {
		if !engine.IsPathAllowed(item.path) {
			t.Fatalf("access denied for %s %s", item.label, item.path)
		}
		resolved, err := engine.ResolveAndAuthorize("read", item.path)
		t.Logf("ResolveAndAuthorize %s=%q err=%v", item.label, resolved, err)
		if err != nil {
			t.Fatalf("resolve %s: %v", item.label, err)
		}
		t.Logf("CanonicalOCCKey %s=%s", item.label, CanonicalOCCKey(item.path))
	}
}

func identitySymlinkPair(t *testing.T, content []byte) (root, realPath, aliasPath string) {
	t.Helper()
	root = t.TempDir()
	realDir := filepath.Join(root, "real")
	aliasDir := filepath.Join(root, "alias")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(aliasDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realPath = filepath.Join(realDir, "archivo.txt")
	if err := os.WriteFile(realPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	aliasPath = filepath.Join(aliasDir, "archivo.txt")
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Skipf("symlink not permitted in this environment: %v", err)
	}
	requireSameFileDistinctClean(t, realPath, aliasPath)
	return root, realPath, aliasPath
}

func assertOriginalPathPreserved(t *testing.T, engine *UltraFastEngine, stored string) {
	t.Helper()
	backups, err := engine.GetBackupManager().ListBackups(0, "all", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, backup := range backups {
		for _, file := range backup.Files {
			t.Logf("OriginalPath=%s", file.OriginalPath)
			if file.OriginalPath == stored {
				return
			}
		}
	}
	t.Fatalf("OriginalPath was not preserved as %q", stored)
}

func assertOCCBaselineViaAlias(t *testing.T, allowed, stored, query string) {
	t.Helper()
	if err := os.WriteFile(stored, identityActual, 0o644); err != nil {
		t.Fatal(err)
	}
	engine := newIdentityEngine(t, allowed)
	logPathIdentity(t, engine, stored, query)
	if _, err := engine.GetBackupManager().CreateBackupFromBytes(stored, identityBaseline, 0o644, "test", "", ""); err != nil {
		t.Fatal(err)
	}
	assertOriginalPathPreserved(t, engine, stored)
	ctx := WithExpectedHash(context.Background(), contentHashFNV(string(identityBaseline)))
	_, txn, err := engine.BeginFileTxn(ctx, query, false)
	if err == nil {
		txn.Release()
		t.Fatal("expected OCC mismatch")
	}
	occ, ok := err.(*OCCMismatchError)
	if !ok {
		t.Fatalf("error=%T %v", err, err)
	}
	if occ.Conflict.Reason != "" || len(occ.Conflict.ChangedRanges) != 1 {
		t.Fatalf("conflict=%+v", occ.Conflict)
	}
	if got := occ.Conflict.ChangedRanges[0]; got != (ChangedLineRange{Start: 2, End: 2}) {
		t.Fatalf("range=%+v", got)
	}
}

func assertRangeHitsWarmCache(t *testing.T, allowed, warm, query string) {
	t.Helper()
	engine := newIdentityEngine(t, allowed)
	logPathIdentity(t, engine, warm, query)
	ctx := context.Background()
	if _, err := engine.ReadFileContent(ctx, warm); err != nil {
		t.Fatal(err)
	}
	before := diskReadCount.Load()
	result, err := engine.ReadFileRange(ctx, query, 1, 2)
	if err != nil {
		t.Fatalf("ReadFileRange: %v", err)
	}
	delta := diskReadCount.Load() - before
	t.Logf("DiskLoads delta=%d", delta)
	if delta != 0 {
		t.Fatalf("warm cache missed for alias range read, disk loads=%d\nresult=%q", delta, result)
	}
	if !strings.Contains(result, "one") || !strings.Contains(result, "two") {
		t.Fatalf("range content=%q", result)
	}
	wantBase := filepath.Base(query)
	if !strings.Contains(result, wantBase) {
		t.Fatalf("footer missing presentation name %q in %q", wantBase, result)
	}
	if !strings.Contains(result, "[Lines 1-2 of 3 total lines in ") {
		t.Fatalf("footer=%q", result)
	}
}

func assertAliasInvalidation(t *testing.T, allowed, warm, other string) {
	t.Helper()
	engine := newIdentityEngine(t, allowed)
	ctx := context.Background()
	if _, err := engine.ReadFileContent(ctx, warm); err != nil {
		t.Fatal(err)
	}
	resolved, err := engine.ResolveAndAuthorize("read", warm)
	if err != nil {
		t.Fatal(err)
	}
	gen := engine.cache.CaptureGeneration(resolved)
	engine.InvalidateCache(other)
	info, err := os.Stat(resolved)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if engine.cache.SetFileIfGeneration(resolved, data, cache.MetaFromInfo(info), gen) {
		t.Fatal("in-flight capture repopulated an invalidated generation")
	}
	before := diskReadCount.Load()
	got, err := engine.ReadFileContent(ctx, warm)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("content=%q", got)
	}
	if diskReadCount.Load()-before == 0 {
		t.Fatal("invalidate via alias reused cached content")
	}
}

func assertExternalChangeVisible(t *testing.T, allowed, warm, query string) {
	t.Helper()
	engine := newIdentityEngine(t, allowed)
	ctx := context.Background()
	if _, err := engine.ReadFileContent(ctx, warm); err != nil {
		t.Fatal(err)
	}
	changed := []byte("one\nCHANGED-EXTERNALLY\nthree\n")
	if err := os.WriteFile(warm, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := engine.ReadFileRange(ctx, query, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "CHANGED-EXTERNALLY") {
		t.Fatalf("external change not visible via alias: %q", result)
	}
}

func TestPathIdentity_SymlinkOCCBaseline(t *testing.T) {
	root, realPath, aliasPath := identitySymlinkPair(t, identityActual)
	t.Run("store-alias-query-real", func(t *testing.T) {
		assertOCCBaselineViaAlias(t, root, aliasPath, realPath)
	})
	t.Run("store-real-query-alias", func(t *testing.T) {
		assertOCCBaselineViaAlias(t, root, realPath, aliasPath)
	})
}

func TestPathIdentity_SymlinkRangeUsesWarmCache(t *testing.T) {
	body := []byte("one\ntwo\nthree\n")
	root, realPath, aliasPath := identitySymlinkPair(t, body)
	t.Run("warm-real-range-alias", func(t *testing.T) {
		assertRangeHitsWarmCache(t, root, realPath, aliasPath)
	})
	t.Run("warm-alias-range-real", func(t *testing.T) {
		assertRangeHitsWarmCache(t, root, aliasPath, realPath)
	})
	t.Run("warm-alias-range-alias", func(t *testing.T) {
		assertRangeHitsWarmCache(t, root, aliasPath, aliasPath)
	})
}

func TestPathIdentity_SymlinkInvalidateAndFreshness(t *testing.T) {
	body := []byte("one\ntwo\nthree\n")
	root, realPath, aliasPath := identitySymlinkPair(t, body)
	t.Run("invalidate", func(t *testing.T) {
		assertAliasInvalidation(t, root, realPath, aliasPath)
	})
	t.Run("external-change", func(t *testing.T) {
		assertExternalChangeVisible(t, root, realPath, aliasPath)
	})
}

func TestPathIdentity_InvalidateMissingStillClearsReceivedKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(path, []byte("version-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := newIdentityEngine(t, dir)
	ctx := context.Background()
	if _, err := engine.ReadFileContent(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	engine.InvalidateCache(path)
	if err := os.WriteFile(path, []byte("version-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := engine.ReadFileContent(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "version-2\n" {
		t.Fatalf("after delete+invalidate got %q", got)
	}
}

func TestPathIdentity_OCCRejectsOtherFileAndWrongHash(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(target, identityActual, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, identityBaseline, 0o644); err != nil {
		t.Fatal(err)
	}
	engine := newIdentityEngine(t, dir)
	if _, err := engine.GetBackupManager().CreateBackupFromBytes(other, identityBaseline, 0o644, "test", "", ""); err != nil {
		t.Fatal(err)
	}
	ctx := WithExpectedHash(context.Background(), contentHashFNV(string(identityBaseline)))
	_, txn, err := engine.BeginFileTxn(ctx, target, false)
	if err == nil {
		txn.Release()
		t.Fatal("expected OCC mismatch")
	}
	occ, ok := err.(*OCCMismatchError)
	if !ok {
		t.Fatalf("error=%T %v", err, err)
	}
	if occ.Conflict.Reason != "no_baseline" {
		t.Fatalf("other file with identical bytes was accepted: %+v", occ.Conflict)
	}

	engine2 := newIdentityEngine(t, dir)
	if _, err := engine2.GetBackupManager().CreateBackupFromBytes(target, []byte("nope\n"), 0o644, "test", "", ""); err != nil {
		t.Fatal(err)
	}
	_, txn, err = engine2.BeginFileTxn(ctx, target, false)
	if err == nil {
		txn.Release()
		t.Fatal("expected OCC mismatch")
	}
	occ, ok = err.(*OCCMismatchError)
	if !ok {
		t.Fatalf("error=%T %v", err, err)
	}
	if occ.Conflict.Reason != "no_baseline" {
		t.Fatalf("wrong-hash backup was accepted: %+v", occ.Conflict)
	}
}

func TestPathIdentity_DistinctCaseNamesAreNotTheSameFile(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("case-distinct names are a case-sensitive filesystem guard")
	}
	dir := t.TempDir()
	upper := filepath.Join(dir, "A.txt")
	lower := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(upper, []byte("AAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lower, []byte("aaa\n"), 0o644); err != nil {
		t.Skipf("filesystem did not keep A.txt and a.txt distinct: %v", err)
	}
	infoUpper, err := os.Stat(upper)
	if err != nil {
		t.Fatal(err)
	}
	infoLower, err := os.Stat(lower)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(infoUpper, infoLower) {
		t.Skip("filesystem folded A.txt and a.txt into one file")
	}
	engine := newIdentityEngine(t, dir)
	if _, err := engine.GetBackupManager().CreateBackupFromBytes(lower, []byte("aaa\n"), 0o644, "test", "", ""); err != nil {
		t.Fatal(err)
	}
	ctx := WithExpectedHash(context.Background(), contentHashFNV("aaa\n"))
	_, txn, err := engine.BeginFileTxn(ctx, upper, false)
	if err == nil {
		txn.Release()
		t.Fatal("expected OCC mismatch")
	}
	occ, ok := err.(*OCCMismatchError)
	if !ok {
		t.Fatalf("error=%T %v", err, err)
	}
	if occ.Conflict.Reason != "no_baseline" {
		t.Fatalf("distinct case names were treated as the same file: %+v", occ.Conflict)
	}
}
