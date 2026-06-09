package core

import (
	"errors"
	"strings"
	"testing"
)

// TestApplyAdaptiveWriteBlock exercises every decision branch in the
// adaptive downgrade logic. The helper is a pure function over a
// `FeedbackSignal` + injected backup creator, so no engine, no filesystem,
// no real BackupManager — fast and deterministic.
func TestApplyAdaptiveWriteBlock(t *testing.T) {
	// Factory helpers
	truncationSignal := func() *FeedbackSignal {
		return &FeedbackSignal{
			Status:     FeedbackKO,
			Pattern:    PatternTruncation,
			Message:    "new content (400 B) is less than 50% of existing file (1000 B)",
			Suggestion: "Read the full file first, then use edit_file for partial changes.",
			BlockOp:    true,
		}
	}
	inflationSignal := func() *FeedbackSignal {
		return &FeedbackSignal{
			Status:     FeedbackKO,
			Pattern:    PatternInflationLoop,
			Message:    "new content (70 KB) is 3x the existing file (20 KB)",
			Suggestion: "Use edit_file for surgical changes instead of write_file.",
			BlockOp:    true,
		}
	}
	okBackup := func(id string) AdaptiveWriteBackupCreator {
		return func(_, _, _ string) (string, error) { return id, nil }
	}
	failingBackup := AdaptiveWriteBackupCreator(func(_, _, _ string) (string, error) {
		return "", errors.New("disk full")
	})

	cases := []struct {
		name             string
		signal           *FeedbackSignal
		backupAvailable  bool
		existingSize     int64
		newSize          int64
		creator          AdaptiveWriteBackupCreator
		wantBlocked      bool
		wantStatus       FeedbackStatus
		wantDowngraded   bool
		wantBackupID     string
		wantSuggestionHas string
	}{
		{
			name:            "truncation + no backup → keep block",
			signal:          truncationSignal(),
			backupAvailable: false,
			existingSize:    1000, newSize: 400,
			creator:         okBackup("irrelevant"),
			wantBlocked:     true,
			wantStatus:      FeedbackKO,
			wantDowngraded:  false,
			wantBackupID:    "",
		},
		{
			name:            "truncation + backup but creator fails → keep block",
			signal:          truncationSignal(),
			backupAvailable: true,
			existingSize:    1000, newSize: 400,
			creator:         failingBackup,
			wantBlocked:     true,
			wantStatus:      FeedbackKO,
			wantDowngraded:  false,
			wantBackupID:    "",
		},
		{
			name:             "truncation + backup + creator OK → downgrade with restore cmd",
			signal:           truncationSignal(),
			backupAvailable:  true,
			existingSize:     1000, newSize: 400,
			creator:          okBackup("20260604-130xxx"),
			wantBlocked:      false,
			wantStatus:       FeedbackWarn,
			wantDowngraded:   true,
			wantBackupID:     "20260604-130xxx",
			wantSuggestionHas: "20260604-130xxx",
		},
		{
			name:            "inflation + no backup → keep block",
			signal:          inflationSignal(),
			backupAvailable: false,
			existingSize:    20000, newSize: 70000,
			creator:         okBackup("irrelevant"),
			wantBlocked:     true,
			wantStatus:      FeedbackKO,
			wantDowngraded:  false,
		},
		{
			name:             "inflation + backup + creator OK → downgrade",
			signal:           inflationSignal(),
			backupAvailable:  true,
			existingSize:     20000, newSize: 70000,
			creator:          okBackup("backup-456"),
			wantBlocked:      false,
			wantStatus:       FeedbackWarn,
			wantDowngraded:   true,
			wantBackupID:     "backup-456",
			wantSuggestionHas: "backup-456",
		},
		{
			name:            "full_rewrite signal (not block) → no-op regardless of backup",
			signal: &FeedbackSignal{
				Status:     FeedbackWarn,
				Pattern:    PatternFullRewrite,
				Message:    "Full rewrite of foo.go (20 KB → 15 KB).",
				Suggestion: "Prefer edit_file for targeted changes.",
				BlockOp:    false,
			},
			backupAvailable: true,
			existingSize:    20000, newSize: 15000,
			creator:         okBackup("should-not-be-called"),
			wantBlocked:     false,
			wantStatus:      FeedbackWarn,
			wantDowngraded:  false,
			wantBackupID:    "",
		},
		{
			name:            "OK signal (no pattern) → no-op",
			signal:          OK(),
			backupAvailable: true,
			existingSize:    5000, newSize: 5000,
			creator:         okBackup("should-not-be-called"),
			wantBlocked:     false,
			wantStatus:      FeedbackOK,
			wantDowngraded:  false,
			wantBackupID:    "",
		},
		{
			name:            "nil signal → no-op (defensive)",
			signal:          nil,
			backupAvailable: true,
			existingSize:    1000, newSize: 500,
			creator:         okBackup("irrelevant"),
			wantBlocked:     false,
			wantStatus:      "",
			wantDowngraded:  false,
			wantBackupID:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotID := ApplyAdaptiveWriteBlock(
				tc.signal, tc.backupAvailable, "/tmp/foo",
				tc.newSize, tc.existingSize, tc.creator,
			)

			if got != nil && got.BlockOp != tc.wantBlocked {
				t.Errorf("BlockOp = %v, want %v", got.BlockOp, tc.wantBlocked)
			}
			if got != nil && got.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got != nil && got.Downgraded != tc.wantDowngraded {
				t.Errorf("Downgraded = %v, want %v", got.Downgraded, tc.wantDowngraded)
			}
			if gotID != tc.wantBackupID {
				t.Errorf("returned backupID = %q, want %q", gotID, tc.wantBackupID)
			}
			if tc.wantSuggestionHas != "" {
				if got == nil || !strings.Contains(got.Suggestion, tc.wantSuggestionHas) {
					t.Errorf("Suggestion %q does not contain %q", got.Suggestion, tc.wantSuggestionHas)
				}
			}
		})
	}
}

// TestApplyAdaptiveWriteBlock_RestoreCommandFormat pins the exact
// restore-command template the suggestion must follow. If the format
// ever drifts, the user-facing recovery story breaks.
func TestApplyAdaptiveWriteBlock_RestoreCommandFormat(t *testing.T) {
	signal := &FeedbackSignal{
		Status:     FeedbackKO,
		Pattern:    PatternTruncation,
		Message:    "msg",
		Suggestion: "original suggestion",
		BlockOp:    true,
	}
	creator := AdaptiveWriteBackupCreator(func(_, _, _ string) (string, error) {
		return "backup-abc", nil
	})

	got, _ := ApplyAdaptiveWriteBlock(signal, true, "/x", 100, 500, creator)
	if got == nil {
		t.Fatal("got nil signal")
	}
	want := `Backup created: backup-abc. To undo: backup(action:"restore", backup_id:"backup-abc"). original suggestion`
	if got.Suggestion != want {
		t.Errorf("Suggestion = %q\n          want %q", got.Suggestion, want)
	}
}
