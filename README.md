# MCP Filesystem Server Ultra

**v4.5.29** · Go · MCP 2025-11-25 · 20 tools (17 core + git + minify_js + help)

A [Model Context Protocol](https://modelcontextprotocol.io) filesystem server written in Go, designed for **safe file editing by AI agents**: automatic backups with step-through undo, optimistic concurrency to detect external file changes, an accidental-rewrite guard, strict path security, and risk assessment on every mutation. Built for Claude Desktop and Claude Code, with support for large files, WSL/Windows interoperability, and token-efficient responses.

Legacy aliases (`read_text_file`, `View`, `Edit`, etc.) and the `fs` super-tool are disabled; only the 20 canonical tool names are registered.

---

## Features

### Safety and correctness

- **Automatic backups with step-through undo** — every mutation is recoverable: `backup(action:"undo_last")` walks the chain; `restore` returns a file to its pre-edit bytes
- **Optimistic concurrency (OCC)** — `content_hash`/`expected_hash` chaining detects external file changes between read and edit; `--auto-occ` warns or blocks on stale edits
- **Accidental-rewrite guard** (v4.5.10) — blocks `edit_file` calls that look like unintended full-file rewrites
- **Path security** — symlink-resolved containment via `filepath.Rel`, NTFS ADS blocking, RTLO/zero-width Unicode rejection, Windows reserved names, TOCTOU symlink defense
- **Risk assessment** — mutations above configurable thresholds are flagged (20% change = MEDIUM, 75% = HIGH by default); HIGH/CRITICAL results include post-edit integrity verification
- **Access control** — restrict the server to specific directory trees via `--allowed-paths` (also enforced in batch operations)
- **Plan mode** — dry-run analysis with diff preview and risk report before applying changes
- **Structured output** — `outputSchema` + `structuredContent` on the 4 I/O core tools, with a handler-level conformance sweep enforced in CI

### Productivity

- **20 tools** — 17 core + `git` + `minify_js` + `help`, consolidated from 59 in v3.x with no loss of functionality
- **MCP spec-compliant annotations** — `readOnlyHint`, `destructiveHint`, `idempotentHint` on every tool
- **Hook system** — 16 pre/post events (write, edit, delete, create, move, copy, read, search)
- **Pipeline system** — 12 actions with conditions, templates, and DAG-based parallel execution; reduces client/server round-trips for multi-step refactors
- **Atomic batch operations** — grouped file operations with rollback on failure
- **Compact mode** — reduced-token responses for high-volume sessions
- **Audit logging** — JSON Lines operation log + metrics snapshots
- **Dashboard** — separate HTTP binary for real-time metrics, operation log, and backup recovery

### Performance

- **3-tier cache** (BigCache + go-cache) with file-watcher invalidation
- **Streaming and chunked I/O** for files up to 50 MB
- **WSL ↔ Windows path translation** — accepts `/mnt/c/...`, `C:\...`, and `/tmp/...` transparently
- **Optional embedded ripgrep** (`embed_rg` tag) for accelerated content search

---

## Build

```bash
# Build (Windows) — v4 binary
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe .

# With ripgrep embedded (~4MB larger)
go build -ldflags="-s -w" -trimpath -tags embed_rg -o filesystem-ultra-v4-embed.exe .

# Or use the build scripts
build-windows.bat        # default
build-windows.sh         # Linux/macOS
```

Requires Go 1.26.5+. No CGO. Tested on Windows 11 and Ubuntu 22.04 (WSL2).

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

Linux:

```json
{
  "mcpServers": {
    "filesystem-ultra": {
      "command": "/path/to/filesystem-ultra",
      "args": [
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "--log-dir", "/home/user/.local/share/mcp-filesystem/logs",
        "/home/user/projects/"
      ]
    }
  }
}
```

The positional arguments after the flags are the allowed base paths. Omitting paths disables access control entirely.

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--compact-mode` | off | Reduced-token responses |
| `--cache-size` | 100MB | In-memory file cache limit |
| `--parallel-ops` | 2×CPU (max 16) | Max concurrent operations |
| `--backup-dir` | system temp | Directory for automatic backups |
| `--backup-max-age` | 72h | Maximum backup retention |
| `--backup-max-count` | 50 | Maximum backup count per file |
| `--risk-threshold-medium` | 20 | % change flagged as medium risk |
| `--risk-threshold-high` | 75 | % change flagged as high risk |
| `--hooks-enabled` | off | Enable pre/post operation hooks |
| `--hooks-config` | — | Path to hooks configuration JSON |
| `--log-dir` | — | Directory for audit logs and metrics (enables logging) |
| `--log-level` | info | Log level: debug, info, warn, error |
| `--debug` | off | Verbose debug logging |

---

## Tool Discovery

Claude Desktop uses **lazy tool loading** — it discovers only a few tools per query via semantic search, missing most of the 20 registered tools.

Three layers address this:

| Layer | How it works | Client support |
|-------|-------------|----------------|
| **`/filesystem-ultra-tools` skill** | Claude Code skill that calls `help` on conversation start | Claude Code |
| **`help` tool** | Generates a catalog from the tools actually registered in the server | Any MCP client |
| **`server.WithInstructions()`** | Identifies the server's real-host filesystem scope and points to `help()` | Spec-compliant clients |

### Using the skill

The skill ships in `.claude/skills/filesystem-ultra-tools/`. In Claude Code or Claude Desktop, invoke:

```
/filesystem-ultra-tools
```

This calls the `help` tool and loads the full catalog. You can also add this to your project instructions:

```
At the start of every conversation, do tool_search for "filesystem help" and then call filesystem-ultra:help()
```

### Host filesystem vs runtime sandbox

`filesystem-ultra:*` tools operate on the host filesystem visible to the MCP server. A client may also expose native tools such as `create_file`, `str_replace`, or `view`; those can run in a separate sandbox even when passed a Windows-looking path.

For a host project, bind the whole task to the filesystem-ultra family. After every creation or edit, verify independently with `get_file_info` or `list_directory`, and use `read_file` when content matters. If a known file reports `File not found`, stop and audit recent operations made through the failing tool family before retrying or switching tools.

---

## Available Tools

### Reading and editing (5)

| Tool | Description |
|------|-------------|
| `read_file` | Read full file, line range (`start_line`/`end_line`), head/tail (`max_lines`+`mode`), or base64 (`encoding:"base64"`) |
| `write_file` | Create or overwrite a file. Supports text (`content`) and binary (`encoding:"base64"`) |
| `edit_file` | Find-and-replace with backup and risk assessment. Modes: exact match (default), `search_replace` (all occurrences), `regex` (capture groups), `occurrence:N` (Nth match) |
| `multi_edit` | Multiple find-and-replace operations on the same file in one call via `edits_json`. v4.5.25+: `diff_format` (auto\|full\|summary\|stat\|none) for the aggregate batch diff |
| `project_replace` | Rename a token across all files in a directory tree (regex or literal) |

### Search and inspection (4)

| Tool | Description |
|------|-------------|
| `list_directory` | Directory listing with cache |
| `search_files` | Search by pattern with optional `file_types`, `include_content`, `include_context`, `case_sensitive`, `count_only` |
| `get_file_info` | Size, permissions, timestamps, type |
| `analyze_operation` | Dry-run preview via `operation`: file, edit, delete, write, optimize, compare |

### File operations (4)

| Tool | Description |
|------|-------------|
| `move_file` | Move or rename file/directory |
| `copy_file` | Recursive copy preserving permissions |
| `delete_file` | Soft-delete (default) or permanent (`permanent: true`) |
| `create_directory` | Create directory tree (`mkdir -p`) |

### Batch and recovery (2)

| Tool | Description |
|------|-------------|
| `batch_operations` | Atomic batch ops (`request_json`), multi-step pipelines (`pipeline_json`), or batch rename (`rename_json`) — with rollback on failure |
| `backup` | Manage backups via `action`: list, info, compare, cleanup, restore |

### Platform and utilities (5)

| Tool | Description |
|------|-------------|
| `wsl` | WSL ↔ Windows sync and status. Params: `wsl_path`/`windows_path` + `direction`, or `action:"status"` |
| `git` | Git operations: `init`, `status`, `diff`, `log`, `show`, `add`, `commit`, `restore`, `branch`. Native-array `paths[]`, `output` enum, `rev` for revisions |
| `minify_js` | Pure-Go JS minification (no Node dependency) |
| `server_info` | Server diagnostics via `action`: stats, help, artifact |
| `help` | Returns the full 20-tool catalog with keywords for lazy discovery |

---

## Dashboard

Separate binary for real-time observability — reads audit logs and serves a web UI.

```bash
# Build
go build -ldflags="-s -w" -trimpath -o dashboard.exe ./cmd/dashboard/

# Run
dashboard.exe --log-dir=C:\logs\mcp-filesystem --backup-dir=C:\backups --port=9100
```

- No coupling with the MCP server (file-based communication only)
- Embedded web assets via `go:embed` (single binary, no dependencies)
- Real-time updates via SSE (Server-Sent Events)
- Pages: Dashboard (metrics), Operations (audit log), Backups (search and recovery), Statistics, Proxy/Tokens, Edit Analysis

### MCP Proxy

Transparent stdio proxy that logs every tool call with timing and token estimates. Sits between Claude Desktop and the MCP server.

```bash
# Build
go build -ldflags="-s -w" -trimpath -o mcp-proxy.exe ./cmd/proxy/

# Claude Desktop config — wrap the MCP server with the proxy
# "command": "mcp-proxy.exe",
# "args": ["--model", "sonnet-4", "--log-dir", "C:\\logs\\mcp-proxy", "--", "filesystem-ultra-v4.exe", ...]
```

Point the dashboard at the proxy logs to see the **Proxy / Tokens** page:

```bash
dashboard.exe --log-dir=C:\logs\mcp-filesystem --proxy-log-dir=C:\logs\mcp-proxy --port=9100
```

See the [docs-website Dashboard & Monitoring guide](docs-website/src/content/docs/features/dashboard.md) for the full setup.

### Audit Logging

When `--log-dir` is set on the MCP server, it writes:

- `operations.jsonl` — JSON Lines audit log (one entry per tool call, auto-rotates at 10MB)
- `metrics.json` — Performance metrics snapshot (updated every 30 seconds)

---

## Architecture

```
main.go                     Entry point — config, CLI flags, server startup
audit.go                    auditWrap — request normalization + audit logging
format.go                   Response formatters, parseSize, truncateContent, formatSize
help_content.go             getHelpContent() — static help text for all topics
tools_core.go               toolRegistry, registerTools, read_file/write_file/edit_file
tools_search.go             list_directory, search_files, analyze_operation
tools_files.go              create_directory, delete_file, move_file, copy_file, get_file_info
tools_batch.go              multi_edit, batch_operations, backup
tools_platform.go           wsl, server_info
tools_aliases.go            Aliases + fs super-tool (disabled), help tool
tools_git.go                git (9 actions: init, status, diff, log, show, add, commit, restore, branch)
tools_minify.go             minify_js (pure-Go JS minification)
core/
  engine.go                 UltraFastEngine — central struct, cache, worker pool, metrics
  edit_operations.go        EditFile, MultiEdit — backup, risk assessment, hooks
  file_operations.go        Rename, SoftDelete, Copy, Move
  streaming_operations.go   StreamingWrite, ChunkedRead, SmartEdit
  search_operations.go      SmartSearch, AdvancedTextSearch
  backup_manager.go         Create, restore, compare, and clean backups
  impact_analyzer.go        Risk assessment (LOW / MEDIUM / HIGH / CRITICAL)
  edit_safety_layer.go      Context validation, stale-edit prevention
  hooks.go                  Pre/post hook system (16 event types)
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
  proxy/
    main.go                 Stdio proxy — logs tool calls, timing, token estimates
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

- `IsPathAllowed()` resolves symlinks via `filepath.EvalSymlinks()` before the containment check — prevents symlink escape from allowed paths
- **Allowed-path root protection** (v4.2.1) — `delete_file`, `soft_delete`, and `move_file` reject the `--allowed-paths` root itself, preventing `os.RemoveAll()` from wiping an entire tree
- Strict parameter validation — unknown params rejected, types enforced (`core/param_validator.go`)
- Temp files and backup IDs use `crypto/rand` (not timestamps)
- Backup IDs are sanitized to `[a-zA-Z0-9_-]` to prevent path traversal
- Temp files and backup metadata written with `0600` permissions
- `copyDirectory()` skips symlinks
- No `unsafe` package usage in production code

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/mark3labs/mcp-go` | v0.54.1 | MCP server SDK |
| `github.com/allegro/bigcache/v3` | v3.1.0 | File content cache |
| `github.com/patrickmn/go-cache` | v2.1.0 | Directory and metadata cache |
| `github.com/panjf2000/ants/v2` | v2.12.1 | Goroutine pool |
| `github.com/fsnotify/fsnotify` | v1.10.1 | File system event watching |

---

## Documentation

Full documentation at **[filesystem.scopweb.com](https://filesystem.scopweb.com)**.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the full version history (latest unreleased: v4.5.31 — security hardening, structured-output conformance, stability tiers).

---

## License

MIT
