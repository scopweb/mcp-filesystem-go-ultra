# MCP Filesystem Server

**v3.14.2** · Go · MCP 2025-11-25 · 56 tools

A high-performance [Model Context Protocol](https://modelcontextprotocol.io) filesystem server written in Go. Designed for use with Claude Desktop and Claude Code, with first-class support for large files, WSL/Windows interoperability, and token-efficient responses.

---

## Features

- **56 MCP tools** covering read, write, edit, search, copy, move, delete, streaming, and backup operations
- **Intelligent editing** with automatic backup, risk assessment, and rollback on failure
- **3-tier cache** (BigCache + go-cache) with file watcher invalidation for O(1) reads
- **Streaming and chunked I/O** for files up to 50 MB without blocking
- **WSL ↔ Windows path translation** — accepts `/mnt/c/...`, `C:\...`, and `/tmp/...` (UNC) transparently
- **Compact mode** — minimal token responses (~90% reduction) for high-volume Claude Desktop sessions
- **Risk assessment** — edits above configurable thresholds (30 / 50 / 90% change) require explicit confirmation
- **Hook system** — pre/post hooks for write, edit, delete, create, move, and copy events
- **Plan mode** — dry-run analysis with diff preview and risk report before applying changes
- **Pipeline system** — chain search → edit → verify in a single MCP call (5–22× fewer round-trips)
- **Atomic batch operations** — grouped file operations with rollback on failure
- **Access control** — restrict the server to specific directory trees via `--allowed-paths`

---

## Build

```bash
go build -ldflags="-s -w" -trimpath -o filesystem-ultra.exe .
```

Requires Go 1.25+. No CGO. Tested on Windows 11 and Ubuntu 22.04 (WSL2).

```bash
# Run tests
go test ./tests/... ./core/...

# With race detector
go test -race ./...
```

---

## Configuration

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\MCPs\\clone\\mcp-filesystem-go-ultra\\filesystem-ultra.exe",
      "args": [
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "C:\\your\\project\\"
      ]
    }
  }
}
```

The positional arguments after the flags are the allowed base paths. Omitting `--allowed-paths` (and positional paths) disables access control entirely.

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--compact-mode` | off | Minimal token responses |
| `--cache-size` | 100MB | In-memory file cache limit |
| `--parallel-ops` | 2×CPU (max 16) | Max concurrent operations |
| `--backup-dir` | system temp | Directory for automatic backups |
| `--backup-max-age` | 72h | Maximum backup retention |
| `--backup-max-count` | 50 | Maximum backup count per file |
| `--risk-threshold-medium` | 30 | % change that triggers a warning |
| `--risk-threshold-high` | 50 | % change that requires `force: true` |
| `--hooks-enabled` | off | Enable pre/post operation hooks |
| `--hooks-config` | — | Path to hooks configuration JSON |
| `--debug` | off | Verbose debug logging |

---

## Available Tools

### File I/O

| Tool | Description |
|------|-------------|
| `read_file` | Read file contents with cache |
| `write_file` | Atomic write with backup |
| `read_file_range` | Read a line range (avoids loading large files) |
| `streaming_write_file` | Chunked write for large files |
| `chunked_read_file` | Chunked read for large files |
| `get_file_info` | Size, permissions, timestamps |

### Editing

| Tool | Description |
|------|-------------|
| `edit_file` | Find-and-replace with backup and risk assessment |
| `multi_edit` | Multiple find-and-replace operations in one call |
| `smart_edit_file` | Edit files larger than 1 MB |
| `replace_nth_occurrence` | Replace only the Nth occurrence of a pattern |
| `intelligent_edit` | Auto-selects edit strategy by file size |
| `recovery_edit` | Edit with automatic error recovery (fuzzy match, line normalization) |

### Search

| Tool | Description |
|------|-------------|
| `smart_search` | File name and content search with filters |
| `advanced_text_search` | Full-text search with context lines, case sensitivity, whole-word |
| `search_and_replace` | Regex or literal replacement across a directory tree |
| `count_occurrences` | Count pattern occurrences with optional line numbers |

### Directory Operations

| Tool | Description |
|------|-------------|
| `list_directory` | Directory listing with cache |
| `create_directory` | Create directory tree (`mkdir -p`) |
| `rename_file` | Rename or move within the same volume |
| `move_file` | Move file or directory across paths |
| `copy_file` | Recursive copy preserving permissions |
| `delete_file` | Permanent delete (file or directory) |
| `soft_delete_file` | Move to trash folder instead of deleting |

### Backup & Recovery

| Tool | Description |
|------|-------------|
| `list_backups` | List backups with filters (path, operation, age) |
| `restore_backup` | Restore a file from backup (with preview) |
| `compare_with_backup` | Diff current file against a backup |
| `cleanup_backups` | Remove old backups (supports dry-run) |
| `get_backup_info` | Metadata for a specific backup |

