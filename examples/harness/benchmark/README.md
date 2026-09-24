# PR-0 Failure Intelligence baseline

This is the v4.7 gate. It measures current wire behavior behind `cmd/proxy` before PR-1 changes errors or PR-4 adds `detail`.

## Scenarios

| Scenario | Calls |
|----------|-------|
| `explore_2k` | `list_allowed_directories` → `directory_tree` → `search_files` on 2,000 generated Go files |
| `occ_clash` | write → read hash → external write → stale `edit_file` |
| `patch_fail` | unified diff whose context does not apply |
| `help_catalog` | `help()` without `tool` |

The runner never retries a failed operation. `retries` therefore records actual replay attempts (currently zero), not a guess.

## Suites

| `-suite` | Profile | What |
|----------|---------|------|
| `pr0` (default) | strict | Original Failure Intelligence baseline |
| `reliability` | ultra | Scripted gate for FIABILIDAD-OPERATIVA: search pagination, multi_edit diagnosis, `project_replace` `detail:full`, `git remote` |
| `all` | ultra | Both |

`reliability` is **not** a real-model eval. It proves the three field-report failures stay fixed on the wire. P1 in ROADMAP.md (Claude Code / Codex / OpenCode on a real repo) stays open.

## Run

Build the server and proxy from the repository root:

```powershell
go build -o .\filesystem-ultra.exe .\cmd\filesystem-ultra
go build -o .\mcp-proxy.exe .\cmd\proxy
go run .\examples\harness\benchmark -proxy .\mcp-proxy.exe -server .\filesystem-ultra.exe -out .\examples\harness\benchmark\sample-report.json
go run .\examples\harness\benchmark -suite reliability -proxy .\mcp-proxy.exe -server .\filesystem-ultra.exe -out .\examples\harness\benchmark\reliability-report.json
```

Linux/macOS:

```bash
go build -o ./filesystem-ultra ./cmd/filesystem-ultra
go build -o ./mcp-proxy ./cmd/proxy
go run ./examples/harness/benchmark -proxy ./mcp-proxy -server ./filesystem-ultra -out ./examples/harness/benchmark/sample-report.json
go run ./examples/harness/benchmark -suite reliability -proxy ./mcp-proxy -server ./filesystem-ultra -out ./examples/harness/benchmark/reliability-report.json
```

The generated workspace is temporary. The committed `sample-report.json` is redacted: no source paths or response bodies, only proxy metrics.

## Cache suite (real TTL baseline)

`-suite cache` is separate from `all` so the existing fast suites do not acquire a multi-minute wait. Use `ultra` because `server_info` exposes additive `structuredContent.cache` counters on `action:stats` (no new mode or output schema; existing text preserved).

Prepare an authorized artifact directory, then from the repository root:

```powershell
go build -trimpath -o C:\temp\cache-baseline\filesystem-ultra.exe ./cmd/filesystem-ultra
go build -trimpath -o C:\temp\cache-baseline\mcp-proxy.exe ./cmd/proxy
go build -trimpath -o C:\temp\cache-baseline\benchmark.exe ./examples/harness/benchmark
C:\temp\cache-baseline\benchmark.exe -suite cache -proxy C:\temp\cache-baseline\mcp-proxy.exe -server C:\temp\cache-baseline\filesystem-ultra.exe -cache-dir C:\temp\cache-baseline -cache-repetitions 3 -cache-wait 190s -out C:\temp\cache-baseline\cache-report.json
C:\temp\cache-baseline\benchmark.exe -cache-summary C:\temp\cache-baseline\cache-report.json -out C:\temp\cache-baseline\cache-summary.json
```

The runner creates a unique retained workspace below `-cache-dir`. Give the outer command a timeout of at least 5 minutes (600000 ms recommended). TTLs are fixed at production values 3m and 10m; wait must be strictly between them. `-cache-summary` is offline and does not repeat the experiment.

- Same deterministic 24-file corpus for every process: 1/8/32/128 KiB, six files per size, 1,038,336 bytes total. Files are isolated one per directory; third-access sibling prefetch finds no candidates. Measured prefetch activity must remain zero.
- For each alternating 3m/10m instance: initial full read, immediate repetition. The runner holds the instances idle together, waits 190s from the **latest initial completion**, then measures aged reads sequentially. Completion bounds the last insertion from above; the earliest initial request bounds it from below. Immediate reads must be actual measured hits with zero disk loads, so they do not renew insertions. Aged bounds must remain >3m and <10m for the entire phase.
- Then terminate each old MCP and launch a fresh proxy/MCP, reading the same original corpus again. OS cache is never purged; seeding already warms it. Neither the initial phase nor restart is an OS-cold measurement. OS cache residence is not directly measured.
- Measurements are sequential; only idle waits overlap. `--reap-stale=false` avoids interference with other MCP instances. Graceful stdin closure drains logs before process exit, with a bounded kill fallback.
- `read_file(encoding:base64)` calls `ReadFileSnapshot` → `GetFileFresh` → deduplicated disk load. Full decoded bytes are SHA256-checked. This avoids projection/truncation; base64, hashing, JSON and sandbox checks are part of the workload.
- Proxy additive `duration_ns` uses QPC on Windows (a zero/coarse Go timestamp was observed during regression testing), Go monotonic time elsewhere. Percentiles use nearest rank on raw nanoseconds. `duration_ms` remains for old reports. Runner timing includes request serialization, wire, proxy log/forward and JSON decoding; first-useful also includes initialize, discovery, stats and content verification.
- Each phase records raw request IDs, bytes, errors, proxy/runner durations, before/after file-demand hits/misses, physical captures and tracked resident bytes. Stats queries do not perform corpus lookups. `disk_loads` counts original-file captures, not physical device I/O. Tracked resident bytes expire lazily and can include expired content until lookup; capacity is reserved storage, not RSS. Do not infer hits from latencies or residency.
- Gates reject unexpected hit/miss/load counts, prefetch, invalid content, missing proxy samples or age-window violations. Partial reports retain `complete:false` and an error. The offline summary rejects incomplete runs and pools raw samples rather than averaging percentiles.

[Executed baseline, 2026-09-21](cache-baseline-20260921.md): three repetitions per configuration, complete raw artifacts retained outside the repository. Unit tests validate planning, corpus isolation, scenario gates and report math without TTL sleeps.

## Report fields

`calls`, `tokens_in`, `tokens_out`, `duration_ms`, `occ_mismatch`, `patch_failed`, `retries`, and `errors` are grouped per scenario. `tokens_*` are proxy estimates (`bytes / 4`). `by_tool` ranks the tools that emitted the most output and supplies the PR-4 decision for which tools can receive `detail=summary|normal|full`.

The committed baseline predates PR-1 and records `PATCH_APPLY_FAILED`. The runner accepts that legacy spelling and stable `PATCH_FAILED`; both represent a non-retryable patch failure.
