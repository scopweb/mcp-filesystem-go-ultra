package core

import (
	"os"
	"path/filepath"
	"testing"
)

// Feedback 2026-08-05 (BUG 2): after a session-initiated mutation that reverts
// or rewrites file bytes (undo_last, restore, regex/search_replace edits,
// move/copy), the known-hash baseline must be resynced from disk so the next
// edit does not raise a false external_change warning.

func TestRefreshKnownHashes_ResyncsAfterExternalByteChange(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("warn")

	dir := t.TempDir()
	p := filepath.Join(dir, "resync.txt")
	original := []byte("original content\n")
	if err := os.WriteFile(p, original, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Session reads the file (baseline recorded).
	RecordReadHash(NormalizePath(p), contentHashFNV(string(original)))

	// Simulate an undo/restore: disk bytes change back to something else.
	reverted := []byte("reverted content\n")
	if err := os.WriteFile(p, reverted, 0644); err != nil {
		t.Fatalf("revert: %v", err)
	}

	// Without resync, CheckAutoOCC would fire a false external_change.
	diskHash := contentHashFNV(string(reverted))
	if sig := CheckAutoOCC(NormalizePath(p), diskHash); sig.Status == FeedbackOK {
		t.Fatal("expected signal before resync (sanity check)")
	}

	// The mutation path resyncs from disk (what restore/undo handlers now do).
	RefreshKnownHashes([]string{p})

	if sig := CheckAutoOCC(NormalizePath(p), diskHash); sig.Status != FeedbackOK {
		t.Errorf("after RefreshKnownHashes, own restore must not signal external_change, got %v", sig.Pattern)
	}
}

func TestRefreshKnownHashes_ClearsBaselineForMissingFile(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("warn")

	dir := t.TempDir()
	p := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(p, []byte("data\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	RecordReadHash(NormalizePath(p), contentHashFNV("data\n"))

	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	RefreshKnownHashes([]string{p})

	globalSession.mu.Lock()
	_, present := globalSession.knownHash[NormalizePath(p)]
	globalSession.mu.Unlock()
	if present {
		t.Error("baseline must be cleared when the file no longer exists")
	}
}
