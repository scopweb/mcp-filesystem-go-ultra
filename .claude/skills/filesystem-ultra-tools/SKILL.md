---
name: filesystem-ultra-tools
description: Tool catalog for filesystem-ultra MCP server v4.7.1. 25 tools ultra / 16 strict. First call list_allowed_directories, then directory_tree or help(tool:X). Host filesystem, post-write verify, aliases disabled. Recommended flags: --profile=strict --compact-mode --roots-mode=union.
---

# Filesystem Ultra v4.7.1 — Tool Discovery

## Recommended server flags

`--profile=strict --compact-mode --roots-mode=union` (`--readonly` off). `--git-network` only if the agent must `git push`/`fetch`. `--git-remote-allow` is optional (empty = any remote). GitHub+GitLab: `--git-remote-allow=github.com,gitlab.com`. `--profile=ultra` (default) keeps all 25 tools including `analyze_code`.

## Bind each project to one filesystem tool family

- `filesystem-ultra` operates on the real host filesystem (`C:\...`, host-mounted `/mnt/...`). Runtime-native `create_file`, `str_replace`, `view` may target a different sandbox.
- First call `list_allowed_directories` (before any read). Then `directory_tree` or `help(tool:"X")` on demand. Do not dump the full catalog at startup.
- For host projects, use only filesystem-ultra tools. After every host create/edit, verify with `get_file_info` or `list_directory`; `read_file` when content matters.
- `File not found` for a known file is a filesystem-mismatch signal: stop, confirm with the host reader, audit recent writes. Never switch tool families silently.

## Subagents do not inherit this skill

Harness subagents start without this file. The portable contract is `server.WithInstructions()` (handshake). Copy `examples/harness/` rather than guessing flags.

**Handoff** parent → child: roots, path, `content_hash`, `backup_id`, profile. The child re-reads; it must not reuse the parent's file body. First call `list_allowed_directories` (structured: `profile`, `roots_mode`, `readonly`, `tool_count`). One role = one disk family — never mix native Read/Edit/Write/Glob with filesystem-ultra in the same subagent.

## Discovery order

1. `list_allowed_directories` — sandbox roots. Zero parameters.
2. `directory_tree` — explore. Defaults: `respect_ignore=true`, `max_depth=2`, `max_nodes=500`. Structured: `truncated` + `hidden_count`.
3. `help(tool:"X")` — schema + examples on demand. `help()` lists whatever is registered (16 or 25).

If a client asks for `read_multiple_files` / `read_text_file`, use `read_file` (`paths[]` / `mode` head|tail).

## Strict profile (16) — always available when `--profile=strict`

| Tool | When / notes |
|------|----------------|
| `list_allowed_directories` | First call |
| `directory_tree` | After roots. Gitignore ON |
| `list_directory` | Copy exact paths (case) before edits |
| `search_files` | Gitignore ON (`no_ignore=false`). `max_results` is the page size; continue with `offset`. Structured: `truncated` + `hidden_count` + `continuation`. Do not pair `count_only` with `detail=full` |
| `get_file_info` | Verify after a host mutation. Batch `paths[]` |
| `read_file` | Full / range / head / tail / base64 / `paths[]`. Logs: `mode:"tail"` `max_lines:40` |
| `write_file` | New files or whole-file rewrite. `mode:"append"` skips rewrite-guard |
| `edit_file` | Targeted edits. Override rewrite-guard with `allow_rewrite:true` (not `force`) |
| `multi_edit` | Several anchors in one file. Ambiguous `old_text` (>1 match) rejects the batch (file unchanged). The error lists each edit (index, `FAILED`/`AMBIGUOUS`); do not treat `Failed: .` as empty |
| `apply_patch` | One file: `path` is the dest. Several files: `path` is the directory root; in-process all-or-nothing (not crash-durable). Happy path: `dry_run` + `expected_hash` (single-file). Dest EOL wins. If `PATCH_FAILED`: `read_file` and regenerate; do not retry the same patch |
| `diff_files` | Preview before `apply_patch`, or two paths / `against:"backup"` |
| `create_directory` | `mkdir -p` |
| `move_file` | Move or rename |
| `delete_file` | Soft-delete default; `permanent:true` hard |
| `backup` | Undo: `undo_last` / `undo_chain` / `restore` (`backup_id`). Trash: `list_trash` / `restore_trash` (`sd_id`, not `backup_id`) / `purge_trash` |
| `help` | On demand, not at startup |

## Ultra only (absent in `--profile=strict`)

`copy_file`, `project_replace`, `batch_operations`, `analyze_operation`, `wsl`, `git`, `minify_js`, `analyze_code`, `server_info`.

