# Safe Editing Protocol

**For: Claude Code, Claude Desktop, and developers**
**Purpose: Zero risk of file destruction**
**Version: 4.1.2**

---

## Quick Start Checklist

Use this for ANY file edit operation:

### STEP 1: Read & Verify
- [ ] Read the file with `read_file(path, start_line, end_line)` to see exact content
- [ ] Copy the EXACT text you want to change (including spaces/tabs)
- [ ] Note the line numbers

### STEP 2: Confirm Pattern
- [ ] Run `search_files(path, pattern, count_only: true)` to count occurrences
- [ ] Expected count matches actual count

### STEP 3: Choose Edit Method
- [ ] Single change: `edit_file(path, old_text, new_text)`
- [ ] Multiple changes same file: `multi_edit(path, edits_json)`
- [ ] Multiple files atomically: `batch_operations(request_json)`
- [ ] Global find-replace: `edit_file(path, mode:"search_replace", pattern, replacement)`

### STEP 4: Execute Edit
```
edit_file(
  path: "file.go",
  old_text: "<copied from read_file>",
  new_text: "<replacement text>"
)
```

### STEP 5: Verify Result
- [ ] File size didn't change drastically
- [ ] New text appears: `read_file(path, start_line, end_line)`
- [ ] Old text is gone: `search_files(path, old_pattern, count_only: true)` returns 0

### If something went wrong:
```
backup(action: "undo_last")
```

---

## Critical Rules

### Rule 1: NEVER use fuzzy matching for critical edits
- **BAD**: Copy text from memory and hope it matches
- **GOOD**: Use `read_file(path, start_line, end_line)` to get exact text

### Rule 2: NEVER skip line ending verification
- **BAD**: Assume file uses Unix line endings
- **GOOD**: Check `read_file` output for `\r\n` or `\n`

### Rule 3: NEVER edit without backup
All `edit_file` and `multi_edit` calls create automatic backups. Every response includes:
```
UNDO: backup(action:"restore", backup_id:"...")
```

### Rule 4: Use multi_edit for multiple changes in one file
- **BAD**: Call `edit_file` 5 times on the same file
- **GOOD**: Use `multi_edit(path, edits_json)` — one call, atomic

### Rule 5: NEVER skip verification after edit
- After each edit, verify with `read_file` or `search_files(count_only: true)`

---

## Decision Tree

```
START: "I need to edit a file"
  |
  +-- Is it a CRITICAL file? (config, production code)
  |   +-- YES -> Use batch_operations with atomic=true, create_backup=true
  |   +-- NO --+
  |
  +-- Are you making MULTIPLE changes to the SAME file?
  |   +-- YES -> Use multi_edit
  |   +-- NO --+
  |
  +-- Are you making changes across MULTIPLE files?
  |   +-- YES -> Use batch_operations
  |   +-- NO --+
  |
  +-- DECISION: Use edit_file
```

---

## Tool Reference (v4)

### 1. read_file — Get Exact Content
```
read_file(path: "file.go", start_line: 10, end_line: 20)
```
Returns exact text with line numbers. Copy this for `old_text`.

### 2. search_files — Find & Count
```
search_files(path: "file.go", pattern: "text_to_find", count_only: true)
```
Confirms text exists and how many times.

### 3. edit_file — Single Edit
```
edit_file(path: "file.go", old_text: "<from read_file>", new_text: "<replacement>")
```
Returns backup_id for undo. Use `force: true` if risk warning appears.

### 4. multi_edit — Multiple Edits, Same File
```
multi_edit(path: "file.go", edits_json: '[{"old_text":"a","new_text":"b"},{"old_text":"c","new_text":"d"}]')
```
All edits applied atomically.

### 5. batch_operations — Multiple Files
```
batch_operations(request_json: '{"operations":[{"type":"edit","path":"f1.go","old_text":"a","new_text":"b"},{"type":"edit","path":"f2.go","old_text":"c","new_text":"d"}],"atomic":true,"create_backup":true}')
```

### 6. backup — Undo & Recovery
```
backup(action: "undo_last")                           # Undo most recent edit
backup(action: "undo_last", preview: true)            # Preview what would be restored
backup(action: "restore", backup_id: "...")            # Restore specific backup
backup(action: "list", filter_path: "filename")        # Find backups for a file
backup(action: "compare", backup_id: "...", file_path: "...")  # Diff before restoring
```

---

## When to Use Which Tool

| Situation | Tool | Reason |
|-----------|------|--------|
| First time editing a file | `read_file` | Get exact content |
| Verify pattern exists | `search_files` (count_only) | Confirm before editing |
| Single edit, any file | `edit_file` | Simple, fast, auto-backup |
| Multiple edits, same file | `multi_edit` | Atomic, one call |
| Multiple files | `batch_operations` | Atomic with rollback |
| Find code location | `search_files` | Returns line numbers |
| After any edit | `read_file` or `search_files` | Verify success |
| Something went wrong | `backup(action:"undo_last")` | Instant recovery |

---

## Common Mistakes

### Mistake 1: Different Line Endings
```
BAD:  old_text = "line1\nline2"     # Unix
FILE: "line1\r\nline2"              # Windows
FIX:  Copy exactly from read_file output
```

### Mistake 2: Extra Whitespace
```
BAD:  old_text = "public List Orders"
FILE: "    public List Orders"        # 4 spaces at start
FIX:  Copy from read_file including indentation
```

### Mistake 3: Editing Without Verification
```
BAD:  Edit -> move on immediately
GOOD: Edit -> search_files(count_only:true) old text = 0 -> read_file to verify
```

---

## Emergency Procedures

### File got corrupted
```
# 1. Stop immediately — don't make more edits

# 2. Quick undo
backup(action: "undo_last")

# 3. Or find specific backup
backup(action: "list", filter_path: "broken-file.go")

# 4. Compare before restoring
backup(action: "compare", backup_id: "...", file_path: "broken-file.go")

# 5. Restore
backup(action: "restore", backup_id: "...")

# 6. Verify restoration
read_file(path: "broken-file.go", start_line: 1, end_line: 20)
```

### Golden Rule
**If edits make things WORSE, STOP editing and RESTORE from backup.**
Repeated edits on a broken file make recovery harder.

---

*Updated: 2026-03-17 | Version: 4.1.2*
