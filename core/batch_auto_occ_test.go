package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBatchRefreshesAutoOCCBaseline verifies the fix: after a batch modifies a
// file the session had read, auto-OCC uses the batch's result as the new
// baseline (no false "external change" on the next edit).
func TestBatchRefreshesAutoOCCBaseline(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("warn")

	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Session read records the original baseline.
	RecordReadHash(NormalizePath(path), contentHashFNV("original\n"))

	mgr := NewBatchOperationManager(t.TempDir(), 10)
	res := mgr.ExecuteBatch(BatchRequest{
		Operations: []FileOperation{{Type: "write", Path: path, Content: "changed by batch\n"}},
	})
	if !res.Success {
		t.Fatalf("batch failed: %v", res.Errors)
	}

	// Auto-OCC against the NEW on-disk hash must be OK — the batch refreshed the
	// baseline, so the session's own write isn't flagged as external.
	newHash := contentHashFNV("changed by batch\n")
	if sig := CheckAutoOCC(NormalizePath(path), newHash); sig.Status != FeedbackOK {
		t.Errorf("expected OK after batch refreshed baseline, got status=%v pattern=%v", sig.Status, sig.Pattern)
	}
}

// TestBatchDeleteInvalidatesBaseline verifies a deleted file's baseline is
// cleared so a later check doesn't compare against a stale hash.
func TestBatchDeleteInvalidatesBaseline(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("warn")

	dir := t.TempDir()
	path := filepath.Join(dir, "g.txt")
	if err := os.WriteFile(path, []byte("data\n"), 0644); err != nil {
		t.Fatal(err)
	}
	RecordReadHash(NormalizePath(path), contentHashFNV("data\n"))

	mgr := NewBatchOperationManager(t.TempDir(), 10)
	res := mgr.ExecuteBatch(BatchRequest{
		Operations: []FileOperation{{Type: "delete", Path: path}},
	})
	if !res.Success {
		t.Fatalf("batch failed: %v", res.Errors)
	}

	globalSession.mu.Lock()
	_, present := globalSession.knownHash[NormalizePath(path)]
	globalSession.mu.Unlock()
	if present {
		t.Error("expected known hash to be cleared after batch delete")
	}
}
