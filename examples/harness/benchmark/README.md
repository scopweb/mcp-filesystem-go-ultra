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

`reliability` is **not** a real-model eval. It proves the three field-report failures stay fixed on the wire. P1 in PLAN-PENDIENTE.md (Claude Code / Codex / OpenCode on a real repo) stays open.

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

## Report fields

`calls`, `tokens_in`, `tokens_out`, `duration_ms`, `occ_mismatch`, `patch_failed`, `retries`, and `errors` are grouped per scenario. `tokens_*` are proxy estimates (`bytes / 4`). `by_tool` ranks the tools that emitted the most output and supplies the PR-4 decision for which tools can receive `detail=summary|normal|full`.

The committed baseline predates PR-1 and records `PATCH_APPLY_FAILED`. The runner accepts that legacy spelling and stable `PATCH_FAILED`; both represent a non-retryable patch failure.
