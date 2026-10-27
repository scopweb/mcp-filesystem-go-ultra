package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCheckAutoOCC covers the core auto-OCC logic (new point 4): it fires only
// when the session has a fresh known hash that differs from the on-disk hash,
// and a write the session itself made updates the baseline (no false positive).
func TestCheckAutoOCC(t *testing.T) {
	defer SetAutoOCCMode("warn") // restore default for other tests
	SetAutoOCCMode("warn")

	p := filepath.Join(t.TempDir(), "test-a")

	// No known hash yet → never signals.
	if sig := CheckAutoOCC(p, "aaaaaaaa"); sig.Status != FeedbackOK {
		t.Fatalf("expected OK with no known hash, got %v", sig.Pattern)
	}

	RecordReadHash(p, "aaaaaaaa")
	// Disk matches what we read → OK.
	if sig := CheckAutoOCC(p, "aaaaaaaa"); sig.Status != FeedbackOK {
		t.Errorf("expected OK when disk matches known, got %v", sig.Pattern)
	}
	// Disk differs (external change) → non-blocking warn in warn mode.
	sig := CheckAutoOCC(p, "bbbbbbbb")
	if sig.Status == FeedbackOK || sig.Pattern != PatternExternalChange {
		t.Errorf("expected external_change signal, got status=%v pattern=%v", sig.Status, sig.Pattern)
	}
	if sig.BlockOp {
		t.Error("warn mode must not block")
	}

	// Our own write updates the baseline → no false positive next time.
	RecordWriteHash(p, "cccccccc")
	if sig := CheckAutoOCC(p, "cccccccc"); sig.Status != FeedbackOK {
		t.Errorf("own write should not trigger external-change, got %v", sig.Pattern)
	}
}

func TestCheckAutoOCC_ModeBlockAndOff(t *testing.T) {
	defer SetAutoOCCMode("warn")

	p := filepath.Join(t.TempDir(), "test-b")
	RecordReadHash(p, "11111111")

	SetAutoOCCMode("block")
	if sig := CheckAutoOCC(p, "22222222"); sig.Status == FeedbackOK || !sig.BlockOp {
		t.Errorf("block mode should return a blocking signal, got status=%v block=%v", sig.Status, sig.BlockOp)
	}

	SetAutoOCCMode("off")
	if sig := CheckAutoOCC(p, "33333333"); sig.Status != FeedbackOK {
		t.Errorf("off mode should never signal, got %v", sig.Pattern)
	}
}

func TestSetAutoOCCMode_FallbackToWarn(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("nonsense")
	p := filepath.Join(t.TempDir(), "test-c")
	RecordReadHash(p, "deadbeef")
	// Fallback is "warn" → an external change signals but does not block.
	sig := CheckAutoOCC(p, "feedface")
	if sig.Status == FeedbackOK || sig.BlockOp {
		t.Errorf("unknown mode should fall back to warn, got status=%v block=%v", sig.Status, sig.BlockOp)
	}
}

// With several conversations multiplexed through one process, a baseline older
// than autoOCCBlockWindow may belong to another chat: block degrades to warn.
func TestCheckAutoOCC_BlockDegradesToWarnForOldBaseline(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("block")

	p := filepath.Join(t.TempDir(), "test-d")
	RecordReadHash(p, "11111111")

	// Fresh baseline → blocks.
	if sig := CheckAutoOCC(p, "22222222"); !sig.BlockOp {
		t.Fatalf("fresh baseline in block mode must block, got %+v", sig)
	}

	// Age the baseline past the block window but inside the stale window.
	key := CanonicalOCCKey(p)
	globalSession.mu.Lock()
	globalSession.lastRead[key] = time.Now().Add(-(autoOCCBlockWindow + time.Second))
	globalSession.mu.Unlock()

	sig := CheckAutoOCC(p, "22222222")
	if sig.Status == FeedbackOK || sig.Pattern != PatternExternalChange {
		t.Fatalf("aged baseline must still warn, got %+v", sig)
	}
	if sig.BlockOp {
		t.Errorf("baseline older than %v must not block", autoOCCBlockWindow)
	}
	if !strings.Contains(sig.Message, "another conversation") {
		t.Errorf("message should explain the degrade, got %q", sig.Message)
	}

	// Past the stale window → no baseline at all.
	globalSession.mu.Lock()
	globalSession.lastRead[key] = time.Now().Add(-(autoOCCStaleWindow + time.Second))
	globalSession.mu.Unlock()
	if sig := CheckAutoOCC(p, "22222222"); sig.Status != FeedbackOK {
		t.Errorf("baseline older than %v must be ignored, got %+v", autoOCCStaleWindow, sig)
	}
}

// Two interleaved readers in one process: B's edit after A's read must not be
// refused on A's baseline once that baseline has aged.
func TestCheckAutoOCC_InterleavedConversationsDoNotBlockEachOther(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("block")

	p := filepath.Join(t.TempDir(), "test-e")
	key := CanonicalOCCKey(p)

	// Conversation A read the file a while ago.
	RecordReadHash(p, "aaaaaaaa")
	globalSession.mu.Lock()
	globalSession.lastRead[key] = time.Now().Add(-3 * time.Minute)
	globalSession.mu.Unlock()

	// Conversation B never read it; the file meanwhile changed on disk.
	if sig := CheckAutoOCC(p, "bbbbbbbb"); sig.BlockOp {
		t.Fatalf("B must not be blocked by A's stale baseline, got %+v", sig)
	}
}
