# Eval OpenCode — mcp-filesystem-go-ultra

Date: 2026-09-21  
Client: OpenCode  
Model: grok-4.6 (`xai/grok-4.6`)  
MCP: filesystem-ultra **live binary `d2706b3` (2026-09-18)**, not HEAD `4398ae1`  
Repo: `C:\MCPs\clone\mcp-filesystem-go-ultra`  
Mutations: `C:\temp\fs-ultra-opencode-eval-20260921` (scratch; repo files not rewritten)

This is **one** real-model session. It is not Claude Code / Codex, not concurrent writer vs reviewer, and not a lost-response injection. Those remain P1.

## Tasks

| # | Task | Result |
|---|------|--------|
| 1 | `git(action:"remote")` on this repo | PASS — `origin → https://github.com/scopweb/mcp-filesystem-go-ultra.git` |
| 2 | `search_files` `func Test` `*.go` `max_results:15` | PASS — `824 matches (showing 1-15)` + `offset:15` |
| 3 | Continue `offset:15` then `offset:810` | PASS — pages 16-30 then 811-824; no reformulation; last page 14 hits |
| 4 | `count_only` same search | PASS — `824 matches in 152 files` |
| 5 | `multi_edit` valid + ambiguous on `mix.txt` | PASS — `Failed: edit 2 [ambiguous]`; mix.txt stayed 12 B |
| 6 | STALE_READ after `write_file` without `read_file` | WARN once — then silenced after `read_file` |
| 7 | `project_replace` preview compact | PASS — `3 files \| 3 replacements`, no paths |
| 8 | `project_replace` `detail:"full"` | **BLOCKED** — live `help(tool)` schema has no `detail` (`d2706b3`) |
| 9 | `server_info stats` | PASS — splits MCP vs intern; backups 707 ≠ session edits |

## Telemetry (same process)

| | Before eval tasks | After |
|--|-------------------|-------|
| mcp | 102 | 115 |
| intern | 95 | 105 |
| hit | 14.4% | 14.1% |
| p50 | 51.5 ms | 51.0 ms |
| applied / rejected / simulated | 34 / 0 / 0 | 38 / 2 / 2 |
| ops/s | 0.0 | 0.0 |
| Edit telemetry total_edits | 27 | 27 |
| Total backups | 707 | 707 |

`ops/s:0.0` with 13 MCP calls in the window still does not describe this session. Edit telemetry did not move on `multi_edit` rollback (expected: no write). `rejected:2` matches the two rolled-back `multi_edit`s. `simulated:2` matches the two `project_replace` previews.

## Findings

1. **Stale server.** OpenCode is still talking to `d2706b3`. Unreleased `detail` on `project_replace` is in git (`4398ae1`) but not in the live schema. Restart the MCP binary after pull.
2. **Search pagination works on this tree** (824 hits) without subdirectory guessing.
3. **Rollback diagnosis is usable** (`edit 2 [ambiguous]`, candidates 1,3). No `Failed: .`.
4. **`write_file` does not clear STALE_READ**; `read_file` does. Warning is once per file after that.
5. **Git remote is visible without `--git-network`.** Push was not attempted.
6. One model, one client. Does not close PLAN-PENDIENTE P1.

## Retest HEAD `4398ae1` (2026-09-21, after restart)

Same client/model/repo/scratch. Fresh process (`mcp:0` at start).

| # | Task | Result |
|---|------|--------|
| 1 | `git remote` | PASS — `origin → https://github.com/scopweb/mcp-filesystem-go-ultra.git` |
| 2–4 | search pages + `count_only` | PASS — 824 hits, offset 15 / 810, last page 811-824 |
| 5 | `multi_edit` ambiguous | PASS — `edit 2 [ambiguous]`; mix.txt 12 B unchanged |
| 7 | `project_replace` compact | PASS — counters only |
| 8 | `project_replace` `detail:"full"` | **PASS** — `a.txt: 1` / `b.txt: 1` / `c.txt: 1` |
| 9 | stats | `mcp:12 intern:12` p50:58.8ms applied:0 rejected:1 simulated:2 |

`help(tool:project_replace)` now lists `detail`. All nine tasks pass on HEAD.

STALE_READ still fired on `multi_edit` after a `read_file` of `mix.txt` in this process (no `expected_hash`). Once-per-file; does not block.

`ops/s` still `0.0` with 12 MCP calls. Edit telemetry still empty (no applied `edit_file`). Backups 709 ≠ this session.

P1 still open: one client/model; no concurrent writer/reviewer; no lost-response injection.
