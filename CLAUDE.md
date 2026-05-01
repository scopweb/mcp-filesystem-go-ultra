# CLAUDE.md - Project Context for Claude Code

## Project Overview

MCP Filesystem Server Ultra-Fast (v4.2.1) - A high-performance MCP (Model Context Protocol) filesystem server written in Go, optimized for Claude Desktop and Claude Code. Provides **16 MCP tools** (consolidated from 59 in v3.x) for file operations, search, editing, backups, streaming, and WSL/Windows integration. All tools include MCP spec-compliant annotations (readOnlyHint, destructiveHint, idempotentHint).

## Build & Test

```bash
# Build (Windows) — v4 binary
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe .

# Or use the build script
build-windows.bat

# Run all tests
go test ./tests/...
go test ./core/...
go test ./tests/security/...

# Run with race detector
go test -race ./...

# Run security fuzzing
go test -fuzz=Fuzz ./tests/security

# Run specific test
go test ./tests/ -run TestName -v

# Build dashboard (separate binary)
go build -ldflags="-s -w" -trimpath -o dashboard.exe ./cmd/dashboard/
```

## Key Consolidations (v3 59 tools → v4 16 core tools)
- `read_file` replaces: `read_file`, `chunked_read_file`, `intelligent_read`, `read_file_range`, `read_base64`
- `write_file` replaces: `write_file`, `create_file`, `streaming_write_file`, `intelligent_write`, `write_base64`
- `edit_file` replaces: `edit_file`, `smart_edit_file`, `intelligent_edit`, `recovery_edit`, `search_and_replace`, `replace_nth_occurrence`, `regex_transform_file`
- `search_files` replaces: `smart_search`, `advanced_text_search`, `count_occurrences`
- `move_file` also replaces: `rename_file`
- `batch_operations` also replaces: `execute_pipeline`, `batch_rename_files`
- `backup` also replaces: `restore_backup`, `list_backups`, `get_backup_info`, `compare_with_backup`, `cleanup_backups`
- `wsl` also replaces: `wsl_sync`, `wsl_status`, `wsl_to_windows_copy`, `windows_to_wsl_copy`, `configure_autosync`
- `server_info` also replaces: `stats`, `get_help`, `artifact`, `performance_stats`, `get_edit_telemetry`
- `analyze_operation` also replaces: `analyze_file`, `analyze_write`, `analyze_edit`, `analyze_delete`, `get_optimization_suggestion`
- `delete_file` also replaces: `soft_delete_file` (soft-delete is now default; use `permanent:true` for hard delete)

## Architecture

```
main.go                    # Entry point: CLI flags, server startup
tools_core.go             # Core 3: read_file, write_file, edit_file
tools_search.go            # search_files
tools_files.go            # get_file_info, move_file, copy_file, delete_file, create_directory
tools_batch.go            # batch_operations
tools_platform.go         # wsl, server_info, analyze_operation
tools_aliases.go          # 6 aliases + fs super-tool + help (discovery tool)
core/
  engine.go                # UltraFastEngine - central struct with cache, worker pool, metrics
  edit_operations.go       # EditFile, MultiEdit with backup, risk assessment, hooks
  file_operations.go       # RenameFile, SoftDeleteFile, CopyFile, MoveFile, etc.
  streaming_operations.go  # StreamingWriteFile, ChunkedReadFile, SmartEditFile
  search_operations.go     # SmartSearch, AdvancedTextSearch
  backup_manager.go        # BackupManager - create/restore/compare/cleanup backups
  impact_analyzer.go       # Risk assessment (LOW/MEDIUM/HIGH/CRITICAL)
  edit_safety_layer.go     # EditSafetyValidator - context validation, stale edit prevention
  hooks.go                 # Pre/post operation hook system (12 event types)
  large_file_processor.go  # Line-by-line and chunk-based processing
  regex_transformer.go     # Advanced regex transformations with capture groups
  batch_operations.go      # Batch file operations with rollback
  batch_rename.go          # Batch file renaming
  audit_logger.go          # AuditLogger - JSON Lines operation log + MetricsSnapshot writer
  claude_optimizer.go      # Claude Desktop auto-optimization (small/large file strategy)
  plan_mode.go             # Dry-run analysis (analyze_write, analyze_edit, analyze_delete)
  path_converter.go        # WSL <-> Windows path conversion
  path_detector.go         # Path format detection
  wsl_sync.go              # WSL/Windows file synchronization
  autosync_config.go       # Auto-sync configuration system
  watcher.go               # File watcher for cache invalidation
  mmap.go                  # Memory-mapped file I/O (Windows fallback)
  config.go                # Performance thresholds and constants
  errors.go                # Custom error types (PathError, ValidationError, EditError, etc.)
  param_validator.go       # Strict schema validation: unknown param rejection, type checking
cache/
  intelligent.go           # 3-tier cache: BigCache (files) + go-cache (dirs) + go-cache (metadata)
mcp/
  mcp.go                   # MCP type definitions (legacy, mostly unused)
tests/
  mcp_functions_test.go    # Core MCP function tests
  bug5_test.go - bug9_test.go  # Regression tests
  edit_safety_test.go      # Edit safety validation tests
  security/                # Security & fuzzing tests (package: security)
cmd/
  dashboard/
    main.go              # Separate binary: HTTP dashboard for logs/metrics/backups
    static/              # Embedded web UI (go:embed) - HTML + vanilla JS + CSS

## Tool Inventory (v4.1.3 — 24 tools total)

```
16 CORE:   read_file, write_file, edit_file, list_directory, search_files,
           get_file_info, move_file, copy_file, delete_file, create_directory,
           batch_operations, backup, analyze_operation, wsl, server_info, multi_edit
