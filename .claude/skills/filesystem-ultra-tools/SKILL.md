---
name: filesystem-ultra-tools
description: Load and discover all 16 tools + 6 aliases + fs super-tool from the filesystem-ultra MCP server v4.2.1. Call this at the start of any conversation. Essential because MCP clients like Claude Desktop only discover 4-5 tools per semantic search — this loads the full catalog in one call.
---

# Filesystem Ultra v4.2.1 — Tool Discovery

## Load tools

Call directly — no tool_search needed:
```
filesystem-ultra:help()
```

No arguments needed. This loads the full catalog of **16 tools + 6 aliases + fs super-tool + help** in one call.

## Why

MCP clients like Claude Desktop use **lazy tool loading** — they only discover 4-5 tools per semantic search. `filesystem-ultra:help()` is always discoverable and loads all tools at once. Skipping `tool_search` saves ~200 tokens per conversation.

## After calling help

Use the right tool for each task:

- **Modify existing files** → `edit` alias or `edit_file` (NOT `write_file`)
- **Multiple edits in one file** → `multi_edit` (NOT multiple `edit_file` calls)
- **Search then edit** → `search` → `read_text_file(start_line/end_line)` → `edit` (saves 98% tokens)
- **Bulk operations** → `batch_operations` (batch, pipeline, rename)
- **Undo edits** → `backup(action:"undo_last")` or `backup(action:"restore", backup_id:"...")`
- **Dry-run preview** → `analyze_operation` or `edit_file(dry_run:true)` / `multi_edit(dry_run:true)`
- **WSL sync** → `wsl`
- **All ops via single tool** → `fs(action:"...", ...)` — for clients with limited tool loading

## Recommended workflow
1. **Locate** → `search` with `include_content:true`, `context_lines:3`
2. **Read range** → `read_text_file` with `start_line/end_line`
3. **Edit** → `edit` (NOT `write`)
4. **Verify** → `analyze_operation` or `read_text_file` after large edits

## Aliases (all have COMPLETE schemas matching their originals)

| Alias | → Original | Key params |
|-------|-----------|------------|
| `read_text_file` | `read_file` | path, start_line, end_line, max_lines, mode, encoding |
| `search` | `search_files` | path, pattern, include_content, file_types, case_sensitive, whole_word, include_context, context_lines, count_only, return_lines |
| `edit` | `edit_file` | path, old_text, new_text, mode, pattern, replacement, patterns_json, occurrence, force, case_sensitive, create_backup, dry_run, whole_word |
| `write` | `write_file` | path, content, content_base64, encoding |
| `create_file` | `write_file` | path, content, content_base64, encoding |
| `directory_tree` | `list_directory` | path |

## fs super-tool

For clients that only load 4-5 tools (e.g., claude.ai), the `fs` tool provides access to ALL 16 operations via a single `action` parameter:

```
fs(action:"read_file", path:"/some/file", start_line:1, end_line:50)
fs(action:"edit_file", path:"/some/file", old_text:"x", new_text:"y")
fs(action:"search_files", path:"/dir", pattern:"TODO", include_content:true)
fs(action:"backup", backup_action:"undo_last")
```

Valid actions: read_file, write_file, edit_file, multi_edit, list_directory, search_files, analyze_operation, create_directory, delete_file, move_file, copy_file, get_file_info, batch_operations, backup, wsl, server_info

For backup/wsl/server_info, use `backup_action`, `wsl_action`, `server_action` params to avoid collision with the top-level `action` param.

## Security notes (v4.2.1)

- `--allowed-paths` roots are protected: `delete_file`, `soft_delete`, and `move_file` reject the root directory itself
- All delete operations default to soft-delete (trash); use `permanent:true` for hard delete
- `dry_run:true` on `edit_file` and `multi_edit` now correctly previews without writing to disk
