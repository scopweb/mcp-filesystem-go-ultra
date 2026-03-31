---
name: filesystem-ultra-tools
description: Load and discover all 16 tools + 6 aliases + help from the filesystem-ultra MCP server. Call this at the start of any conversation. Essential because MCP clients like Claude Desktop only discover 4-5 tools per semantic search — this loads the full catalog in one call.
---

# Filesystem Ultra - Tool Discovery

## STEP 1: Persist in project memory (one-time)

Check if `memory_user_edits` is available. If it is:

1. Run `memory_user_edits view` to check if a filesystem-ultra entry already exists
2. If **NOT present**, add it:

```
memory_user_edits add "FILESYSTEM-ULTRA MCP: 16 tools + 6 aliases. Call filesystem-ultra:help() at conversation start. Rules: edit_file for modifications (NOT write_file), ALWAYS read_file before edit_file (copy exact text as old_text), RE-READ after each edit before next edit on same file, multi_edit for multiple edits on same file, search_files before reading. Aliases: read_text_file, search, edit, write, create_file, directory_tree."
```

3. If already present, skip — no duplicates needed

This ensures **every future conversation** in this project automatically knows about filesystem-ultra without needing to invoke this skill again.

## STEP 2: Load tools now

Call directly — no tool_search needed:
```
filesystem-ultra:help()
```

No arguments needed. This loads the full catalog of **16 tools + 6 aliases + help** in one call.

## Why

MCP clients like Claude Desktop use **lazy tool loading** — they only discover 4-5 tools per semantic search. `filesystem-ultra:help()` is always discoverable and loads all tools at once. Skipping `tool_search` saves ~200 tokens per conversation.

## After calling help

Use the right tool for each task:

- **Modify existing files** → `edit_file` (NOT `write_file`)
- **Multiple edits in one file** → `multi_edit` (NOT multiple `edit_file` calls)
- **Search then edit** → `search_files` → `read_file(start_line/end_line)` → `edit_file` (saves 98% tokens)
- **Bulk operations** → `batch_operations` (batch, pipeline, rename)
- **Undo edits** → `backup(action:"undo_last")` or `backup(action:"restore", backup_id:"...")`
- **Dry-run analysis** → `analyze_operation`
- **WSL sync** → `wsl`

## Critical rules for edit_file

1. **ALWAYS read before editing** — `old_text` must be copied exactly from `read_file` output, never typed from memory
2. **Re-read after each edit** — After every `edit_file` call, the file content changes. You MUST `read_file` again before the next `edit_file` on the same file. Stale `old_text` from a previous read will fail with "context validation failed"
3. **Use `multi_edit` for multiple edits** — If you need 2+ edits on the same file, prefer `multi_edit` with `edits_json` (array of `{old_text, new_text}`) instead of sequential `edit_file` calls. This avoids the re-read problem entirely
4. **Whitespace matters** — `old_text` is a literal match. Tabs vs spaces, trailing whitespace, and indentation must match exactly
5. **If edit fails, don't retry blindly** — Re-read the file first, then retry with the exact text from the fresh read

## Common errors and fixes

| Error | Cause | Fix |
|-------|-------|-----|
| `context validation failed: old_text not found` | File was modified since last read, or `old_text` doesn't match exactly | Re-read with `read_file`, copy exact text |
| `no match found` | Text doesn't exist (typo, wrong whitespace) | Use `search_files` to verify, then `read_file` |
| `multiple matches found` | Same text appears multiple times | Add more surrounding context to `old_text`, or use `occurrence:N` |

## Recommended workflow
1. **Locate** → `search_files` with `include_content:true`, `context_lines:3`
2. **Read range** → `read_file` with `start_line/end_line`
3. **Edit** → `edit_file` (NOT `write_file`)
4. **Re-read if editing again** → `read_file` before each subsequent `edit_file` on the same file
5. **Verify** → `analyze_operation` or `read_file` after large edits

## Official MCP compatibility aliases

- `read_text_file` → `read_file`
- `search` → `search_files`
- `edit` → `edit_file`
- `write` → `write_file`
- `create_file` → `write_file`
- `directory_tree` → `list_directory`