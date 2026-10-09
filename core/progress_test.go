package core

import (
	"context"
	"testing"
)

func TestReportProgress_NoReporterIsSilent(t *testing.T) {
	ReportProgress(context.Background(), 1, 2, "scan")
}

func TestReportProgress_CancelledDoesNotCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = ContextWithProgress(ctx, func(int, int, string) {
		t.Fatal("cancelled context must not report")
	})
	ReportProgress(ctx, 1, 1, "scan")
}

func TestReportProgress_ReportsStage(t *testing.T) {
	var got string
	ctx := ContextWithProgress(context.Background(), func(done, total int, message string) {
		if done != 1 || total != 3 || message != "scan" {
			t.Fatalf("stage %d/%d %s", done, total, message)
		}
		got = message
	})
	ReportProgress(ctx, 1, 3, "scan")
	if got != "scan" {
		t.Fatal("missing report")
	}
}
