# Benchmark Suite

Standard Go benchmarks for the `core` package. Useful for detecting perf regressions across releases and comparing changes locally.

## Running

```bash
# All benchmarks (recommended for a baseline)
go test ./core/ -run=xxx -bench=. -benchmem -benchtime=3s

# Single benchmark
go test ./core/ -run=xxx -bench=BenchmarkReadFile_Large -benchmem

# Parallel-read scalability across worker counts
go test ./core/ -run=xxx -bench=BenchmarkParallelReads -cpu=1,2,4,8,16 -benchtime=2s
```

Flags:
- `-run=xxx` disables unit tests so only `Benchmark*` functions run.
- `-benchmem` prints allocations per op (essential for memory regressions).
- `-benchtime=3s` gives each benchmark a steady-state window. Use `1x` for a smoke test.
- `-cpu=...` varies `GOMAXPROCS` for `BenchmarkParallelReads` to surface contention.

## What each benchmark measures

| Benchmark | What it covers | Why it matters |
|-----------|---------------|----------------|
| `BenchmarkReadFile_Small` | Cold 4 KB read through `ReadFileContent` | Small-file fast path, cache miss cost |
| `BenchmarkReadFile_Medium` | Cold 200 KB read | Streaming threshold boundary |
| `BenchmarkReadFile_Large` | Cold 2 MB read | Chunk path + BigCache insertion |
| `BenchmarkReadFile_CacheHit` | Warm 50 KB read (cache already populated) | Steady-state latency for repeat reads |
| `BenchmarkReadFileRange` | Partial-read of 100 lines from a 1 MB file | Range fast path vs full-file read |
| `BenchmarkWriteFile_Small` | 4 KB write | Invalidation + cache write-through |
| `BenchmarkWriteFile_Large` | 1 MB write | Streaming writer, backup bypass |
| `BenchmarkEditFile` | Full edit cycle (read → patch → write + backup) | End-to-end edit latency with safety layer |
| `BenchmarkParallelReads` | 8-file warm pool under `RunParallel` | Lock contention + worker pool scaling |

## Comparing runs

```bash
# Save baseline
go test ./core/ -run=xxx -bench=. -benchmem -benchtime=3s > bench-baseline.txt

# After a change, capture new numbers
go test ./core/ -run=xxx -bench=. -benchmem -benchtime=3s > bench-current.txt

# Compare with benchstat (go install golang.org/x/perf/cmd/benchstat@latest)
benchstat bench-baseline.txt bench-current.txt
```

## Interpreting results

- `ns/op` — latency per call. Lower is better.
- `MB/s` — throughput (only reported for ops with `SetBytes`).
- `B/op` and `allocs/op` (with `-benchmem`) — memory pressure. A regression here often precedes a user-visible slowdown.

## Notes

- Benchmarks use `b.TempDir()` so they never touch the real backup directory.
- The cache is per-engine, so each benchmark starts cold unless explicitly warmed (see `BenchmarkReadFile_CacheHit` / `BenchmarkParallelReads`).
- Edit benchmark calls `b.StopTimer()` while resetting the file; the reported time reflects only the actual edit operation.