- `git` — local only unless `--git-network` (then `push`/`fetch` and `openWorldHint=true`). `git(action:"remote")` is read-only and works without `--git-network`; it shows effective fetch/push URLs (credentials redacted). Push/fetch print that destination, not only `origin`. `--git-remote-allow` is optional (omit = any configured remote; GitHub+GitLab: `github.com,gitlab.com`). Matches the effective URL; `force:true` does not bypass. Path must be inside a repo (or `init`). Branch delete requires `delete:true`. Commit `risk` is informational and does not block.
- `minify_js` — exists in ultra; not in handshake instructions.
- `analyze_code` — ultra only. For editing: `apply_patch`/`edit_file`. For understanding code: `analyze_code`. Do not use git grep or bash. Actions: `symbols`, `lint`, `sec`, `impact`. Impact is a text search, not a callgraph.
- Dry-run impact preview → `analyze_operation` (ultra). In strict use `edit_file`/`apply_patch`/`multi_edit` with `dry_run:true`.

## Never bash for filesystem

Do not use bash `cat`, `head`, `tail`, `cut`, `sed -n`, `ls`, `dir`, `tree`, `grep`, `find`, `rg`, or `stat` on host project files.

| Need | Call |
|------|------|
| Last 40 log lines | `read_file(path, mode:"tail", max_lines:40)` |
| First N lines | `read_file(path, max_lines:N)` (mode omitted/`"all"`) or `mode:"head"` |
| Exact line range | `read_file(path, start_line, end_line)` |
| Line cut override | `max_line_length:N` or `0` to disable |

## search_files aliases

| Native | Alias | Purpose |
|--------|-------|---------|
| `file_types` | `include` | Glob (e.g. `*.go`) |
| `output_format` | `output` | `"text"` or `"json"`. Omit for auto (ripgrep-style `path:line:content` if ≤5 matches). Legacy `content`/`files_with_matches`/`count` are **not** implemented. |

`include_context:true` forces verbose layout. `output_format:"json"` uses ripgrep when `rg` is on PATH or embedded (`embed_rg`). When truncated, call the same search with `offset` from `continuation` — do not reformulate. There is no hidden 10/20 presentation cap.

## Key behaviors (strict-safe)

- **Modify existing files** → `edit_file` or `apply_patch` (one path). Whole-file rewrite → `write_file`.
- **Several edits same file** → `multi_edit` (each `old_text` unique in the original file). On atomic rollback, read the per-edit causes; the file was not written.
- **Dry-run** → `edit_file(dry_run:true)` / `multi_edit(dry_run:true)` / `apply_patch(dry_run:true)`.
- **OCC** → every successful read/edit returns `content_hash`; pass as `expected_hash` on the next mutation. `--auto-occ` `off`/`warn` (default)/`block` flags *external* changes only.
- **STALE_READ** (`edit_file` only): non-blocking. Consecutive edits on the same file do not need re-reads.
- **Structured results** → prefer `structuredContent`: `status` (`applied`/`simulated`/`empty`/`partial`), `truncated`, `hidden_count`, hashes. Empty search is `status:empty` (`isError=false`). Errors have `retryable` — do not auto-retry when false.
- **Line-based edits** → `edit_file` `mode:"delete_range"` / `"replace_range"` (1-based inclusive).
- **Undo** → `backup(action:"undo_last")` / `undo_chain` / `restore` with `backup_id`.
- **Soft-delete recovery** → `backup(action:"list_trash")`; restore with `backup(action:"restore_trash", sd_id:"...")` (not `backup_id`); purge with `purge_trash`.

## Ultra behaviors (only if those tools are registered)

- **Project-wide token rename** → `project_replace` (1 call). `create_backup:true` snapshots before writes.
- **Batch / pipeline / extract** → `batch_operations`. `extract` moves lines `[start_line,end_line]` from source to destination atomically.
- **Impact preview** → `analyze_operation`.

## Critical workflow rules

### Always copy paths from `list_directory` / `read_file` — never from memory

Windows is case-insensitive at the FS layer. Passing `estats.razor` for `Estats.razor` succeeds, then Razor/MSBuild fail later (`RZ10011: class estats`). Copy the path character-by-character from a prior listing.

### Never use `edit_file` for whole-file rewrites

`edit_file` replaces only the matched `old_text`. A small header + full-file `new_text` concatenates. The server blocks `new_text > 2× old_text` with leftover file content. Override is `allow_rewrite:true`, not `force`. Prefer `write_file`.

| Situation | Use |
|-----------|-----|
| Targeted small change | `edit_file` mode `replace` |
| Replace all occurrences | `edit_file` mode `search_replace` |
| Whole-file rewrite | `write_file` |
| Multiple targeted changes same file | `multi_edit` |
| Delete/replace known line range | `edit_file` `delete_range` / `replace_range` |
| Unified diff (one file or multi-file transaction) | `apply_patch` (regenerate on `PATCH_FAILED`) |
| Rename token project-wide | `project_replace` (ultra) |

## Disabled (not registered)

Aliases `read_text_file`, `search`, `edit`, `write`, `create_file`, `View`, `Edit`, `Write`, `Replace`, `LS`, `GlobTool`, `GrepTool`, and the `fs` super-tool. A runtime-native tool with one of those names is not a filesystem-ultra alias.

## project_replace (ultra)

`path`, `find`, `replace` required. `literal` default true. `file_types` e.g. `.php`. `exclude_paths` globs. `preview` / `create_backup` (default true) / `parallel` / `max_files` (default 1000). Compact default is counters only; `detail:"full"` lists each file.
