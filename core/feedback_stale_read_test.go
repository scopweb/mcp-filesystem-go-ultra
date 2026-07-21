package core

// Regression coverage for the STALE_READ warning-noise issue: the warning
// fired on EVERY edit of a file whose pre-edit read was done with a
// non-filesystem-ultra tool (the session tracker only sees this server's
// reads). It is now emitted at most once per file per session, and is
// suppressed entirely when the caller passes expected_hash (cryptographic
// proof of a prior read).

import (
	"path/filepath"
	"testing"
)

func TestCheckEditOp_StaleReadWarnsOncePerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warn-once.txt")

	first := CheckEditOp(path, "old", 100)
	if first.Status != FeedbackWarn || first.Pattern != PatternStaleRead {
		t.Fatalf("first call should warn STALE_READ, got %v (%v)", first.Status, first.Pattern)
	}

	second := CheckEditOp(path, "old", 100)
	if second.Status != FeedbackOK {
		t.Errorf("second call for the same file must be silenced, got %v (%s)", second.Status, second.Message)
	}

	third := CheckEditOp(path, "different old_text", 100)
	if third.Status != FeedbackOK {
		t.Errorf("third call for the same file must be silenced, got %v (%s)", third.Status, third.Message)
	}
}

func TestCheckEditOp_StaleReadSuppressedWithExpectedHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "with-hash.txt")

	signal := CheckEditOp(path, "old", 100, true)
	if signal.Status != FeedbackOK {
		t.Errorf("expected_hash must suppress STALE_READ, got %v (%s)", signal.Status, signal.Message)
	}

	// Explicit false keeps the classic behavior (warn once).
	first := CheckEditOp(path, "old", 100, false)
	if first.Status != FeedbackWarn || first.Pattern != PatternStaleRead {
		t.Errorf("expected_hash=false should still warn (once), got %v (%v)", first.Status, first.Pattern)
	}
}

func TestCheckEditOp_RecordReadReArmsWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rearm.txt")

	if s := CheckEditOp(path, "old", 100); s.Status != FeedbackWarn {
		t.Fatalf("precondition: first call warns, got %v", s.Status)
	}
	if s := CheckEditOp(path, "old", 100); s.Status != FeedbackOK {
		t.Fatalf("precondition: second call silenced, got %v", s.Status)
	}

	// A real read re-arms the detector: the next stale window may warn again.
	RecordRead(path)
	if s := CheckEditOp(path, "old", 100); s.Status != FeedbackOK {
		t.Errorf("right after a read there must be no warning, got %v (%s)", s.Status, s.Message)
	}

	// Simulate the read expiring AND a fresh session state: clearing both the
	// last-read timestamp and the warned flag must produce a warning again.
	globalSession.mu.Lock()
	delete(globalSession.lastRead, path)
	delete(globalSession.staleWarned, path)
	globalSession.mu.Unlock()
	if s := CheckEditOp(path, "old", 100); s.Status != FeedbackWarn {
		t.Errorf("after re-arm, a stale edit should warn again, got %v", s.Status)
	}
}
