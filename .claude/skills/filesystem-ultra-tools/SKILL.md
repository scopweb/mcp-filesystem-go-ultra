---
name: filesystem-ultra-tools
description: Tool catalog for filesystem-ultra MCP server v4.5.29: 20 tools (17 core + git + minify_js + help). Host-filesystem binding, post-write verification, aliases disabled.
---

# Filesystem Ultra v4.5.29 — Tool Discovery

## Bind each project to one filesystem tool family

- `filesystem-ultra` operates on the real host filesystem visible to its MCP server (`C:\...`, host-mounted `/mnt/...`, etc.). Runtime-native `create_file`, `str_replace`, `view`, or similar tools may operate in a different sandbox.
- Before the first project read or write, select the family explicitly mapped to that project. For host projects reachable through filesystem-ultra, use only its read/write/edit/list/info/copy/delete tools; reserve native tools for explicit agent scratch work.
- After every host creation or edit, verify independently with `get_file_info` or `list_directory`; use `read_file` when content matters. A successful write response alone does not prove that a different tool family targeted the host.
- Treat `File not found` for a known file as a filesystem-mismatch signal: stop, confirm with the host reader, audit recent writes made through the failing family, and understand the mismatch before retrying. Never switch tools silently.

## The 20 tools (17 core + git + minify_js + help)

| Tool | Purpose |
|------|---------|
| `read_file` | Read files (single or batch via `paths`) |
| `write_file` | Write/create files (binary via base64) |
| `edit_file` | Replace exact text, regex, nth occurrence |
| `multi_edit` | Multiple edits in one file |
| `project_replace` | Project-wide find/replace in one call |
| `list_directory` | List directory contents |
| `search_files` | Search by pattern (regex or literal) |
| `get_file_info` | File info (single or batch) |
| `move_file` | Move/rename files |
| `copy_file` | Copy files |
| `delete_file` | Delete (soft by default, permanent option) |
| `create_directory` | Create directories |
| `batch_operations` | Atomic ops, pipelines, batch rename |
| `backup` | Backup/restore/undo/list/compare |
| `analyze_operation` | Dry-run impact analysis |
| `wsl` | WSL/Windows sync and path conversion |
| `server_info` | Stats, help, artifact capture |
| `git` | Version control (status, diff, log, **show**, add, commit, restore, branch, init). `paths` is a **native array**; `output` enum (`stat`/`name-only`/`full`); 4-layer guardrail downgrades big full diffs to stat with a top-of-output banner; `rev` replaces `commit_range`/`source`. Errors include a `usage:` line; `help(tool:"git")` returns schema + 8 curated examples. |
| `minify_js` | Pure-Go JS minification, no Node (v4.5.7+) |
| `help` | Discovery — call first to see all 20 tools |

## search_files ripgrep-compatible params

`search_files` accepts both native names and ripgrep-compatible aliases:

| Native | Alias | Purpose |
|--------|-------|---------|
| `file_types` | `include` | Glob pattern filter (e.g., `*.go`, `**/*.ts`) |
| `output_format` | `output` | `content`, `files_with_matches`, `count` |

## Key behaviors

- **Modify existing files** → `edit_file`
- **Multiple edits same file** → `multi_edit`
- **Project-wide find/replace** → `project_replace` (1 call instead of N)
- **Batch ops** → `batch_operations` (atomic, with rollback)
- **Undo** → `backup(action:"undo_last")` or `backup(action:"restore", backup_id:"...")`
- **Git operations** → `git` tool (status, diff, log, add, commit, restore, branch, init). **The path passed must be inside a git repository** (or use `init` to create one). Calling `git` on a non-repo path is the #1 source of errors (analysis of 18 calls showed 5 of 7 errors were "not a git repository" — instant failures before any git command ran). Since v4.5.23 `restore` supports real dry-run and hardened path handling (`--` separator); no `force` needed.
- **STALE_READ warning** (`edit_file` only): non-blocking notice if the file wasn't read in the last 10 min of this session. The engine records reads after each successful edit, so consecutive edits on the same file don't need re-reads. Hard external-change protection = `expected_hash` or `--auto-occ=block`.
- **Dry-run** → `analyze_operation` or `edit_file(dry_run:true)` / `multi_edit(dry_run:true)` / `project_replace(preview:true)`
- **Fast search** → `search_files` with `output_format:"json"` uses ripgrep when available
- **Chain edits without re-reading** → every successful edit returns `content_hash`; pass it as `expected_hash` on the next edit. External-change detection also via `--auto-occ` flag (`off`/`warn` default/`block`) — only flags changes NOT made by this session.
- **Line-based edits** → `edit_file` `mode:"delete_range"` (remove lines start..end) and `mode:"replace_range"` (replace lines with `new_text`) — 1-based inclusive, no fragile `old_text` match.
- **Move lines between files atomically** → `batch_operations` op type `extract` (`source`, `destination`, `start_line`, `end_line`, `append`) — bytes written = bytes removed, both atomic, revert together under `atomic:true`.

