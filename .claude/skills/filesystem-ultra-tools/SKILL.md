---
name: filesystem-ultra-tools
description: Load all 16 tools + 6 aliases + fs super-tool + help from filesystem-ultra MCP server v4.2.1. Call at start of any conversation. Essential because MCP clients like Claude Desktop only discover 4-5 tools per semantic search — this provides the full catalog in one call.
---

# Filesystem Ultra v4.2.1 — Tool Discovery

## Load full catalog

Call directly — no tool_search needed:
```
filesystem-ultra:help()
```

Returns the complete tool list. Skipping it means you only see ~5 tools and miss `edit_file`, `multi_edit`, `backup`, etc.

## The 16 tools

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

## 6 aliases (complete schemas)

| Alias | → Original | Key params |
|-------|-----------|------------|
| `read_text_file` | `read_file` | path, start_line, end_line, max_lines, mode, encoding |
| `search` | `search_files` | path, pattern, include_content, file_types, case_sensitive, whole_word, include_context, context_lines, count_only, return_lines |
| `edit` | `edit_file` | path, old_text, new_text, old_str, new_str, mode, pattern, replacement, patterns_json, occurrence, force, case_sensitive, create_backup, dry_run, whole_word |
| `write` | `write_file` | path, content, content_base64, encoding |
| `create_file` | `write_file` | path, content, content_base64, encoding |
| `directory_tree` | `list_directory` | path |

## fs super-tool

Single entry point for ALL 16 operations via `action` param — for clients with limited tool loading:

```
fs(action:"read_file", path:"/some/file", start_line:1, end_line:50)
fs(action:"edit_file", path:"/some/file", old_text:"x", new_text:"y")
fs(action:"search_files", path:"/dir", pattern:"TODO", include_content:true)
fs(action:"backup", backup_action:"undo_last")
```

Valid actions: read_file, write_file, edit_file, multi_edit, list_directory, search_files, analyze_operation, create_directory, delete_file, move_file, copy_file, get_file_info, batch_operations, backup, wsl, server_info

For backup/wsl/server_info, use `backup_action`, `wsl_action`, `server_action` params (they clash with top-level `action`).

## Never use bash alternatives

- **Search** → `search_files` (NOT `grep`, `find`, `rg`)
- **Read** → `read_file` (NOT `cat`, `type`, `bat`)
- **List dir** → `list_directory` (NOT `ls`, `dir`)

Bash commands bypass the MCP cache, skip audit logging, and return untyped output.

## Recommended workflow

1. **Locate** → `search` with `include_content:true`
2. **Read range** → `read_text_file` with `start_line/end_line`
3. **Edit** → `edit` (NOT `write`)
4. **Verify** → `read_text_file` or `analyze_operation`

## Key behaviors

- **Modify existing files** → `edit_file` / `edit` (NOT `write_file` / `write` — that truncates)
- **Multiple edits same file** → `multi_edit` (NOT multiple `edit_file` calls)
- **Batch ops** → `batch_operations` (atomic, with rollback)
- **Undo** → `backup(action:"undo_last")` or `backup(action:"restore", backup_id:"...")`
- **Dry-run** → `analyze_operation` or `edit_file(dry_run:true)` / `multi_edit(dry_run:true)`
