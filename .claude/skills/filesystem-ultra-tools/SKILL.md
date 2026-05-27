---
name: filesystem-ultra-tools
description: Tool catalog for filesystem-ultra MCP server v4.5.2: 17 core tools + git + help. Aliases and fs super-tool disabled.
---

# Filesystem Ultra v4.5.2 — Tool Discovery

## The 18 tools (17 core + git + help)

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
| `git` | Version control (status, diff, log, add, commit, restore, branch, init) |

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
- **Git operations** → `git` tool (status, diff, log, add, commit, restore, branch, init)
- **Dry-run** → `analyze_operation` or `edit_file(dry_run:true)` / `multi_edit(dry_run:true)` / `project_replace(preview:true)`
- **Fast search** → `search_files` with `output_format:"json"` uses ripgrep when available

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