## Critical workflow rules (anti-bug)

These two failure modes are silent at the tool level — the tool returns OK, the file is "valid", but the work is wrong. The model MUST avoid them via workflow discipline, not via the tool (the tool can't always detect intent).

### ⚠️ Always copy paths from `list_directory` / `read_file` — never from memory (case-mismatch bug, 2026-06-11)

The path you pass to `edit_file` / `multi_edit` / `write_file` / `move_file` / `delete_file` MUST be copied character-by-character from the output of a prior `list_directory` or `read_file` call. **Do not retype it from memory or from the conversation history.**

**Why:** Windows resolves paths case-insensitively, so writing `estats.razor` when the file is `Estats.razor` succeeds at the filesystem level — but downstream tools that register classes/modules by the path (Razor compiler, webpack, MSBuild, etc.) use the wrong capitalization and fail 3 layers down with cryptic errors like `RZ10011: class estats` or `module not found`.

**Symptom if you fall into this:** edit appears successful in the tool response, but compilation/build/import fails later with a "class/module not found" error that mentions a name you didn't expect. Verify the case of every path in your most recent edit.

### ⚠️ Never use `edit_file` for whole-file rewrites (bug 2026-06-11)

If you intend to rewrite most or all of a file's content, use `write_file` directly. `edit_file` only swaps the matched `old_text` block — everything else in the file remains. Passing a full file as `new_text` with a small `old_text` (e.g., a header) produces a **concatenated/doubled file** silently.

**Heuristic:**
- `len(new_text) > 2 × len(old_text)` AND file has content beyond the match → probably you want `write_file`
- The server now BLOCKS this pattern by default (use `force:true` to override)

**Tool guidance:**
| Situation | Use |
|-----------|-----|
| Targeted small change | `edit_file` mode `replace` |
| Replace all occurrences of a pattern | `edit_file` mode `search_replace` |
| Whole-file rewrite | `write_file` |
| Multiple targeted changes same file | `multi_edit` with several anchors |
| Delete/replace known line range | `edit_file` mode `delete_range` / `replace_range` |
| Rename token project-wide | `project_replace` |

## project_replace — Project-wide find/replace

Replaces N calls to `multi_edit` with 1 call. Scans directory tree, matches pattern, replaces all occurrences.

**Parameters:**
- `path` — root directory (required)
- `find` — text or regex (required)
- `replace` — replacement text (required)
- `literal` — if false, find is regex (default: true)
- `case_sensitive` — (default: true)
- `file_types` — ".php,.html" (comma-separated)
- `exclude_paths` — ["jotajotape/**"] (globs to skip)
- `preview` — diff without writing (default: false)
- `create_backup` — single consolidated backup (default: true)
- `parallel` — process files concurrently (default: true)
- `max_files` — safety cap (default: 1000)

**Example:**
```json
{
  "path": "C:\\project\\public_html",
  "find": "utf8_encode(",
  "replace": "utf8e(",
  "file_types": ".php",
  "exclude_paths": ["jotajotape/**"],
  "preview": false
}
```

**Response:** `files_changed`, `total_replacements`, `backup_id`, `per_file` array

## Disabled (v4.4.0 cleanup)

- 13 aliases (`read_text_file`, `search`, `edit`, `write`, `create_file`, `directory_tree`, `View`, `Edit`, `Write`, `Replace`, `LS`, `GlobTool`, `GrepTool`)
- `fs` super-tool

These were disabled to reduce discovery noise and token overhead. The 17 core tools are self-sufficient.

## Ripgrep backend

When `rg` (ripgrep) is available on PATH or embedded, `search_files` with `output_format:"json"` uses ripgrep for 10-100x faster search.

**Detection priority:**
1. `rg` in PATH
2. Embedded binary (build with `embed_rg` tag)
3. Fallback to Go-native regex

**Log output:**
```
INFO Ripgrep detected for accelerated search version=14.x.x
INFO Ripgrep not found - using Go-native search
```