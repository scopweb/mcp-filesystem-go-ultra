package mcpserver

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// The probe must answer with the version of the git-lfs in PATH when there is
// one, and must not report a version it did not read.
func TestGitLFSVersionProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	v, err := gitLFSVersion(ctx)
	if err != nil {
		t.Skipf("git-lfs not available on this machine: %v", err)
	}
	if v == "" {
		t.Fatal("empty version")
	}
	if !strings.HasPrefix(v, "3.") && !strings.HasPrefix(v, "2.") {
		t.Fatalf("unexpected version %q", v)
	}
}

// The warning path must be reachable: the probe has to log something in every
// case, so the operator can tell "checked and fine" from "not checked".
func TestWarnIfGitLFSVulnerableLogs(t *testing.T) {
	var buf strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	warnIfGitLFSVulnerable()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "git-lfs") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the probe logged nothing:\n%s", buf.String())
}
