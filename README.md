# MCP Filesystem Server Ultra-Fast

**v4.1.2** · Go · MCP 2025-11-25 · 16 tools

A high-performance [Model Context Protocol](https://modelcontextprotocol.io) filesystem server written in Go. Designed for use with Claude Desktop and Claude Code, with first-class support for large files, WSL/Windows interoperability, and token-efficient responses.

---

## Features

- **16 MCP tools** (consolidated from 59 in v3.x) — all functionality preserved, zero tool bloat
- **MCP spec-compliant annotations** — `readOnlyHint`, `destructiveHint`, `idempotentHint` on every tool
- **Intelligent editing** with automatic backup, risk assessment, and rollback on failure
- **3-tier cache** (BigCache + go-cache) with file watcher invalidation for O(1) reads
- **Streaming and chunked I/O** for files up to 50 MB without blocking
- **WSL ↔ Windows path translation** — accepts `/mnt/c/...`, `C:\...`, and `/tmp/...` transparently
- **Compact mode** — minimal token responses (~90% reduction) for high-volume Claude Desktop sessions
- **Risk assessment** — edits above configurable thresholds (30 / 50 / 90% change) require explicit confirmation
- **Hook system** — pre/post hooks for write, edit, delete, create, move, and copy events
- **Plan mode** — dry-run analysis with diff preview and risk report before applying changes
- **Pipeline system** — 12 actions with conditions, templates, DAG-based parallel execution (5–22× fewer round-trips)
- **Atomic batch operations** — grouped file operations with rollback on failure
- **Audit logging** — JSON Lines operation log + metrics snapshots for observability
- **Dashboard** — separate HTTP binary for real-time metrics, operation log, and backup recovery
- **Access control** — restrict the server to specific directory trees via `--allowed-paths`

---

## Build

```bash
# Build (Windows) — v4 binary
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe .

# Or use the build script
build-windows.bat
```

Requires Go 1.26+. No CGO. Tested on Windows 11 and Ubuntu 22.04 (WSL2).

```bash
# Run tests
go test ./tests/... ./core/...

# With race detector
go test -race ./...

# Security fuzzing
go test -fuzz=Fuzz ./tests/security
```

---

## Configuration

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\path\\to\\filesystem-ultra-v4.exe",
      "args": [
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "--log-dir", "C:\\logs\\mcp-filesystem",
        "C:\\your\\project\\"
      ]
    }
  }
}
```

The positional arguments after the flags are the allowed base paths. Omitting paths disables access control entirely.

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
| `--log-dir` | — | Directory for audit logs and metrics (enables logging) |
| `--log-level` | info | Log level: debug, info, warn, error |
| `--debug` | off | Verbose debug logging |

---

## Available Tools (16)

### Core (5)

| Tool | Description |
|------|-------------|
| `read_file` | Read full file, line range (`start_line`/`end_line`), head/tail (`max_lines`+`mode`), or base64 (`encoding:"base64"`) |
| `write_file` | Create or overwrite file. Supports text (`content`) and binary (`encoding:"base64"`) |
| `edit_file` | Find-and-replace with backup and risk assessment. Modes: exact match (default), `search_replace` (all occurrences), `regex` (capture groups), `occurrence:N` (Nth match) |
| `list_directory` | Directory listing with cache |
| `search_files` | Search by pattern with optional `file_types`, `include_content`, `include_context`, `case_sensitive`, `count_only` |

### Edit+ (1)

| Tool | Description |
|------|-------------|
| `multi_edit` | Multiple find-and-replace operations on the same file in one call via `edits_json` |

### File Operations (4)

| Tool | Description |
|------|-------------|
| `move_file` | Move or rename file/directory |
| `copy_file` | Recursive copy preserving permissions |
| `delete_file` | Soft-delete (default) or permanent (`permanent: true`) |
| `create_directory` | Create directory tree (`mkdir -p`) |

### Batch & Pipeline (1)

| Tool | Description |
|------|-------------|
| `batch_operations` | Atomic batch ops (`request_json`), multi-step pipelines (`pipeline_json`), or batch rename (`rename_json`) — with rollback on failure |

### Backup (1)

| Tool | Description |
|------|-------------|
| `backup` | Manage backups via `action`: list, info, compare, cleanup, restore |

### Analysis (1)

| Tool | Description |
|------|-------------|
| `analyze_operation` | Dry-run preview via `operation`: file, edit, delete, write, optimize, compare |

### WSL (1)

| Tool | Description |
|------|-------------|
| `wsl` | WSL ↔ Windows sync and status. Params: `wsl_path`/`windows_path` + `direction`, or `action:"status"` |

### Utility (1)

| Tool | Description |
|------|-------------|
| `server_info` | Server diagnostics via `action`: stats, help, artifact |

### Info (1)

| Tool | Description |
|------|-------------|
| `get_file_info` | Size, permissions, timestamps, type |

---

## Dashboard

Separate binary for real-time observability — reads audit logs and serves a web UI.

```bash
# Build
go build -ldflags="-s -w" -trimpath -o dashboard.exe ./cmd/dashboard/

