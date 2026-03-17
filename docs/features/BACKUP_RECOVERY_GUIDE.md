# Backup & Recovery Guide

**Version:** 4.1.2
**Updated:** 2026-03-17

---

## Overview

MCP Filesystem Ultra automatically creates backups before every destructive operation (edit, delete, batch). You can recover any file at any time using the `backup` tool.

### Key Benefits

- **Automatic backups** — created before every `edit_file` and `multi_edit` call
- **Risk validation** — warns before large or risky changes
- **Quick undo** — `backup(action:"undo_last")` restores the most recent backup
- **Full audit trail** — every backup has metadata (timestamp, operation, files)

---

## Automatic Backups

### When are backups created?

Backups are created automatically before:

1. **File edits** — `edit_file`, `multi_edit`
2. **File deletions** — `delete_file`
3. **Batch operations** — `batch_operations` with `create_backup: true`

### Backup location

By default, backups are stored in the system temp directory:
```
%TEMP%\mcp-batch-backups\
```

Each backup has its own directory:
```
<backup-id>\
  metadata.json       # Backup metadata
  files\              # Backed-up files
    your_file.go
```

### Configuration

Customize backup behavior in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "filesystem-ultra": {
      "command": "C:\\path\\to\\filesystem-ultra-v4.exe",
      "args": [
        "--backup-dir=C:\\backups\\mcp",
        "--backup-max-age=14",
        "--backup-max-count=200",
        "C:\\your\\project",
        "C:\\backups\\mcp"
      ]
    }
  }
}
```

**Important:** The backup directory MUST be in the allowed paths (positional args).

---

## Risk Validation

Before editing a file, the system analyzes the impact:

| Level | Conditions | Behavior |
|-------|-----------|----------|
| **LOW** | <30% change, <50 occurrences | Proceeds normally |
| **MEDIUM** | 30-50% change, 50-100 occurrences | Shows warning |
| **HIGH** | 50-90% change, >100 occurrences | Proceeds with warning + VERIFY instruction |
| **CRITICAL** | >90% change | Requires `force: true` |

### Recommended workflow for risky edits

1. **Preview first** with `analyze_operation`:
   ```
   analyze_operation(path: "main.go", operation: "edit")
   ```

2. **Review the impact analysis**

3. **Proceed with force** if safe:
   ```
   edit_file(path: "main.go", old_text: "func", new_text: "function", force: true)
   ```

---

## Backup Management

### Quick undo (most recent edit)

```
backup(action: "undo_last")
```

Preview before undoing:
```
backup(action: "undo_last", preview: true)
```

### List available backups

```
backup(action: "list")
backup(action: "list", filter_path: "main.go")
backup(action: "list", filter_operation: "edit", limit: 10)
```

### Get backup details

```
backup(action: "info", backup_id: "20260317-153045-abc123")
```

### Compare before restoring

```
backup(action: "compare", backup_id: "20260317-153045-abc123", file_path: "main.go")
```

### Restore a specific backup

```
backup(action: "restore", backup_id: "20260317-153045-abc123")
backup(action: "restore", backup_id: "20260317-153045-abc123", file_path: "main.go")
```

A safety backup of the current state is created before restoring.

### Cleanup old backups

Preview what would be deleted:
```
backup(action: "cleanup", older_than_days: 7, dry_run: true)
```

Execute cleanup:
```
backup(action: "cleanup", older_than_days: 7)
```

---

## Common Use Cases

### Case 1: Safe Mass Edit

```
# 1. Count occurrences first
search_files(path: "main.go", pattern: "oldFunc", count_only: true)

# 2. Preview impact
analyze_operation(path: "main.go", operation: "edit")

# 3. Edit (auto-creates backup)
edit_file(path: "main.go", mode: "search_replace", pattern: "oldFunc", replacement: "newFunc")

# 4. If something went wrong
backup(action: "undo_last")
```

### Case 2: Emergency Recovery

```
# 1. Find backups for the broken file
backup(action: "list", filter_path: "important.go")

# 2. Compare to see the diff
backup(action: "compare", backup_id: "...", file_path: "important.go")

# 3. Restore
backup(action: "restore", backup_id: "...", file_path: "important.go")
```

### Case 3: Batch Operations with Safety

```
batch_operations(request_json: '{
  "operations": [
    {"type": "edit", "path": "f1.go", "old_text": "old", "new_text": "new"},
    {"type": "edit", "path": "f2.go", "old_text": "old", "new_text": "new"}
  ],
  "atomic": true,
  "create_backup": true
}')
```

If any operation fails, all changes are automatically rolled back.

---

## Advanced Configuration

### Custom risk thresholds

```json
{
  "args": [
    "--risk-threshold-medium=40.0",
    "--risk-threshold-high=60.0"
  ]
}
```

### Backup retention

```json
{
  "args": [
    "--backup-max-age=14",
    "--backup-max-count=200"
  ]
}
```

Defaults: max age 72h, max count 50.

---

## FAQ

**Are backups created automatically?**
Yes, before every `edit_file` and `multi_edit` call.

**Can I access backups manually?**
Yes, they are regular files in the backup directory.

**What if I run out of disk space?**
Use `backup(action:"cleanup", older_than_days: 3)` to free space.

**Can I disable backups?**
No, but you can minimize retention with `--backup-max-age=1 --backup-max-count=5`.

---

*Version: 4.1.2 | Updated: 2026-03-17*
