# Batch Operations Guide

## Overview

The `batch_operations` tool allows you to execute multiple file operations **atomically** with automatic rollback on failure. This is perfect for complex multi-step operations where you need **all-or-nothing** execution.

## Key Features

- **Atomic Transactions**: All operations succeed or all are rolled back
- **Automatic Backups**: Creates backups before destructive operations
- **Pre-execution Validation**: Validates all operations before executing
- **Rollback on Failure**: Automatically reverts changes if any operation fails
- **Progress Tracking**: Detailed results for each operation
- **Validate-Only Mode**: Test operations without executing them

## Supported Operations

### 1. `write` - Write file
```json
{
  "type": "write",
  "path": "/path/to/file.txt",
  "content": "file content here"
}
```

### 2. `edit` - Edit file content
```json
{
  "type": "edit",
  "path": "/path/to/file.txt",
  "old_text": "text to replace",
  "new_text": "new text"
}
```

### 3. `move` - Move/rename file
```json
{
  "type": "move",
  "source": "/path/to/old.txt",
  "destination": "/path/to/new.txt"
}
```

### 4. `copy` - Copy file
```json
{
  "type": "copy",
  "source": "/path/to/source.txt",
  "destination": "/path/to/destination.txt"
}
```

### 5. `delete` - Delete file
```json
{
  "type": "delete",
  "path": "/path/to/file.txt"
}
```

### 6. `create_dir` - Create directory
```json
{
  "type": "create_dir",
  "path": "/path/to/new/directory"
}
```

## Usage Examples

### Example 1: Simple Batch Write

```json
{
  "tool": "batch_operations",
  "arguments": {
    "request_json": "{\"operations\":[{\"type\":\"write\",\"path\":\"C:\\\\temp\\\\file1.txt\",\"content\":\"Hello\"},{\"type\":\"write\",\"path\":\"C:\\\\temp\\\\file2.txt\",\"content\":\"World\"}],\"atomic\":true,\"create_backup\":true}"
  }
}
```

### Example 2: Refactoring Multiple Files

```json
{
  "operations": [
    {
      "type": "edit",
      "path": "src/utils.js",
      "old_text": "function oldName(",
      "new_text": "function newName("
    },
    {
      "type": "edit",
      "path": "src/index.js",
      "old_text": "oldName()",
      "new_text": "newName()"
    },
    {
      "type": "edit",
      "path": "tests/utils.test.js",
      "old_text": "oldName()",
      "new_text": "newName()"
    }
  ],
  "atomic": true,
  "create_backup": true
}
```

### Example 3: Reorganize Project Structure

```json
{
  "operations": [
    {
      "type": "create_dir",
      "path": "src/components"
    },
    {
      "type": "move",
      "source": "Header.js",
      "destination": "src/components/Header.js"
    },
    {
      "type": "move",
      "source": "Footer.js",
      "destination": "src/components/Footer.js"
    },
    {
      "type": "write",
      "path": "src/components/index.js",
      "content": "export { Header } from './Header';\nexport { Footer } from './Footer';"
    }
  ],
  "atomic": true,
  "create_backup": true
}
```

### Example 4: Validation Only (Dry Run)

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

### `operations` (required)
Array of operations to execute. Each operation must have a `type` field and type-specific fields.

### `atomic` (optional, default: `true`)
If `true`, all operations are rolled back if any single operation fails.

### `create_backup` (optional, default: `true`)
If `true`, creates a timestamped backup of all affected files before execution.

### `validate_only` (optional, default: `false`)
If `true`, only validates operations without executing them. Useful for testing.

## Response Format

### Success Response

```
✅ Batch Operations Completed Successfully
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📊 Summary:
  Total operations: 3
  Completed: 3
  Failed: 0
  Execution time: 45.2ms
  Backup created: C:\Temp\mcp-batch-backups\batch-20250124-153045

📋 Operation Details:
  ✓ [0] write: C:\temp\file1.txt (5 B)
  ✓ [1] write: C:\temp\file2.txt (5 B)
  ✓ [2] move: old.txt (1.2 KB)
```

