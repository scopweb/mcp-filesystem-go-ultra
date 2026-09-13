# MCP Filesystem Server Ultra

**v4.6.2** · Go 1.27.1 · MCP 2025-11-25 · 25 tools ultra / 16 strict (agent core)

A [Model Context Protocol](https://modelcontextprotocol.io) filesystem server written in Go, designed for **safe file editing by AI agents**: automatic backups with step-through undo, optimistic concurrency to detect external file changes, an accidental-rewrite guard, strict path security, and risk assessment on every mutation. Built for Claude Desktop, Claude Code, and OpenCode, with support for large files, WSL/Windows interoperability, and token-efficient responses.

Legacy aliases (`read_text_file`, `View`, `Edit`, etc.) and the `fs` super-tool are disabled. Default `--profile=ultra` registers all 25 tools; `--profile=strict` registers the 16-tool agent core (includes `backup` for undo; no `analyze_code`, no `git` network).

---

## Quick start

```bash
# 1. Build
go build -ldflags="-s -w" -trimpath -o filesystem-ultra ./cmd/filesystem-ultra

# 2. Run (agent-optimized; required: at least one path)
./filesystem-ultra --profile=strict --compact-mode --roots-mode=union /path/to/project

# 3. Tests (core + server)
go test ./core/ ./internal/mcpserver/ -count=1
```

See [Build](#build) and [Configuration](#configuration) below for more.

---

## Features

### Safety and correctness

- **Automatic backups with step-through undo** — every mutation is recoverable: `backup(action:"undo_last")` walks the chain; `restore` returns a file to its pre-edit bytes
- **Optimistic concurrency (OCC)** — `content_hash`/`expected_hash` chaining detects external file changes between read and edit; `--auto-occ` warns or blocks on stale edits
- **Accidental-rewrite guard** (v4.5.10) — blocks `edit_file` calls that look like unintended full-file rewrites
- **Path security** — symlink-resolved containment via `filepath.Rel`, NTFS ADS blocking, RTLO/zero-width Unicode rejection, Windows reserved names, TOCTOU symlink defense
- **Risk assessment** — mutations above configurable thresholds are flagged (20% change = MEDIUM, 75% = HIGH by default); HIGH/CRITICAL results include post-edit integrity verification
- **Access control** — fail-closed: at least one `--allowed-paths` / positional root is required (also enforced in batch operations). `--insecure-open` is labs-only.
- **Plan mode** — dry-run analysis with diff preview and risk report before applying changes
- **Structured output** — `outputSchema` + `structuredContent` on read/write/edit/multi_edit plus `search_files`, `list_directory`, `batch_operations`, and `backup`. Status (`applied`/`simulated`/`empty`/`partial`), truncation, and hashes are machine-readable. Errors use a JSON envelope with `retryable` (do not auto-retry when false). Text fallbacks stay byte-identical. Handler-level sweep in CI.
- **Native tool inputs** — arrays/objects for `paths`, `edits`, `patterns`, `request`/`pipeline`/`rename`; legacy `*_json` strings stay as adapters. Conflicting dual forms and invalid enums are rejected
- **Optional strict match** — `strict` + `expected_matches` on `edit_file`/`multi_edit`; mismatch lists candidate line numbers and `match_method`
- **Unified tool contract** — one `ToolContract` drives validation, `help(tool:)` examples, and `--readonly` mutation gating; CI asserts MCP wire types, enums and array `items` match the contract

### Productivity

- **25 tools (ultra)** — 17 core + `git` + `minify_js` + `analyze_code` + `help` + discovery/patch. `--profile=strict` registers the 16-tool agent core (includes `backup` for undo; no `analyze_code`).
- **MCP spec-compliant annotations** — `readOnlyHint`, `destructiveHint`, `idempotentHint` on every tool
- **Hook system** — 16 pre/post events (write, edit, delete, create, move, copy, read, search)
- **Pipeline system** — 12 actions with conditions, templates, and DAG-based parallel execution; reduces client/server round-trips for multi-step refactors
- **Atomic batch operations** — grouped file operations with journal rollback (`complete`/`partial`/`failed`); recover-on-error in-process, not crash-durable. Opt-in retries: `operation_id` + `retry_contract:"e3-v1"`
- **Compact mode** — reduced-token responses for high-volume sessions
- **Log tail without bash** — `read_file(mode:"tail", max_lines:40)` replaces `tail | cut`; each line auto-cut to 300 chars (`max_line_length` to override)
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
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe ./cmd/filesystem-ultra

# With ripgrep embedded (~4MB larger)
go build -ldflags="-s -w" -trimpath -tags embed_rg -o filesystem-ultra-v4-embed.exe ./cmd/filesystem-ultra

# Or use the build scripts
build-windows.bat        # default
build-windows.sh         # Linux/macOS
```

Requires Go 1.27.1+. No CGO. Tested on Windows 11 and Ubuntu 22.04 (WSL2).

```bash
# Run tests
go test ./tests/... ./core/... ./internal/mcpserver/

# With race detector
go test -race ./...

# Security fuzzing
go test -fuzz=Fuzz ./tests/security
```

---

## Configuration

### Recommended flags for Claude Code / OpenCode

Keep this trio in the MCP `args` (and leave `--readonly` off so the agent can write):

```
--profile=strict --compact-mode --roots-mode=union
```

| Flag | Why |
|------|-----|
| `--profile=strict` | Registers the 16-tool agent core (`list_allowed_directories`, `directory_tree`, `read_file` / `write_file` / `edit_file` / `apply_patch`, `backup` for undo, …). Omits `git`, `wsl`, `minify_js`, `analyze_code`, `batch_operations`, `copy_file`, `server_info`, … so `tools/list` stays small and lazy-loading clients actually see the useful set. Default is `ultra` (25 tools) so existing configs do not break. |
| `--compact-mode` | Short token-efficient responses (hashes, UNDO ids, no emoji walls). |
| `--roots-mode=union` | MCP client Roots are **added** to the CLI allowlist. Default `replace` is wrong for OpenCode: it sends the workspace as Roots and **wipes** every other CLI path, so `list_allowed_directories` only shows one folder. |

Do not pass `--git-network` unless the agent must `git push` / `git fetch`. Do not pass `--readonly` unless the session should be read-only.

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\path\\to\\filesystem-ultra-v4.exe",
      "args": [
        "--profile", "strict",
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "--log-dir", "C:\\logs\\mcp-filesystem",
        "--roots-mode", "union",
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
        "--profile", "strict",
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "--log-dir", "/home/user/.local/share/mcp-filesystem/logs",
        "--roots-mode", "union",
        "/home/user/projects/"
      ]
    }
  }
}
```

### OpenCode

OpenCode does **not** read `claude_desktop_config.json`. Put this in the project `opencode.json` / `opencode.jsonc`, or globally in `~/.config/opencode/opencode.json` (Windows: `%USERPROFILE%\.config\opencode\opencode.json`).

Differences vs Claude: key is `mcp` (not `mcpServers`), `type` is required, `command` is **one array** (binary + flags + paths), and `timeout` defaults to **5s** — too short for `tools/list`. Set `30000`. Recommended: `--profile=strict --compact-mode --roots-mode=union` (`--readonly` off).

Windows (`opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "filesystem-ultra": {
      "type": "local",
      "command": [
        "C:\\path\\to\\filesystem-ultra-v4.exe",
        "--profile", "strict",
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "--log-dir", "C:\\logs\\mcp-filesystem",
        "--roots-mode", "union",
        "C:\\your\\project\\"
      ],
      "enabled": true,
      "timeout": 30000
    }
  }
}
```

Linux:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "filesystem-ultra": {
      "type": "local",
      "command": [
        "/path/to/filesystem-ultra",
        "--profile", "strict",
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "--log-dir", "/home/user/.local/share/mcp-filesystem/logs",
        "--roots-mode", "union",
        "/home/user/projects/"
      ],
      "enabled": true,
      "timeout": 30000
    }
  }
}
```

Quit and restart OpenCode after saving.

**`--roots-mode` for OpenCode.** Default is `replace`: OpenCode sends the current workspace as MCP Roots and **replaces** the entire CLI allowlist with that one folder. A config with many paths will look correct on disk, but `list_allowed_directories` will only show the workspace.

| Mode | Effect |
|------|--------|
| `replace` (default) | Client Roots replace the CLI list. OpenCode → only the workspace. |
| `union` | CLI paths **plus** the workspace. Use this when the allowlist must survive. |
| `ignore` | CLI list only; client Roots are ignored. |

Allowed paths: positional args after the flags, **or** one `--allowed-paths` with comma-separated values. Do **not** repeat `--allowed-paths=` (Go `flag.String` keeps the last value only; that pattern is for filesystem-ultra-rust / clap). **Required** since v4.6.0 (fail-closed). Omitting them exits 2. Labs only: `--insecure-open` disables the sandbox (entire disk).

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--allowed-paths` | (required) | Comma-separated allowed roots; or pass paths as positional args |
| `--insecure-open` | off | Labs only: disable the sandbox (entire disk). Fail-closed by default since v4.6.0. |
| `--roots-mode` | replace | How MCP client Roots combine with CLI paths: `replace`, `union`, `ignore` |
| `--profile` | ultra | `ultra` = all 25 tools; `strict` = 16-tool agent core (includes `backup`) |
| `--git-network` | off | Enable `git` push/fetch. Off the critical path; ignored in `strict` |
| `--readonly` | off | Reject mutating tools |
| `--allow-secrets` | off | Allow `.env` / keys (audited) |
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

Do **not** dump the full catalog at startup. Handshake instructions are short. First call `list_allowed_directories`, then `directory_tree` or `help(tool:"X")`.

| Step | Call | When |
|------|------|------|
| 1 | `list_allowed_directories` | Before the first read |
| 2 | `directory_tree` or `help(tool:"X")` | Explore roots, or look up one tool on demand |
| 3 | `help()` | Only if you need the live registered list |

`--profile=strict` shrinks `tools/list` to the 16-tool agent core so lazy-loading clients see the useful set.

### Using the skill

The skill ships in `.claude/skills/filesystem-ultra-tools/`. Same order: `list_allowed_directories` first, then `directory_tree` / `help(tool:X)`.

### Host filesystem vs runtime sandbox

`filesystem-ultra:*` tools operate on the host filesystem visible to the MCP server. A client may also expose native tools such as `create_file`, `str_replace`, or `view`; those can run in a separate sandbox even when passed a Windows-looking path.

For a host project, bind the whole task to the filesystem-ultra family. After every creation or edit, verify independently with `get_file_info` or `list_directory`, and use `read_file` when content matters. If a known file reports `File not found`, stop and audit recent operations made through the failing tool family before retrying or switching tools.

### Subagentes y arneses

Copy [examples/harness/](examples/harness/) — do not guess flags. One MCP instance per workspace: `--profile=strict --roots-mode=union --compact-mode`. OpenCode `timeout` ≥ 30000. One role = one disk family (never mix native Read/Edit with ultra). Codex `*** Begin Patch` is not ultra `apply_patch`. Parent→child handoff: roots, path, `content_hash`, `backup_id`, profile; the child re-reads.

---

## Available Tools

25 in `--profile=ultra` (default). `--profile=strict` keeps the 16 marked **strict**.

| Tool | Purpose | When to use |
|------|---------|-------------|
| `list_allowed_directories` | Sandbox roots. Zero parameters. **strict** | Before the first read |
| `directory_tree` | Compact recursive tree. `respect_ignore=true`, `max_depth=2`, `max_nodes=500`. **strict** | After roots, to explore. `truncated` + `hidden_count` in structured output |
| `diff_files` | Unified diff of two paths, or `against:"backup"`. **strict** | Preview before `apply_patch` or to compare two files |
| `apply_patch` | One-file unified diff. `dry_run` + `expected_hash`. Dest EOL wins. **strict** | Surgical one-file edit. If `PATCH_APPLY_FAILED`: `read_file` and regenerate; do not retry the same patch |
| `read_file` | Full / range / head / tail / base64 / `paths[]`. Replaces bash `cat`/`head`/`tail`/`cut`. **strict** | If a client asks for `read_multiple_files` or `read_text_file`, use this |
| `write_file` | Create or overwrite. `mode:"append"` skips rewrite-guard. **strict** | New files or whole-file rewrite |
| `edit_file` | Exact / regex / range / insert. Backup + OCC. **strict** | Targeted edits (`allow_rewrite` not `force` for rewrite-guard) |
| `multi_edit` | Multiple replacements on one file. **strict** | Several anchors in the same file |
| `list_directory` | Directory listing. **strict** | Copy exact paths (case) before edits |
| `search_files` | Name or content. Gitignore ON (`no_ignore=false`). **strict** | Find, then edit. `truncated` + `hidden_count` |
| `get_file_info` | Size, dates, type. Batch `paths[]`. **strict** | Verify after a host mutation |
| `create_directory` | `mkdir -p`. **strict** | New folders |
| `move_file` | Move or rename. **strict** | Relocate |
| `delete_file` | Soft-delete default; `permanent:true` hard. **strict** | Remove |
| `help` | Live catalog; `help(tool:"X")` schema + examples. **strict** | On demand — not at startup |
| `copy_file` | Recursive copy | ultra |
| `project_replace` | Token rename across a tree | ultra |
| `batch_operations` | Atomic ops / pipelines / rename | ultra |
| `backup` | Undo / restore / trash. **strict** | After a bad edit: `undo_last` / `restore` / `restore_trash` |
| `analyze_operation` | Dry-run risk preview | ultra |
| `wsl` | WSL ↔ Windows sync | ultra |
| `git` | Local git. `push`/`fetch` need `--git-network` | ultra |
| `minify_js` | Pure-Go JS minify (no Node) | ultra |
| `analyze_code` | Read-only symbols / lint / sec / impact. Not a writer. | ultra — understand code; edit with `apply_patch`/`edit_file`. Do not use git grep or bash |
| `server_info` | Stats / static help / artifacts | ultra |

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
cmd/
  filesystem-ultra/
    main.go                 Process entry point — calls mcpserver.Run()
  proxy/
    main.go                 Stdio proxy — logs tool calls, timing, token estimates
  dashboard/
    main.go                 HTTP dashboard for logs/metrics/backups
    static/                 Embedded web UI (go:embed)
internal/mcpserver/         MCP server: config, tool registration, stdio loop
                            (tests live here too, next to the code they drive)
  run.go                    Run() — config, CLI flags, server startup
  audit.go                  auditWrap — request normalization + audit logging
  format.go                 Response formatters, parseSize, truncateContent, formatSize
  help_content.go           getHelpContent() — static help text for all topics
  tools_core.go             read_file, write_file, edit_file (the 3 core mutators)
  tools_search.go           list_directory, directory_tree, search_files, analyze_operation
  tools_files.go            create_directory, delete_file, move_file, copy_file, get_file_info
  tools_batch.go            multi_edit, batch_operations, backup, project_replace
  tools_platform.go         wsl, server_info
  tools_discovery.go        list_allowed_directories
  tools_patch.go            diff_files, apply_patch
  tools_git.go              git (11 actions; push/fetch gated by --git-network)
  tools_minify.go           minify_js (pure-Go)
  tools_aliases.go          help (on-demand; legacy aliases disabled)
  tools_analyze.go          analyze_code (ultra only: symbols|lint|sec|impact)
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
tests/                      Black-box suite over the exported core API
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
| `github.com/mark3labs/mcp-go` | v1.0.0 | MCP server SDK |
| `github.com/allegro/bigcache/v3` | v3.2.0 | File content cache |
| `github.com/patrickmn/go-cache` | v2.1.0 | Directory and metadata cache |
| `github.com/panjf2000/ants/v2` | v2.12.1 | Goroutine pool |
| `github.com/fsnotify/fsnotify` | v1.10.1 | File system event watching |

---

## Documentation

Full documentation at **[filesystem.scopweb.com](https://filesystem.scopweb.com)**.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the full version history (latest: v4.6.2 — agent discovery, `--profile=strict`, `--git-network`). Remaining work: [PLAN-PENDIENTE.md](PLAN-PENDIENTE.md).

---

## License

MIT
