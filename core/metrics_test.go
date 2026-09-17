package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
)

func TestMetrics_OpsPerSecUsesDelta(t *testing.T) {
	dir := t.TempDir()
	c, _ := cache.NewIntelligentCache(4 * 1024 * 1024)
	e, err := NewUltraFastEngine(&Config{Cache: c, AllowedPaths: []string{dir}, ParallelOps: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.metrics.MCPCalls = 10
	e.metrics.LastUpdateTime = time.Now().Add(-2 * time.Second)
	e.metrics.lastMCPCalls = 4
	e.updateMetrics()
	if e.metrics.OperationsPerSecond < 2 || e.metrics.OperationsPerSecond > 5 {
		t.Fatalf("ops/s=%v want ~3", e.metrics.OperationsPerSecond)
	}
}

func TestMetrics_DryRunNotApplied(t *testing.T) {
	dir := t.TempDir()
	c, _ := cache.NewIntelligentCache(4 * 1024 * 1024)
	e, err := NewUltraFastEngine(&Config{Cache: c, AllowedPaths: []string{dir}, ParallelOps: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.RecordMCPCall(time.Millisecond, "ok", true, true)
	e.RecordMCPCall(time.Millisecond, "ok", true, false)
	e.RecordMCPCall(time.Millisecond, "error", true, false)
	e.metrics.mu.RLock()
	defer e.metrics.mu.RUnlock()
	if e.metrics.MutationsApplied != 1 || e.metrics.MutationsSimulated != 1 || e.metrics.MutationsRejected != 1 {
		t.Fatalf("applied=%d sim=%d rej=%d", e.metrics.MutationsApplied, e.metrics.MutationsSimulated, e.metrics.MutationsRejected)
	}
}

func TestMetrics_CacheHitOnSecondRead(t *testing.T) {
	dir := t.TempDir()
	c, _ := cache.NewIntelligentCache(4 * 1024 * 1024)
	e, err := NewUltraFastEngine(&Config{Cache: c, AllowedPaths: []string{dir}, ParallelOps: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello cache\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := e.ReadFileContent(ctx, p); err != nil {
		t.Fatal(err)
	}
	h1, m1 := c.GetHitMiss()
	if _, err := e.ReadFileContent(ctx, p); err != nil {
		t.Fatal(err)
	}
	h2, m2 := c.GetHitMiss()
	if h2 <= h1 {
		t.Fatalf("expected cache hit: hits %d→%d misses %d→%d", h1, h2, m1, m2)
	}
}

func TestMetrics_StatsSeparatesMCPAndInternal(t *testing.T) {
	dir := t.TempDir()
	c, _ := cache.NewIntelligentCache(4 * 1024 * 1024)
	e, err := NewUltraFastEngine(&Config{Cache: c, AllowedPaths: []string{dir}, ParallelOps: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.RecordMCPCall(2*time.Millisecond, "ok", false, false)
	text := e.GetPerformanceStats()
	if !strings.Contains(text, "MCP calls:") || !strings.Contains(text, "Internal engine ops:") {
		t.Fatalf("stats:\n%s", text)
	}
	if !strings.Contains(text, "not historical backups") {
		t.Fatalf("missing backup disclaimer:\n%s", text)
	}
}
