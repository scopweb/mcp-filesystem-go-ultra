# MCP Filesystem Ultra — Instructions for AI Agents (v4.1.2)

> This document is designed to be included in AI agent system prompts or context.
> For the complete tool reference, see [System_prompt.md](../System_prompt.md) or the main [README](../../README.md).

---

## Critical Rule: Ask Before Searching (Save 90% tokens)

**BEFORE executing `search_files`, ASK the user:**

```
BAD:  "Let me search where X is..." [automatic search]
GOOD: "Do you know what file/line X is in? If not, I can search for it."
```

**Only search if:**
- User says "I don't know where it is"
- User explicitly asks "find X" or "search for X"

---

## Use MCP Tools, Not Native File Tools

When you have access to **filesystem-ultra** tools, **always prefer them** over native file operations:

**Use these (MCP tools):**
```
read_file, write_file, edit_file, list_directory, search_files
```

**Avoid:** Native file reading tools, direct WSL commands for file I/O.

**Why?** MCP tools automatically convert paths between WSL (`/mnt/c/...`) and Windows (`C:\...`).

---

## The Golden Rule: Surgical Edits Save 98% Tokens

**Wasteful:**
```
read_file(entire_large_file) -> write_file(entire_large_file)
5000-line file = 250,000+ tokens wasted
```

**Efficient:**
```
search_files(file, pattern) -> read_file(file, start_line, end_line) -> edit_file(file, old, new)
5000-line file = ~2,500 tokens (98% savings)
```

---

## Complete Tool List (16 Tools in v4)

### Reading Files
| Tool | When to Use |
|------|-------------|
| `read_file` | Read full file, line range (`start_line`/`end_line`), head/tail (`max_lines` + `mode`), or base64 (`encoding:"base64"`) |

### Writing & Editing
| Tool | When to Use |
|------|-------------|
| `write_file` | Create or overwrite files (text or base64) |
| `edit_file` | Single find-and-replace with auto-backup. Modes: exact (default), `search_replace` (all occurrences), `regex` (capture groups), `occurrence:N` |
| `multi_edit` | Multiple edits on same file, atomically via `edits_json` |

### Search
| Tool | When to Use |
|------|-------------|
| `search_files` | Find files by pattern. Options: `include_content`, `include_context`, `case_sensitive`, `count_only`, `file_types` |

### File Operations
| Tool | When to Use |
|------|-------------|
| `copy_file` | Duplicate file/directory |
| `move_file` | Move or rename |
| `delete_file` | Soft-delete (default) or permanent (`permanent: true`) |
| `get_file_info` | File metadata (size, date, type) |
| `create_directory` | Create directory tree |
| `list_directory` | List contents with cache |

### Batch & Pipeline
| Tool | When to Use |
|------|-------------|
| `batch_operations` | Atomic batch ops (`request_json`), multi-step pipelines (`pipeline_json`), or batch rename (`rename_json`) |

### Backup & Recovery
| Tool | When to Use |
|------|-------------|
| `backup` | Actions: `list`, `info`, `compare`, `cleanup`, `restore`, `undo_last` |

### Analysis & Utility
| Tool | When to Use |
|------|-------------|
| `analyze_operation` | Dry-run preview: `file`, `edit`, `delete`, `write`, `optimize` |
| `server_info` | Actions: `stats`, `help`, `artifact` |
| `wsl` | WSL sync and status |

---

## The Efficient Workflow

For ANY file edit, follow this workflow:

### Step 1: LOCATE
```
search_files(path, "function_name")
-> Returns: "Found at lines 45-67"
```

### Step 2: READ (Only what you need)
```
read_file(path, start_line: 45, end_line: 67)
-> Returns: Only those 22 lines
```

### Step 3: EDIT (Surgically)
```
edit_file(path, old_text: "<exact text>", new_text: "<replacement>")
-> Returns: "OK: 1 changes [backup:abc123 | UNDO: ...]"
```

### Step 4: VERIFY (If needed)
```
read_file(path, start_line: 45, end_line: 67)
-> Confirm the edit looks correct
```

---

## File Size Decision Tree

```
Is file < 1000 lines?
+-- YES -> read_file() is OK
+-- NO  -> MUST use search_files + read_file(start_line, end_line) + edit_file
```

---

## Common Errors & Solutions

### "old_text not found"
**Cause:** Text doesn't exist exactly as specified
**Fix:**
1. Use `read_file(path, start_line, end_line)` to get exact text
2. Check for whitespace/indentation differences
3. Use `search_files(path, pattern, count_only: true)` to verify

### "multiple matches found"
**Cause:** Same text appears multiple times
**Fix:** Use `edit_file(path, old_text, new_text, occurrence: 1)` for first match, `-1` for last

### Path errors with /mnt/c/ or C:\
**Cause:** Path format mismatch
**Fix:** MCP tools auto-convert paths. Just use the path as-is.

---

## Quick Reference

| I want to... | Use this tool |
|--------------|---------------|
| Read a file | `read_file` |
| Read specific lines | `read_file` with `start_line`, `end_line` |
| Create a new file | `write_file` |
| Edit text in a file | `edit_file` |
| Make multiple edits (same file) | `multi_edit` |
| Find where code is | `search_files` |
| Count occurrences | `search_files` with `count_only: true` |
| Replace Nth match only | `edit_file` with `occurrence: N` |
| List directory | `list_directory` |
| Copy/Move files | `copy_file`, `move_file` |
| Delete safely | `delete_file` (soft-delete by default) |
| Multiple file operations | `batch_operations` |
| Undo last edit | `backup(action: "undo_last")` |
| Restore specific backup | `backup(action: "restore", backup_id: "...")` |

---

## Remember

1. **Never read entire large files** — use `read_file` with `start_line`/`end_line`
2. **Use `edit_file` not `write_file`** for changes
3. **Use `multi_edit`** for multiple changes in one file
4. **Use `search_files` first** to find exact locations
5. **Every edit response includes UNDO** — use it if something goes wrong

---

*Version: 4.1.2 | Last Updated: 2026-03-17*