6 ALIASES: read_text_file, search, edit, write, create_file, directory_tree
1 HELP:    help         (discovery tool — call first to see all tools)
1 SUPER:   fs           (dispatch to all 16 ops via action param)
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/mark3labs/mcp-go v0.43.2` | MCP server SDK |
| `github.com/allegro/bigcache/v3 v3.1.0` | High-performance file content cache |
| `github.com/patrickmn/go-cache v2.1.0` | Directory/metadata cache |
| `github.com/panjf2000/ants/v2 v2.11.5` | Goroutine worker pool |
| `github.com/fsnotify/fsnotify v1.9.0` | File system event watching |

Go version: 1.26.0

## Core Patterns

### Access Control
```go
if len(e.config.AllowedPaths) > 0 {
    if !e.IsPathAllowed(path) {
        return nil, &PathError{Op: "operation", Path: path, Err: fmt.Errorf("access denied")}
    }
}
```
`IsPathAllowed()` (exported) resolves symlinks via `filepath.EvalSymlinks()` before checking containment. Empty AllowedPaths = no restrictions. Batch operations also enforce this check via `validateOperations()`.

### Error Handling
Custom error types in `core/errors.go`: `PathError`, `ValidationError`, `CacheError`, `EditError`, `ContextError`. Always wrap with `fmt.Errorf("context: %w", err)`.

### MCP Tool Response
```go
// Success
return mcp.NewToolResultText("result text"), nil

// Error
return mcp.NewToolResultError("error message"), nil
```

### Tool Registration (tools_core.go)
Tools are registered in `registerTools()` (tools_core.go) via `toolRegistry.addTool()` which registers both the MCP tool AND adds its handler to the dispatch map for the `fs` super-tool.

### Concurrency
- `engine.operationSem` channel-based semaphore limits parallel ops
- `engine.pool` (ants worker pool) for goroutine management
- `sync.RWMutex` for shared caches (regex cache, env detection)
- All public methods accept `context.Context`

### File Size Thresholds (core/config.go)
- Small: < 100KB (direct I/O)
- Medium: < 500KB (streaming)
- Large: < 5MB (chunking)
- VeryLarge: < 50MB (special handling, edit rejected above this)

### Risk Assessment
Operations that modify > 30% of file or have > 50 occurrences get MEDIUM risk. > 50% or > 100 occurrences = HIGH. > 90% = CRITICAL. **All operations auto-proceed with backup — never blocked.** HIGH/CRITICAL include VERIFY instruction in response.

### Backup System
Backups stored in configurable dir (default: temp/mcp-batch-backups). Backup IDs are `timestamp-random` format. Metadata stored as JSON alongside backup files. Sanitized against path traversal.

## Code Conventions

- **Naming**: PascalCase for exported types/functions, camelCase for unexported
- **Imports**: stdlib first, then third-party, then local packages
- **Context**: All public operations accept `context.Context` as first param
- **Compact mode**: Many functions check `e.config.CompactMode` to return shorter responses
- **Path normalization**: `NormalizePath()` called early in every tool handler for WSL/Windows compat
- **Language**: Code in English, user-facing docs in both English and Spanish

## Configuration (CLI Flags)

Key flags parsed in `main.go`:
- `--allowed-paths` - Comma-separated or positional args for allowed base paths
- `--compact-mode` - Minimal token responses for Claude Desktop
- `--cache-size` - Cache memory limit (default: 100MB)
- `--parallel-ops` - Max concurrent ops (default: 2x CPU, max 16)
- `--hooks-enabled` / `--hooks-config` - Hook system
- `--backup-dir` / `--backup-max-age` / `--backup-max-count` - Backup config
- `--risk-threshold-medium` / `--risk-threshold-high` - Risk thresholds
- `--debug` / `--log-level` - Logging (debug, info, warn, error — configures slog JSON handler)
- `--log-dir` - Directory for audit logs and metrics snapshots (enables operation logging)

## Logging & Dashboard

### Audit Logging (--log-dir)
When `--log-dir` is set, the MCP server writes:
- `operations.jsonl` — JSON Lines audit log (one entry per tool call, auto-rotates at 10MB, keeps last 3)
- `metrics.json` — Performance metrics snapshot (updated every 30 seconds)

All 16 core tools are wrapped with `auditWrap()` in audit.go which records timing, status, path, bytes_in/out, file_size, args summary, risk level, lines_changed, and matches count. Zero overhead when `--log-dir` is not set (fast-path returns the handler unchanged).

### Dashboard Binary
Separate binary in `cmd/dashboard/` — reads log files and serves a web UI:
```bash
# Build
go build -ldflags="-s -w" -trimpath -o dashboard.exe ./cmd/dashboard/

