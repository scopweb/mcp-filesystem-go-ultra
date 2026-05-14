---
name: filesystem-ultra-tools
description: Tool catalog for filesystem-ultra MCP server v4.4.0: 16 core tools + help. Aliases and fs super-tool disabled.
---

# Filesystem Ultra v4.4.0 — Tool Discovery

## The 16 core tools

| Tool | Purpose |
|------|---------|
| `read_file` | Read files (single or batch via `paths`) |
| `write_file` | Write/create files (binary via base64) |
| `edit_file` | Replace exact text, regex, nth occurrence |
| `multi_edit` | Multiple edits in one file |
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

## search_files ripgrep-compatible params

`search_files` accepts both native names and ripgrep-compatible aliases:

| Native | Alias | Purpose |
|--------|-------|---------|
| `file_types` | `include` | Glob pattern filter (e.g., `*.go`, `**/*.ts`) |
| `output_format` | `output` | `content`, `files_with_matches`, `count` |

## Key behaviors

- **Modify existing files** → `edit_file`
- **Multiple edits same file** → `multi_edit`
- **Batch ops** → `batch_operations` (atomic, with rollback)
- **Undo** → `backup(action:"undo_last")` or `backup(action:"restore", backup_id:"...")`
- **Dry-run** → `analyze_operation` or `edit_file(dry_run:true)` / `multi_edit(dry_run:true)`
- **Fast search** → `search_files` with `output_format:"json"` uses ripgrep when available

## Disabled (v4.4.0 cleanup)

- 13 aliases (`read_text_file`, `search`, `edit`, `write`, `create_file`, `directory_tree`, `View`, `Edit`, `Write`, `Replace`, `LS`, `GlobTool`, `GrepTool`)
- `fs` super-tool

These were disabled to reduce discovery noise and token overhead. The 16 core tools are self-sufficient.

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