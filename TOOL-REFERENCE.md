# filesystem-ultra Tool Reference (v4.4.0 — 31 tools)

### Reading Files

| Need | Tool | Parameters |
|------|------|------------|
| Read full file | `read_file` | `path` |
| Read multiple files | `read_file` | `paths` (JSON array: `'["a.go","b.go"]'`) |
| Read specific lines | `read_file` | `path`, `start_line`, `end_line` |
| Read first/last N lines | `read_file` | `path`, `max_lines`, `mode` ("head" or "tail") |
| Read binary as base64 | `read_file` | `path`, `encoding: "base64"` |

### Writing Files

| Need | Tool | Parameters |
|------|------|------------|
| Write/create file | `write_file` | `path`, `content` |
| Write binary from base64 | `write_file` | `path`, `content_base64` or `content` + `encoding: "base64"` |

### Editing Files

| Need | Tool | Parameters |
|------|------|------------|
| Replace exact text | `edit_file` | `path`, `old_text`, `new_text` |
| Multiple edits same file | `multi_edit` | `path`, `edits_json` (array of `{old_text, new_text}`) |
| Regex find-replace all | `edit_file` | `path`, `mode: "search_replace"`, `pattern`, `replacement` |
| Replace Nth match | `edit_file` | `path`, `old_text`, `new_text`, `occurrence: N` (1=first, -1=last) |
| Regex with captures | `edit_file` | `path`, `mode: "regex"`, `patterns_json` |

### Search

| Need | Tool | Parameters |
|------|------|------------|
| Search files | `search_files` | `path`, `pattern`, `file_types` (optional) |
| Content search | `search_files` | `path`, `pattern`, `include_content: true` |
| Advanced search | `search_files` | `path`, `pattern`, `case_sensitive: true`, `include_context: true` |
| Count pattern | `search_files` | `path`, `pattern`, `count_only: true` |

### File Operations

| Need | Tool | Parameters |
|------|------|------------|
| List directory | `list_directory` | `path` |
| File info | `get_file_info` | `path` |
| File info (batch) | `get_file_info` | `paths` (JSON array: `'["a.go","dir/"]'`) |
| Copy | `copy_file` | `source_path`, `dest_path` |
| Move/rename | `move_file` | `source_path`, `dest_path` |
| Delete (soft) | `delete_file` | `path` |
| Delete multiple | `delete_file` | `paths` (JSON array: `'["a.txt","b.txt"]'`) |
| Delete (permanent) | `delete_file` | `path`, `permanent: true` |
| Create directory | `create_directory` | `path` |

### Batch & Pipeline

| Need | Tool | Parameters |
|------|------|------------|
| Multi-file atomic ops | `batch_operations` | `request_json` — see below |
| Multi-step pipeline | `batch_operations` | `pipeline_json` — see below |
| Batch rename | `batch_operations` | `rename_json` |

### Other

| Need | Tool | Parameters |
|------|------|------------|
| Backup management | `backup` | `action` (list/info/compare/cleanup/restore/undo_last), `backup_id` |
| Restore backup | `backup` | `action: "restore"`, `backup_id`, `file_path` (optional) |
| Undo last edit | `backup` | `action: "undo_last"`, `preview: true` (optional) |
| Analyze before doing | `analyze_operation` | `path`, `operation` (file/edit/delete/write/optimize) |
| Performance stats | `server_info` | `action: "stats"` |
| Help | `server_info` | `action: "help"`, `topic` (optional) |
| Artifact capture | `server_info` | `action: "artifact"`, `sub_action` (capture/write/info) |
| WSL sync | `wsl` | `wsl_path` or `windows_path`, `direction` |
| WSL status | `wsl` | `action: "status"` |

---

## Critical Rules

### 1. ALWAYS verify paths before using them
- Copy-paste paths exactly from `list_directory` or `search_files` results
- **Never retype paths from memory** — typos cause silent failures
- If a tool returns "file not found", double-check the path character by character

