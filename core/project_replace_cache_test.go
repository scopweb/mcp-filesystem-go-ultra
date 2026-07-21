package core

// Regression coverage for the v4.5.29 host/sandbox separation fix.
// project_replace used to write via raw os.WriteFile, which left the read
// cache stale and the auto-OCC baseline pointing at the pre-replace bytes.
// The fix routes the per-file write through atomicWriteFile plus
// invalidateMutatedPath and RecordWriteHash so the next read_file returns
// fresh bytes and the OCC baseline matches disk.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectReplace_InvalidatesReadCacheAndRefreshesBaseline(t *testing.T) {
	dir := t.TempDir()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.config.AllowedPaths = append(engine.config.AllowedPaths, dir)

	path := filepath.Join(dir, "subject.txt")
	original := "alpha beta gamma alpha\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Warm the read cache and record the pre-replace hash as the session baseline.
	firstRead, err := engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if firstRead != original {
		t.Fatalf("read returned %q, want %q", firstRead, original)
	}
	RecordReadHash(NormalizePath(path), contentHashFNV(original))

	// Mutate via project_replace and confirm the cache + baseline were refreshed.
	if _, err := engine.ProjectReplace(context.Background(), dir, "alpha", "OMEGA", true, true, "", nil, nil, false, false, false, 100, false); err != nil {
		t.Fatalf("ProjectReplace: %v", err)
	}

	norm := NormalizePath(path)
	_, _, hit := engine.cache.GetDirectory(filepath.Dir(norm))
	if hit {
		t.Errorf("parent directory cache should have been invalidated by project_replace")
	}

	// Re-read: must return the post-replace content, not the cached pre-replace bytes.
	secondRead, err := engine.ReadFileContent(context.Background(), path)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if secondRead == original {
		t.Errorf("read_file returned stale pre-replace bytes after project_replace")
	}
	if secondRead != "OMEGA beta gamma OMEGA\n" {
		t.Errorf("read_file returned %q, want post-replace content", secondRead)
	}

	// Auto-OCC: the knownHash must match the post-replace disk hash; otherwise
	// the next edit would warn about a phantom external change.
	diskHash := contentHashFNV(secondRead)
	if signal := CheckAutoOCC(norm, diskHash); signal.Status != FeedbackOK {
		t.Errorf("CheckAutoOCC after project_replace should be OK, got %v (%s)", signal.Status, signal.Message)
	}
}
