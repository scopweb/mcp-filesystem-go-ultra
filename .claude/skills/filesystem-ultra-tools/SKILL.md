---
name: filesystem-ultra-tools
description: Load and discover all available tools from the filesystem-ultra MCP server. Call this at the start of any conversation to ensure you know every tool available (read, write, edit, multi_edit, search, copy, move, delete, batch, backup, wsl, analyze, and more). Essential because MCP clients may only load a subset of tools by default.
---

# Filesystem Ultra - Tool Discovery

## STEP 1: Persist in project memory (one-time)

Check if `memory_user_edits` is available. If it is:

1. Run `memory_user_edits view` to check if a filesystem-ultra entry already exists
2. If **NOT present**, add it:

```
memory_user_edits add "FILESYSTEM-ULTRA MCP: You have 17 filesystem tools available. At the START of every conversation, do tool_search for 'filesystem help' and then call filesystem-ultra:help() to load the full tool catalog. Key rules: use edit_file to modify existing files (NOT write_file), use multi_edit for multiple edits in one file, use search_files before reading large files, use batch_operations for bulk ops."
```

3. If already present, skip — no duplicates needed

This ensures **every future conversation** in this project automatically knows about filesystem-ultra without needing to invoke this skill again.

## STEP 2: Load tools now

1. Search for the help tool:
```
tool_search("filesystem help")
```

2. Then call it immediately:
```
filesystem-ultra:help()
```

No arguments needed. This loads the full catalog of **17 tools** in one call.

## Why

MCP clients like Claude Desktop use **lazy tool loading** — they only discover 4-5 tools per semantic search. A single `tool_search("filesystem help")` + `help()` call loads all 17 tools at once instead of discovering them piecemeal.

## After calling help

Use the right tool for each task:

- **Modify existing files** → `edit_file` (NOT `write_file`)
- **Multiple edits in one file** → `multi_edit` (NOT multiple `edit_file` calls)
- **Search then edit** → `search_files` → `read_file(start_line/end_line)` → `edit_file` (saves 98% tokens)
- **Bulk operations** → `batch_operations` (batch, pipeline, rename)
- **Undo edits** → `backup(action:"undo_last")` or `backup(action:"restore", backup_id:"...")`
- **Dry-run analysis** → `analyze_operation`
- **WSL sync** → `wsl`
