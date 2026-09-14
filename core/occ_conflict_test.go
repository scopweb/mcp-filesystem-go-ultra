package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOCCConflict_ChangedRange(t *testing.T) {
	baseline := []byte("one\ntwo\nthree\n")
	actual := []byte("one\nTWO\nthree\n")
	report := BuildOCCConflict(contentHashFNV(string(baseline)), actual, baseline)
	if report.Reason != "" || report.HunkCount != 1 || report.BytesDelta != 0 {
		t.Fatalf("report=%+v", report)
	}
	if len(report.ChangedRanges) != 1 || report.ChangedRanges[0] != (ChangedLineRange{Start: 2, End: 2}) {
		t.Fatalf("ranges=%+v", report.ChangedRanges)
	}
}

func TestBuildOCCConflict_CRLF(t *testing.T) {
	baseline := []byte("one\r\ntwo\r\n")
	actual := []byte("one\r\nTWO\r\n")
	report := BuildOCCConflict(contentHashFNV(string(baseline)), actual, baseline)
	if report.Reason != "" || len(report.ChangedRanges) != 1 {
		t.Fatalf("report=%+v", report)
	}
	if got := report.ChangedRanges[0]; got != (ChangedLineRange{Start: 2, End: 2}) {
		t.Fatalf("range=%+v", got)
	}
}

func TestBuildOCCConflict_NoBaseline(t *testing.T) {
	report := BuildOCCConflict("deadbeef", []byte("current\n"), nil)
	if report.Reason != "no_baseline" || len(report.ChangedRanges) != 0 || report.HunkCount != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestIncludeOCCConflictDiff_IsBounded(t *testing.T) {
	baseline := []byte(strings.Repeat("a", maxOCCConflictDiffBytes+100) + "\n")
	actual := []byte(strings.Repeat("b", maxOCCConflictDiffBytes+100) + "\n")
	report := BuildOCCConflict(contentHashFNV(string(baseline)), actual, baseline)
	IncludeOCCConflictDiff(&report, actual, baseline, "conflict.txt")
	if len(report.Diff) != maxOCCConflictDiffBytes {
		t.Fatalf("diff length=%d", len(report.Diff))
	}
}

func TestBeginFileTxn_OCCConflictUsesMatchingBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conflict.txt")
	baseline := []byte("one\ntwo\nthree\n")
	actual := []byte("one\nTWO\nthree\n")
	if err := os.WriteFile(path, actual, 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)
	defer engine.Close()
	if _, err := engine.GetBackupManager().CreateBackupFromBytes(path, baseline, 0644, "test", "", ""); err != nil {
		t.Fatal(err)
	}
	ctx := WithExpectedHash(context.Background(), contentHashFNV(string(baseline)))
	_, txn, err := engine.BeginFileTxn(ctx, path, false)
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
