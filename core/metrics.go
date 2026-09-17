package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const latencySampleCap = 256

type latencyRing struct {
	samples [latencySampleCap]time.Duration
	n       int
	i       int
}

func (r *latencyRing) add(d time.Duration) {
	r.samples[r.i] = d
	r.i = (r.i + 1) % latencySampleCap
	if r.n < latencySampleCap {
		r.n++
	}
}

func (r *latencyRing) percentile(p float64) time.Duration {
	if r.n == 0 {
		return 0
	}
	cp := make([]time.Duration, r.n)
	if r.n < latencySampleCap {
		copy(cp, r.samples[:r.n])
	} else {
		copy(cp, r.samples[:])
	}
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(float64(r.n-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func (e *UltraFastEngine) RecordMCPCall(d time.Duration, status string, mutating, dryRun bool) {
	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()
	e.metrics.MCPCalls++
	e.metrics.LatencySum += d
	e.metrics.LatencyCount++
	e.metrics.mcpLatency.add(d)
	switch status {
	case "error":
		e.metrics.MCPErrors++
		if mutating && !dryRun {
			e.metrics.MutationsRejected++
		}
	case "ok":
		if mutating && dryRun {
			e.metrics.MutationsSimulated++
		} else if mutating {
			e.metrics.MutationsApplied++
		}
	}
}

func (e *UltraFastEngine) GetPerformanceStats() string {
	e.metrics.mu.RLock()
	defer e.metrics.mu.RUnlock()

	uptime := time.Since(e.metrics.StartedAt)
	avg := time.Duration(0)
	if e.metrics.LatencyCount > 0 {
		avg = e.metrics.LatencySum / time.Duration(e.metrics.LatencyCount)
	}
	p50 := e.metrics.mcpLatency.percentile(0.50)
	p95 := e.metrics.mcpLatency.percentile(0.95)
	hits, misses := int64(0), int64(0)
	if e.cache != nil {
		hits, misses = e.cache.GetHitMiss()
	}

	if e.config.CompactMode {
		return fmt.Sprintf("mcp:%d intern:%d ops/s:%.1f hit:%.1f%% p50:%s applied:%d rejected:%d simulated:%d",
			e.metrics.MCPCalls,
			e.metrics.OperationsTotal,
			e.metrics.OperationsPerSecond,
			e.metrics.CacheHitRate*100,
			p50.Truncate(time.Microsecond),
			e.metrics.MutationsApplied,
			e.metrics.MutationsRejected,
			e.metrics.MutationsSimulated)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Performance Statistics:\n")
	fmt.Fprintf(&b, "Process started: %s\n", e.metrics.StartedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Uptime: %s\n", uptime.Truncate(time.Second))
	fmt.Fprintf(&b, "Measurement window: last metrics interval (ops/s uses MCP-call deltas)\n")
	fmt.Fprintf(&b, "MCP calls: %d (errors %d) — this stats query is included\n", e.metrics.MCPCalls, e.metrics.MCPErrors)
	fmt.Fprintf(&b, "Internal engine ops: %d (sub-operations, not MCP calls)\n", e.metrics.OperationsTotal)
	fmt.Fprintf(&b, "Operations/Second: %.2f (MCP calls / last interval)\n", e.metrics.OperationsPerSecond)
	fmt.Fprintf(&b, "Latency avg/p50/p95: %s / %s / %s (MCP handler samples, n=%d, cap %d)\n",
		avg.Truncate(time.Microsecond), p50.Truncate(time.Microsecond), p95.Truncate(time.Microsecond),
		e.metrics.LatencyCount, latencySampleCap)
	fmt.Fprintf(&b, "Mutations applied/rejected/simulated: %d / %d / %d\n",
		e.metrics.MutationsApplied, e.metrics.MutationsRejected, e.metrics.MutationsSimulated)
	fmt.Fprintf(&b, "Cache hits/misses: %d / %d (hit rate %.2f%%)\n", hits, misses, e.metrics.CacheHitRate*100)
	fmt.Fprintf(&b, "Memory Usage: %s\n", formatSize(e.metrics.MemoryUsage))
	fmt.Fprintf(&b, "Internal read/write/list/search: %d / %d / %d / %d\n",
		e.metrics.ReadOperations, e.metrics.WriteOperations, e.metrics.ListOperations, e.metrics.SearchOperations)
	fmt.Fprintf(&b, "Process edit ops: %d (not historical backups on disk)\n", e.metrics.EditOperations)
	fmt.Fprintf(&b, "Restarting the process resets these counters.\n")
	return b.String()
}