### Analysis & Safety

| Tool | Description |
|------|-------------|
| `analyze_write` | Dry-run write — previews result without writing |
| `analyze_edit` | Dry-run edit — shows diff and risk level |
| `analyze_delete` | Dry-run delete — reports impact |
| `get_edit_telemetry` | Token and operation statistics |
| `get_optimization_suggestion` | Recommends optimal tool for a given file |
| `analyze_file` | Detailed file analysis with strategy recommendation |

### Batch & Pipeline

| Tool | Description |
|------|-------------|
| `batch_operations` | Atomic execution of multiple operations with rollback |
| `execute_pipeline` | Chain steps (search → edit → verify) in one call |

### Artifacts

| Tool | Description |
|------|-------------|
| `capture_last_artifact` | Store content in memory |
| `write_last_artifact` | Write stored artifact to disk (zero token retransmission) |
| `artifact_info` | Size and line count of stored artifact |

### WSL / Windows

| Tool | Description |
|------|-------------|
| `convert_path` | Explicit WSL ↔ Windows path conversion |
| `detect_path_format` | Identify path format (WSL, Windows, UNC, Linux) |
| `sync_to_windows` | Copy a WSL file to the Windows filesystem |
| `sync_to_wsl` | Copy a Windows file into WSL |
| `configure_autosync` | Configure automatic WSL ↔ Windows sync rules |

### Performance & Diagnostics

| Tool | Description |
|------|-------------|
| `performance_stats` | Cache hit rate, operation counts, latency |
| `get_help` | Usage guide and tool selection recommendations |

### Large File Processing

| Tool | Description |
|------|-------------|
| `process_large_file` | Line-by-line processing for files that don't fit in memory |
| `regex_transform` | Advanced regex transformations with capture groups |
| `intelligent_read` | Auto-selects read strategy by file size |
| `intelligent_write` | Auto-selects write strategy by file size |

---

## Architecture

```
main.go                     Entry point — flag parsing, tool registration, server startup
core/
  engine.go                 UltraFastEngine — central struct, cache, worker pool, metrics
  edit_operations.go        EditFile, MultiEdit — backup, risk assessment, hooks
  file_operations.go        Rename, SoftDelete, Copy, Move
  streaming_operations.go   StreamingWrite, ChunkedRead, SmartEdit
  search_operations.go      SmartSearch, AdvancedTextSearch
  backup_manager.go         Create, restore, compare, and clean backups
  impact_analyzer.go        Risk assessment (LOW / MEDIUM / HIGH / CRITICAL)
  edit_safety_layer.go      Context validation, stale-edit prevention
  hooks.go                  Pre/post hook system (12 event types)
  large_file_processor.go   Line-by-line and chunk-based processing
  regex_transformer.go      Regex transformations with capture groups
  pipeline.go               Multi-step pipeline execution
  batch_operations.go       Atomic batch operations with rollback
  plan_mode.go              Dry-run analysis
  path_converter.go         WSL ↔ Windows path conversion
  path_detector.go          Path format detection and WSL distro lookup
  wsl_sync.go               WSL/Windows file synchronization
  config.go                 Thresholds and constants
  errors.go                 PathError, ValidationError, EditError, etc.
cache/
  intelligent.go            BigCache (files) + go-cache (dirs + metadata)
```

### File size thresholds

| Class | Size | Strategy |
|-------|------|----------|
| Small | < 100 KB | Direct I/O |
| Medium | < 500 KB | Streaming |
| Large | < 5 MB | Chunking |
| Very large | < 50 MB | Special handling |
| Over limit | ≥ 50 MB | Edit rejected |

---

## Security

- `isPathAllowed()` resolves symlinks via `filepath.EvalSymlinks()` before containment check — prevents symlink escape from allowed paths
- Temp files and backup IDs use `crypto/rand` (not timestamps)
- Backup IDs are sanitized to `[a-zA-Z0-9_-]` to prevent path traversal
- Temp files and backup metadata written with `0600` permissions
- `copyDirectory()` skips symlinks

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/mark3labs/mcp-go` | v0.43.2 | MCP server SDK |
| `github.com/allegro/bigcache/v3` | v3.1.0 | File content cache |
| `github.com/patrickmn/go-cache` | v2.1.0 | Directory and metadata cache |
| `github.com/panjf2000/ants/v2` | v2.11.5 | Goroutine pool |
| `github.com/fsnotify/fsnotify` | v1.9.0 | File system event watching |

---

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the full version history.

Current release: **v3.14.2** — fixes `batch_operations` edit operations discarding file content instead of performing find-and-replace.

---

## License

MIT
