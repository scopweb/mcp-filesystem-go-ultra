# Batch Operations Guide

**Version:** 4.1.2

## Overview

The `batch_operations` tool executes multiple file operations **atomically** with automatic rollback on failure. It supports three modes:

1. **Batch operations** via `request_json` — atomic multi-file operations
2. **Pipelines** via `pipeline_json` — multi-step transformations with conditions, templates, and parallel execution
3. **Batch rename** via `rename_json` — rename multiple files at once

## Supported Operation Types

### 1. `write` — Write file
```json
{"type": "write", "path": "file.txt", "content": "file content"}
```

### 2. `edit` — Edit file content
```json
{"type": "edit", "path": "file.txt", "old_text": "text to replace", "new_text": "new text"}
```

### 3. `search_and_replace` — Replace all occurrences
```json
{"type": "search_and_replace", "path": "file.txt", "old_text": "pattern", "new_text": "replacement"}
```

### 4. `move` — Move/rename file
```json
{"type": "move", "source": "old.txt", "destination": "new.txt"}
```

### 5. `copy` — Copy file
```json
{"type": "copy", "source": "source.txt", "destination": "dest.txt"}
```

### 6. `delete` — Delete file
```json
{"type": "delete", "path": "file.txt"}
```

### 7. `create_dir` — Create directory
```json
{"type": "create_dir", "path": "new/directory"}
```

## Usage Examples

### Example 1: Refactoring Multiple Files

```json
{
  "operations": [
    {"type": "edit", "path": "src/utils.js", "old_text": "function oldName(", "new_text": "function newName("},
    {"type": "edit", "path": "src/index.js", "old_text": "oldName()", "new_text": "newName()"},
    {"type": "edit", "path": "tests/utils.test.js", "old_text": "oldName()", "new_text": "newName()"}
  ],
  "atomic": true,
  "create_backup": true
}
```

### Example 2: Reorganize Project Structure

```json
{
  "operations": [
    {"type": "create_dir", "path": "src/components"},
    {"type": "move", "source": "Header.js", "destination": "src/components/Header.js"},
    {"type": "move", "source": "Footer.js", "destination": "src/components/Footer.js"},
    {"type": "write", "path": "src/components/index.js", "content": "export { Header } from './Header';\nexport { Footer } from './Footer';"}
  ],
  "atomic": true,
  "create_backup": true
}
```

### Example 3: Validation Only (Dry Run)

```json
{
  "operations": [
    {"type": "delete", "path": "important.txt"},
    {"type": "write", "path": "backup.txt", "content": "backup"}
  ],
  "atomic": true,
  "create_backup": true,
  "validate_only": true
}
```

## Request Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `operations` | required | Array of operations |
| `atomic` | `true` | All-or-nothing execution |
| `create_backup` | `true` | Backup affected files before execution |
| `validate_only` | `false` | Validate without executing |

## Pipeline Mode

For multi-step transformations, use `pipeline_json` instead of `request_json`:

```json
{
  "name": "refactor-todos",
  "parallel": true,
  "stop_on_error": true,
  "create_backup": true,
  "steps": [
    {"id": "find", "action": "search", "params": {"path": ".", "pattern": "TODO", "file_types": [".go"]}},
    {"id": "count", "action": "count_occurrences", "input_from": "find", "params": {"pattern": "TODO"}},
    {"id": "fix", "action": "edit", "input_from": "find",
     "condition": {"type": "count_gt", "step_ref": "count", "value": "0"},
     "params": {"old_text": "TODO", "new_text": "DONE"}}
  ]
}
```

Pipeline features:
- **12 actions**: search, read_ranges, count_occurrences, edit, multi_edit, regex_transform, copy, rename, delete, aggregate, diff, merge
- **Conditions**: has_matches, no_matches, count_gt/lt/eq, file_exists, step_succeeded/failed
- **Templates**: `{{step_id.field}}` references to prior step results
- **Parallel execution**: DAG-based scheduling with safety serialization for destructive ops

See [PIPELINE_GUIDE.md](PIPELINE_GUIDE.md) for a full parallel pipeline walkthrough, or the main [CLAUDE.md](../../CLAUDE.md) for reference data.

## Error Handling

### Atomic mode (`atomic: true`)
- All completed operations are **automatically rolled back** on failure
- Original state is restored
- Detailed error message identifies which operation failed

### Non-atomic mode (`atomic: false`)
- Operations continue despite failures
- Partial results are returned

## Best Practices

1. **Always use `atomic: true`** for related operations
2. **Always use `create_backup: true`** for destructive operations
3. **Use `validate_only: true` first** for critical operations
4. **Order operations carefully** — dependencies must come first (e.g., `create_dir` before `write`)
5. **Only use supported types** — `write`, `edit`, `search_and_replace`, `copy`, `move`, `delete`, `create_dir`

## Backup Management

Backups are stored in the configured backup directory (default: `%TEMP%\mcp-batch-backups\`).

To manage backups, use the `backup` tool:
```
backup(action: "list")                    # List all backups
backup(action: "undo_last")              # Undo the most recent operation
backup(action: "restore", backup_id: "...") # Restore a specific backup
backup(action: "cleanup", older_than_days: 7)  # Clean old backups
```

---

*Version: 4.1.2 | Updated: 2026-03-17*
