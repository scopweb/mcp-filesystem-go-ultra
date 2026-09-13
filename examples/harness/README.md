# Harness pack — one folder, one MCP, one disk family

Copy a subdirectory (`claude-code/`, `opencode/`, or `codex/`) into the project. Do not guess flags.

## Server (every harness)

One MCP instance per workspace:

```
--profile=strict --roots-mode=union --compact-mode
```

| Flag | Why |
|------|-----|
| `--profile=strict` | 16-tool agent core (`backup` included). Default `ultra` is 25 tools. |
| `--roots-mode=union` | Client Roots **add** to the CLI allowlist. Default `replace` wipes CLI paths (OpenCode does this). |
| `--compact-mode` | Short responses (hashes, UNDO ids). |

`--readonly` off unless the role is read-only. `--git-network` only if the agent must `git push`/`fetch`.

OpenCode: set MCP `timeout` **≥ 30000**. Default 5s is too short for `tools/list`.

Linux binary/root placeholders: `/path/to/filesystem-ultra` and `/path/to/workspace` (see `claude-code/mcp.json.example` / `opencode/opencode.jsonc.example` — swap the Windows paths).

## One role = one disk family

Harnesses ship native `Read` / `Edit` / `Write` / `Grep` / `Glob` (and Codex `apply_patch`) on **another sandbox**. Mixing them with filesystem-ultra in the same subagent breaks OCC and backups.

| Role | Disk family |
|------|-------------|
| Host project (`C:\`, `/mnt/…`) | filesystem-ultra only |
| Agent scratch outside the repo | native tools only |

Never mix native Read/Edit with ultra `read_file`/`edit_file` in one subagent.

## Codex vs ultra `apply_patch`

Codex `apply_patch` is `*** Begin Patch` / `*** Update File`. Ultra `apply_patch` is a **unified diff**, one file per call (`dry_run` + `expected_hash`). Do **not** feed Codex `*** Begin Patch` into ultra. Ultra is for Claude Code, OpenCode, and Claude Desktop. This pack does not ship a Begin-Patch adapter.

## Handoff parent → child

Subagents do **not** inherit the parent's skill, OCC hashes, or file body.

Pass: **roots**, **path**, **content_hash**, **backup_id**, **profile**.

The child calls `list_allowed_directories` first (structured: `profile`, `roots_mode`, `readonly`, `tool_count`), then **re-reads**. Do not recycle the parent's file bytes as `old_text`.
