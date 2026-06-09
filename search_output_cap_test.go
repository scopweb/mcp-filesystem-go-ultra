package main

import (
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// newEngineWithCap builds a minimal engine + cache for testing capSearchOutput.
// The engine doesn't need real hooks or backup manager — capSearchOutput only
// reads the config's MaxSearchOutputBytes field.
func newEngineWithCap(t *testing.T, capBytes int) *core.UltraFastEngine {
	t.Helper()
	c, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:               c,
		MaxSearchOutputBytes: capBytes,
		ParallelOps:         2,
	})
	if err != nil {
		t.Fatalf("engine init: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

// TestCapSearchOutput_BelowCap: response under the cap is returned unchanged.
func TestCapSearchOutput_BelowCap(t *testing.T) {
	engine := newEngineWithCap(t, 1024) // 1KB cap
	small := strings.Repeat("a", 500)
	got := capSearchOutput(small, engine)
	if got != small {
		t.Errorf("expected unchanged output under cap, got len=%d (input=%d)", len(got), len(small))
	}
	if strings.Contains(got, "truncated") {
		t.Error("output under cap should not contain truncation marker")
	}
}

// TestCapSearchOutput_AboveCap: response over the cap is truncated with marker.
func TestCapSearchOutput_AboveCap(t *testing.T) {
	engine := newEngineWithCap(t, 1024) // 1KB cap
	big := strings.Repeat("x", 5000)
	got := capSearchOutput(big, engine)
	if len(got) >= len(big) {
		t.Errorf("expected truncated output (len < %d), got len=%d", len(big), len(got))
	}
	if !strings.Contains(got, "⚠️ truncated") {
		t.Error("expected truncation marker in output, not found")
	}
	if !strings.Contains(got, "count_only:true") {
		t.Error("expected marker to suggest count_only:true")
	}
}

// TestCapSearchOutput_DefaultCap: 0 cap → uses DefaultMaxSearchOutputBytes (500KB).
func TestCapSearchOutput_DefaultCap(t *testing.T) {
	engine := newEngineWithCap(t, 0) // 0 → default
	big := strings.Repeat("y", 600*1024) // 600KB > 500KB default
	got := capSearchOutput(big, engine)
	if !strings.Contains(got, "truncated") {
		t.Error("expected default cap (500KB) to truncate 600KB input")
	}
}

// TestCapSearchOutput_ExactBoundary: at the cap exactly, no truncation.
func TestCapSearchOutput_ExactBoundary(t *testing.T) {
	engine := newEngineWithCap(t, 100)
	exact := strings.Repeat("z", 100) // exactly 100 bytes
	got := capSearchOutput(exact, engine)
	if got != exact {
		t.Errorf("at exact boundary expected unchanged, got len=%d (input=%d)", len(got), len(exact))
	}
	overBy1 := exact + "!"
	got2 := capSearchOutput(overBy1, engine)
	if !strings.Contains(got2, "truncated") {
		t.Error("101 bytes should trigger truncation at 100-byte cap")
	}
}
