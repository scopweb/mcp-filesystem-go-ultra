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

// channelHandler is a slog handler that sends every message to a channel.
//
// The probe logs from its own goroutine and the test has to read that output
// to know when the probe finished. A shared buffer would be a data race (the
// race detector on Linux rejects it), and swapping the process-wide logger
// would be another: the test passes its own logger instead, and a channel
// carries the messages between the two goroutines.
type channelHandler struct {
	ch chan string
}

func (h *channelHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *channelHandler) Handle(_ context.Context, r slog.Record) error {
	select {
	case h.ch <- r.Message:
	default:
	}
	return nil
}

func (h *channelHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *channelHandler) WithGroup(string) slog.Handler { return h }

// The warning path must be reachable: the probe has to log something in every
// case, so the operator can tell "checked and fine" from "not checked".
func TestWarnIfGitLFSVulnerableLogs(t *testing.T) {
	ch := make(chan string, 8)
	logger := slog.New(&channelHandler{ch: ch})

	warnIfGitLFSVulnerableWith(logger)

	select {
	case msg := <-ch:
		if !strings.Contains(msg, "git-lfs") {
			t.Fatalf("the probe logged %q, expected a git-lfs message", msg)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the probe logged nothing")
	}
}
