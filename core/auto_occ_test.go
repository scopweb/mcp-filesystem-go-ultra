package core

import "testing"

// TestCheckAutoOCC covers the core auto-OCC logic (new point 4): it fires only
// when the session has a fresh known hash that differs from the on-disk hash,
// and a write the session itself made updates the baseline (no false positive).
func TestCheckAutoOCC(t *testing.T) {
	defer SetAutoOCCMode("warn") // restore default for other tests
	SetAutoOCCMode("warn")

	p := "/auto-occ/test-a"

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

	p := "/auto-occ/test-b"
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
	p := "/auto-occ/test-c"
	RecordReadHash(p, "deadbeef")
	// Fallback is "warn" → an external change signals but does not block.
	sig := CheckAutoOCC(p, "feedface")
	if sig.Status == FeedbackOK || sig.BlockOp {
		t.Errorf("unknown mode should fall back to warn, got status=%v block=%v", sig.Status, sig.BlockOp)
	}
}