# Run (or use run-dashboard.bat)
dashboard.exe --log-dir=<same as MCP server> --backup-dir=<backup path> --port=9100
```
- No coupling with MCP server (file-based communication only)
- Embedded web assets via `go:embed` (single binary)
- Real-time updates via SSE (Server-Sent Events)
- Pages: Dashboard (metrics), Operations (audit log), Backups (enterprise recovery), Statistics, Proxy/Tokens, Edit Analysis

### Enterprise Backup Recovery (Dashboard)
The Backups page provides a full search/filter/recovery system:
- **Summary cards**: Total Backups, Total Size, Latest Backup, Protected Files
- **Search**: text filter (file name, backup ID, context), operation type dropdown, date presets (today/24h/7d/30d/custom range)
- **Content Search**: grep inside backup files with context snippets (2 lines before/after match), 10s timeout
- **Pagination**: server-side with configurable limit/offset
- **Unified backup format**: both normal backups (`backup_id` + `files/` subdir) and batch backups (`batch-*` dirs with `op-N-filename` files) are normalized into a common `BackupInfo` structure
- **Cache**: `backupCache` with 30s TTL avoids repeated disk scans
- **API endpoints**:
  - `GET /api/backups` — all backups (cached, unified)
  - `GET /api/backups/search?q=&operation=&preset=&from=&to=&limit=&offset=` — filtered + paginated
  - `GET /api/backups/search-content?q=&max_results=` — grep inside backup files
  - `GET /api/backups/detail/{id}` — single backup with file details
  - `GET /api/backups/file/{id}/{filename}` — serve backup file (tries `files/` then direct)

## Pipeline Transformation System

The `execute_pipeline` tool provides multi-step file transformation pipelines with 12 actions, conditional logic, template variables, and parallel execution.

### Actions (12)

| Action | Type | Description |
|--------|------|-------------|
| `search` | Read | Search files by pattern (regex or literal) |
| `read_ranges` | Read | Read file contents |
| `count_occurrences` | Read | Count pattern occurrences per file |
| `edit` | Write | Search-and-replace across files |
| `multi_edit` | Write | Multiple edits per file |
| `regex_transform` | Write | Regex-based transformations with capture groups |
| `copy` | Write | Copy files to destination |
| `rename` | Write | Rename/move files |
| `delete` | Write | Soft-delete files |
| `aggregate` | Meta | Combine content/files from multiple steps |
| `diff` | Meta | Line-by-line unified diff between two files |
| `merge` | Meta | Union or intersection of file lists from multiple steps |

### Chaining

- **`input_from`**: Reference a prior step's `FilesMatched` as input. E.g., search → edit chain.
- **`input_from_all`**: Reference multiple prior steps (for `aggregate` and `merge`). Array of step IDs.

### Conditional Steps

Steps can include a `condition` object. If the condition evaluates to false, the step is skipped (`success=true, skipped=true`).

**9 condition types:**
- `has_matches` / `no_matches` — check if referenced step found files
- `count_gt` / `count_lt` / `count_eq` — compare total count against threshold value
- `file_exists` / `file_not_exists` — check file existence by path
- `step_succeeded` / `step_failed` — check prior step's success status

```json
{"condition": {"type": "count_gt", "step_ref": "count-step", "value": "5"}}
```

### Template Variables

Step params support `{{step_id.field}}` references that resolve to prior step results at runtime.

**Available fields:** `count` (sum of counts), `files_count`, `files` (comma-separated), `risk`, `edits`

```json
{"params": {"message": "Found {{find.count}} in {{find.files_count}} files"}}
```

Unresolved references (unknown step/field) are left as-is. Templates work recursively in nested maps and arrays.

### Parallel Execution

Set `parallel: true` on the pipeline request. The DAG scheduler:
1. Builds dependency graph from `input_from`, `input_from_all`, and `condition.step_ref`
2. Groups independent steps into execution levels via topological sort
3. Runs steps within each level concurrently using the engine's worker pool
4. Destructive actions (edit, multi_edit, regex_transform, delete, rename) in the same level are serialized into sub-levels for safety

### Risk Thresholds

| Level | Files | Edits |
|-------|-------|-------|
| MEDIUM | ≥30 | ≥100 |
| HIGH | ≥50 | ≥500 |
| CRITICAL | ≥80 | ≥1000 |

HIGH and CRITICAL operations are blocked unless `force: true` is set.

### Pipeline Flags

- `dry_run: true` — Preview changes without applying
- `force: true` — Bypass risk warnings
- `stop_on_error: true` — Stop at first failure (default), triggers rollback if backup exists
- `create_backup: true` — Auto-backup affected files before destructive steps
- `verbose: true` — Return intermediate data (file contents, per-file counts)
- `parallel: true` — Enable DAG-based parallel execution

### Progress Tracking

When `--log-dir` is set, each completed step emits a separate audit entry with `sub_op: "step:N/M:stepID:action"`, enabling real-time per-step visibility in the dashboard.

### Files

| File | Purpose |
|------|---------|
| `core/pipeline.go` | Executor: sequential/parallel dispatch, action handlers, rollback |
| `core/pipeline_types.go` | Types: PipelineRequest, PipelineStep, StepResult, validation |
| `core/pipeline_conditions.go` | 9 condition types: evaluation and validation |
| `core/pipeline_templates.go` | `{{step_id.field}}` template resolution |
| `core/pipeline_scheduler.go` | DAG builder, topological sort, destructive step splitting |
| `core/large_file_processor.go` | In-memory / line-by-line / chunk processing modes |
| `core/regex_transformer.go` | Sequential/parallel regex transformations |
| `core/errors.go` | PipelineStepError with step context and hints |

### Example Pipeline

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

## Security Notes

- All temp files use `crypto/rand` for names (not predictable timestamps)
- Backup IDs sanitized: only alphanumeric, `-`, `_` allowed
- `IsPathAllowed()` resolves symlinks before containment check (exported for subsystem use)
- Batch operations enforce `--allowed-paths` via `IsPathAllowed()` in `validateOperations()`
- Strict parameter validation: unknown params rejected, types enforced (see `core/param_validator.go`)
- `copyDirectory()` skips symlinks
- Temp files and backup metadata use 0600 permissions
- No `unsafe` package usage in production code

---

## filesystem-ultra Tool Reference (v4.1.3 — 16 tools)

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
| JSON output | `search_files` | `path`, `pattern`, `include_context: true`, `output_format: "json"` |
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

**Backup format in responses:**
- Compact: `M file.go | 7@+7-0 | 42L | UNDO:20260501-123650 | chain:20260501-123649`
  - `UNDO:20260501-123650` — truncated ID (12 chars), use for display or `backup(action:"undo_last", file_path:"...")`
  - `chain:20260501-123649` — parent backup ID, indicates previous version available for step-through undo
- Verbose: `✓ UNDO:20260501-123650-333c964cc3af7a82 ← chain:20260501-123649-...`

**Undo operations:**
- Quick undo (no backup_id needed): `backup(action:"undo_last")`
- **Step-through undo** (recomendado): `backup(action:"undo_last", file_path:"...")` — recorre la cadena de backups hacia atrás, uno a uno
- Preview next step: `backup(action:"undo_last", file_path:"...", preview:true)`
- Ver cadena completa: `backup(action:"undo_chain", file_path:"...")`
- Restore specific backup: `backup(action:"restore", backup_id:"20260501-123650-333c964cc3af7a82")` (full ID required)
- Find backups for a specific file: `backup(action:"list", filter_path:"filename")`

**Auto-verify integrity**: operaciones HIGH/CRITICAL incluyen verificación automática post-edit (legibilidad, tamaño, hash). Si el archivo quedó corrupto o truncado, se reporta inmediatamente.

For HIGH risk edits, verify the result: `read_file(path, mode:"tail")` to confirm file is complete.

Full recovery guide: `server_info(action:"help", topic:"recovery")`

### 11. Use filesystem tools — never bash alternatives
- **Search files** → `search_files` (never `grep`, `find`, `rg`)
- **Read files** → `read_file` (never `cat`, `type`, `bat`)
- **List directories** → `list_directory` (never `ls`, `dir`)

Bash commands bypass the MCP cache, skip audit logging, and return untyped output.
