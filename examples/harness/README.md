# Harness pack — one folder, one MCP, one disk family

Copy a subdirectory (`claude-code/`, `opencode/`, or `codex/`) into the project. Do not guess flags.

## Server (every harness)

One MCP instance per workspace:

```
--profile=strict --roots-mode=union --compact-mode
```

| Flag | Why |
|------|-----|
| `--profile=strict` | 17-tool agent core (`backup` and `security_policy` included). `--profile=agent` adds `git` (18). Default `ultra` is 30 tools. |
| `--roots-mode=union` | Default. Client Roots **add** to the CLI allowlist. `replace` wipes CLI paths when the client sends Roots. |
| `--compact-mode` | Short responses (hashes, UNDO ids). |

`--readonly` off unless the role is read-only. `--git-network` only if the agent must `git push`/`fetch`. `--git-remote-allow` is optional (empty = any remote). GitHub+GitLab: `--git-remote-allow=github.com,gitlab.com`.

OpenCode V2: leave `timeout.execution` unset (default 12h). Do not copy the V1 `"timeout": 30000` into it. See `opencode/opencode.v2.jsonc.example`. V1 has one timeout; 30000 covers `tools/list` and cuts long calls.

Linux binary/root placeholders: `/path/to/filesystem-ultra` and `/path/to/workspace` (see `claude-code/mcp.json.example` / `opencode/opencode.jsonc.example` — swap the Windows paths).

## One role = one disk family

Harnesses ship native `Read` / `Edit` / `Write` / `Grep` / `Glob` (and Codex `apply_patch`) on **another sandbox**. Mixing them with filesystem-ultra in the same subagent breaks OCC and backups.

| Role | Disk family |
|------|-------------|
| Host project (`C:\`, `/mnt/…`) | filesystem-ultra only |
| Agent scratch outside the repo | native tools only |

Never mix native Read/Edit with ultra `read_file`/`edit_file` in one subagent.

## Claude Code subagents

`claude-code/.claude/agents/*.md` list each tool as `mcp__filesystem-ultra__<tool>`, matching the server key in `claude-code/mcp.json.example`. A bare name matches nothing. Do not use `mcp__filesystem-ultra__*`: it would give write tools to the read-only agents. `disallowedTools` belongs in a subagent frontmatter, not at the root of `settings.json`. `claude-code/settings-snippet.json` keeps the native-tool deny in `permissions.deny` and does not deny `git` or `batch_operations` globally.

## Codex vs ultra `apply_patch`

Codex `apply_patch` is `*** Begin Patch` / `*** Update File`. Ultra `apply_patch` is a **unified diff**. A file path applies one diff (`dry_run` + `expected_hash`). A directory path applies a multi-file unified diff as an in-process transaction (not crash-durable); `expected_hash` is single-file only. Do **not** feed Codex `*** Begin Patch` into ultra. There is no Begin-Patch adapter. A binary that returns `multi-file patch not supported` is an older build, not this main. Ultra is for Claude Code, OpenCode, and Claude Desktop.

## Measured cache baseline

[benchmark/](benchmark/README.md) provides `-suite cache`: real 3m/10m TTLs, immediate/aged reads and MCP restart behind the proxy, with retained JSON and demand-cache counters. See the [2026-09-21 results](benchmark/cache-baseline-20260921.md) (576 verified reads; no consistent latency win for 10m). This is a scripted synthetic benchmark; real-model evaluation remains open.

## Handoff parent → child

Subagents do **not** inherit the parent's skill, OCC hashes, or file body.

Pass: **roots**, **path**, **content_hash**, **backup_id**, **profile**.

The child calls `list_allowed_directories` first (structured: `profile`, `roots_mode`, `readonly`, `tool_count`), then **re-reads**. Do not recycle the parent's file bytes as `old_text`.
