# Roadmap

Living plan. Shipped work is [CHANGELOG.md](CHANGELOG.md). Closed plans are local copies in `docs/history/` (that folder is not in git).

## Now — P1, real agent evaluation

Still open. `TestE2E_E6_AgentEval` and `examples/harness/benchmark -suite reliability` are scripted. They do not close this.

A close needs:

- More than one MCP client and model (Claude Code, Codex, OpenCode)
- A representative repository, not a 40-file synthetic tree
- Task success, wrong edits, conflicts, retries, calls, tokens, latency p50/p95
- A real lost response, not only replaying `operation_id`
- A concurrent writer and reviewer, not a sequential pair

What does not close it: the harness pack, one OpenCode + Grok read-only session, the 2/3 concurrent writer run that left `price.go` empty, or the scripted e3 cut. Codex still has no refreshed token. Claude Haiku did not call the MCP.

## Paused

Cache phase 0 is closed (2026-09-22). TTL stays `3m`. WARM-01 stays paused, WARM-02 is not started, mmap is deferred. Reopen persistence only with evidence that read or restart cost matters on a representative workload.

## Not blocking

`apply_patch` picks the missing-base suggestion by comparing the error string. A `PatchReasonBaseMissing` would survive a wording change. Behavior is already correct.

## Out of scope

- MCP Resources / Prompts
- MCP Tasks for long operations
- Streamable HTTP / OAuth (the server is stdio)
- Reopening the View / Edit / `fs` aliases

## Closed

See the changelog. Short map:

| Work | Where |
|------|--------|
| Agent reliability E1–E6 | v4.6.x |
| Failure Intelligence PR-0–PR-6 | v4.7.0 |
| Operational recovery | v4.7.1 |
| e3 receipts, `atomic create_dir` limit, multi-file `apply_patch`, completions limit, rollback status, missing base | Unreleased, on `main` |
| Cache phase 0 | Unreleased, on `main` |