### 2. Read before editing
- **ALWAYS** read the file (or relevant range) before calling `edit_file` or `multi_edit`
- Use the exact text from the read result as `old_text`
- `old_text` must match the file content exactly — it's a literal match, not a regex

### 3. batch_operations format
Supported operation types: `write`, `edit`, `search_and_replace`, `copy`, `move`, `delete`, `create_dir`

```json
{
  "operations": [
    {"type": "edit", "path": "file1.cs", "old_text": "old", "new_text": "new"},
    {"type": "edit", "path": "file2.cs", "old_text": "old", "new_text": "new"},
    {"type": "search_and_replace", "path": "file3.cs", "old_text": "pattern", "new_text": "replacement"},
    {"type": "copy", "source": "a.txt", "destination": "b.txt"}
  ],
  "atomic": true,
  "create_backup": true
}
```

**Do NOT use types that don't exist** (e.g. `search_replace`, `find_replace`). Only the 7 types listed above.

### 4. Pipeline JSON — double-escape regex
Inside `pipeline_json`, regex backslashes need double-escaping because it's JSON-in-JSON:
- `\.` → `\\\\.`
- `\b` → `\\\\b`
- `\d+` → `\\\\d+`

For complex regex patterns, prefer using `edit_file` with `mode:"regex"` directly instead of inside a pipeline.

### 5. Large edits — use anchors
For replacing large code blocks (>10 lines):
1. Use a short, unique anchor line as `old_text` in `edit_file` to insert the new block
2. Then use a second `edit_file` to remove the remaining old block
3. **Do NOT** try to put 50+ lines of code as `new_text` inside `batch_operations` — the JSON escaping will break

### 6. One edit at a time on the same file
- After each `edit_file` on a file, re-read before the next edit
- The file content changes after each edit — stale `old_text` from a previous read will fail
- Exception: `multi_edit` handles multiple edits in one call (preferred for same-file changes)

### 7. edit_file modes
- Default mode: replaces ONE exact text match — use for targeted, precise edits
- `mode:"search_replace"`: replaces ALL occurrences of a pattern (regex or literal) — use for global refactors
- `mode:"regex"`: advanced regex with capture groups — use for complex transformations
- Always run `search_files` with `count_only:true` before global replace to verify impact

### 8. Dry Run Before Destructive Operations
- Use `analyze_operation` to preview the impact of write, edit, or delete operations
- Use `edit_file` with `dry_run: true` for regex mode to validate patterns
- Use `batch_operations` pipeline with `dry_run: true` to preview pipeline execution

### 9. Error recovery
- If `edit_file` says "old_text not found": re-read the file and try again with exact text
- If `batch_operations` fails: check the error for which operation failed and why
- If a tool returns no response (timeout): retry once, the file system may have been briefly locked

### 10. Disaster Recovery — UNDO and backup restore
- Every `edit_file` and `multi_edit` response includes: `UNDO: backup(action:"restore", backup_id:"...")`
- Quick undo (no backup_id needed): `backup(action:"undo_last")`
- Preview before undo: `backup(action:"undo_last", preview:true)`
- Find backups for a specific file: `backup(action:"list", filter_path:"filename")`
- If edits make things WORSE, **STOP editing and RESTORE** from backup — repeated edits on a broken file make recovery harder
- For HIGH risk edits, verify the result: `read_file(path, mode:"tail")` to confirm file is complete
- Full recovery guide: `server_info(action:"help", topic:"recovery")`

### 11. Use filesystem tools — never bash alternatives
- **Search files** → `search_files` (never `grep`, `find`, `rg`)
- **Read files** → `read_file` (never `cat`, `type`, `bat`)
- **List directories** → `list_directory` (never `ls`, `dir`)

The filesystem MCP server provides structured, cached, annotated results. Bash commands bypass the cache, skip audit logging, and return untyped output.