### Failure Response (with Rollback)

```
❌ Batch Operations Failed
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📊 Summary:
  Total operations: 3
  Completed: 1
  Failed: 1
  Execution time: 23.1ms
  Backup created: C:\Temp\mcp-batch-backups\batch-20250124-153045

⚠️  Rollback performed - all changes reverted

📋 Operation Details:
  ✓ [0] write: C:\temp\file1.txt (5 B)
  ✗ [1] write: C:\temp\readonly.txt - Error: permission denied

❌ Errors:
  • Operation 1 failed, rollback completed: permission denied
```

### Validation-Only Response

```
✅ Batch Validation Results
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ All 3 operations validated successfully
✓ Ready to execute
```

## Best Practices

### 1. Always Use Atomic Mode for Related Operations
```json
{
  "operations": [...],
  "atomic": true  // ✅ Ensures consistency
}
```

### 2. Create Backups for Destructive Operations
```json
{
  "operations": [
    {"type": "delete", "path": "file.txt"}
  ],
  "create_backup": true  // ✅ Safety net
}
```

### 3. Validate First for Critical Operations
```json
// Step 1: Validate
{
  "operations": [...],
  "validate_only": true
}

// Step 2: If validation succeeds, execute
{
  "operations": [...],
  "validate_only": false
}
```

### 4. Order Operations Carefully
Operations execute in sequence. Ensure dependencies are ordered correctly:
```json
{
  "operations": [
    {"type": "create_dir", "path": "new_folder"},  // ✅ Create folder first
    {"type": "write", "path": "new_folder/file.txt", "content": "..."}  // ✅ Then write file
  ]
}
```

## Error Handling

### Validation Errors
If validation fails, you'll get a detailed error response:
```
✗ Validation failed
Errors:
  • Op 0: parent directory does not exist: /nonexistent
  • Op 2: file does not exist: /missing.txt
```

### Execution Errors
If execution fails and `atomic: true`:
- All completed operations are **automatically rolled back**
- Original state is restored
- Detailed error message is provided

If `atomic: false`:
- Operations continue despite failures
- Partial results are returned
- No rollback occurs

## Use Cases

### 1. Code Refactoring
Rename functions/variables across multiple files atomically.

### 2. Project Restructuring
Move files, create directories, update imports - all or nothing.

### 3. Configuration Updates
Update multiple config files together, rollback if any fails.

### 4. Database Migrations
Apply schema changes across multiple files atomically.

### 5. Deployment Preparation
Copy files, update versions, create backups - all in one transaction.

## Performance Notes

- **Validation**: ~1-5ms per operation
- **Backup Creation**: Depends on file sizes
- **Execution**: Depends on operation type
- **Rollback**: Usually faster than forward execution

## Backup Management

Backups are stored in:
- **Windows**: `%TEMP%\mcp-batch-backups\`
- **Linux/Mac**: `/tmp/mcp-batch-backups/`

Backups include:
- Timestamped folder (e.g., `batch-20250124-153045`)
- Original files before modification
- `metadata.json` with operation details

Old backups are automatically cleaned, keeping only the last 10.

## Limitations

- Maximum operations per batch: No hard limit (recommended < 100)
- File size limits: Same as individual operations
- Nested transactions: Not supported
- Concurrent batches: Managed via mutex (one at a time)

## Example: Complete Workflow

```json
{
  "operations": [
    {
      "type": "create_dir",
      "path": "backup"
    },
    {
      "type": "copy",
      "source": "important.txt",
      "destination": "backup/important.txt"
    },
    {
      "type": "edit",
      "path": "important.txt",
      "old_text": "version: 1.0",
      "new_text": "version: 2.0"
    },
    {
      "type": "write",
      "path": "CHANGELOG.md",
      "content": "## Version 2.0\n- Updated version"
    }
  ],
  "atomic": true,
  "create_backup": true,
  "validate_only": false
}
```

This will:
1. Create `backup` directory
2. Copy `important.txt` to backup
3. Edit version in `important.txt`
4. Write changelog

If **any** step fails, **all** changes are reverted.