# Run
dashboard.exe --log-dir=C:\logs\mcp-filesystem --backup-dir=C:\backups --port=9100
```

- No coupling with MCP server (file-based communication only)
- Embedded web assets via `go:embed` (single binary, no dependencies)
- Real-time updates via SSE (Server-Sent Events)
- Pages: Dashboard (metrics), Operations (audit log), Backups (enterprise recovery), Statistics, Proxy/Tokens, Edit Analysis

### Audit Logging

When `--log-dir` is set on the MCP server, it writes:
- `operations.jsonl` — JSON Lines audit log (one entry per tool call, auto-rotates at 10MB)
- `metrics.json` — Performance metrics snapshot (updated every 30 seconds)

---

## Architecture

```
main.go                     Entry point — flag parsing, 16 MCP tool registrations, server startup
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
  pipeline.go               Multi-step pipeline execution (sequential + parallel)
  pipeline_types.go         Pipeline types, validation
  pipeline_conditions.go    9 condition types for conditional steps
  pipeline_templates.go     {{step_id.field}} template resolution
  pipeline_scheduler.go     DAG builder, topological sort, destructive step splitting
  batch_operations.go       Atomic batch operations with rollback
  batch_rename.go           Batch file renaming
  audit_logger.go           JSON Lines operation log + MetricsSnapshot writer
  claude_optimizer.go       Claude Desktop auto-optimization (small/large file strategy)
  plan_mode.go              Dry-run analysis (analyze_write, analyze_edit, analyze_delete)
  path_converter.go         WSL ↔ Windows path conversion
  path_detector.go          Path format detection and WSL distro lookup
  wsl_sync.go               WSL/Windows file synchronization
  autosync_config.go        Auto-sync configuration system
  watcher.go                File watcher for cache invalidation
  mmap.go                   Memory-mapped file I/O (Windows fallback)
  config.go                 Thresholds and constants
  errors.go                 PathError, ValidationError, EditError, PipelineStepError, etc.
cache/
  intelligent.go            BigCache (files) + go-cache (dirs + metadata)
cmd/
  dashboard/
    main.go                 HTTP dashboard for logs/metrics/backups
    static/                 Embedded web UI (go:embed)
tests/
  mcp_functions_test.go     Core MCP function tests
  bug5_test.go–bug9_test.go Regression tests
  edit_safety_test.go       Edit safety validation tests
  security/                 Security & fuzzing tests
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
- No `unsafe` package usage in production code

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/mark3labs/mcp-go` | v0.44.1 | MCP server SDK |
| `github.com/allegro/bigcache/v3` | v3.1.0 | File content cache |
| `github.com/patrickmn/go-cache` | v2.1.0 | Directory and metadata cache |
| `github.com/panjf2000/ants/v2` | v2.11.5 | Goroutine pool |
| `github.com/fsnotify/fsnotify` | v1.9.0 | File system event watching |

---

## Documentation

Full documentation at **[filesystem.scopweb.com](https://filesystem.scopweb.com)**.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the full version history.

Current release: **v4.1.2** — version alignment, SECURITY.md, audit fixes.

---

## License

MIT
