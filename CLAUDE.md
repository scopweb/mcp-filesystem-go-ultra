# CLAUDE.md - Project Context for Claude Code

## Project Overview

MCP Filesystem Server Ultra-Fast (v3.13.2) - A high-performance MCP (Model Context Protocol) filesystem server written in Go, optimized for Claude Desktop and Claude Code. Provides 56 MCP tools for file operations, search, editing, backups, streaming, and WSL/Windows integration.

## Build & Test

```bash
# Build (Windows)
go build -ldflags="-s -w" -trimpath -o filesystem-ultra.exe .

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
```

## Architecture

```
main.go                    # Entry point: CLI flags, 56 MCP tool registrations, server startup
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
cache/
  intelligent.go           # 3-tier cache: BigCache (files) + go-cache (dirs) + go-cache (metadata)
mcp/
  mcp.go                   # MCP type definitions (legacy, mostly unused)
tests/
  mcp_functions_test.go    # Core MCP function tests
  bug5_test.go - bug9_test.go  # Regression tests
  edit_safety_test.go      # Edit safety validation tests
  security/                # Security & fuzzing tests (package: security)
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/mark3labs/mcp-go v0.43.2` | MCP server SDK |
| `github.com/allegro/bigcache/v3 v3.1.0` | High-performance file content cache |
| `github.com/patrickmn/go-cache v2.1.0` | Directory/metadata cache |
| `github.com/panjf2000/ants/v2 v2.11.5` | Goroutine worker pool |
| `github.com/fsnotify/fsnotify v1.9.0` | File system event watching |

Go version: 1.25.7

## Core Patterns

### Access Control
```go
if len(e.config.AllowedPaths) > 0 {
    if !e.isPathAllowed(path) {
        return nil, &PathError{Op: "operation", Path: path, Err: fmt.Errorf("access denied")}
    }
}
```
`isPathAllowed()` resolves symlinks via `filepath.EvalSymlinks()` before checking containment. Empty AllowedPaths = no restrictions.

### Error Handling
Custom error types in `core/errors.go`: `PathError`, `ValidationError`, `CacheError`, `EditError`, `ContextError`. Always wrap with `fmt.Errorf("context: %w", err)`.

### MCP Tool Response
```go
// Success
return mcp.NewToolResultText("result text"), nil

// Error
return mcp.NewToolResultError("error message"), nil
```

### Tool Registration (main.go)
```go
s.AddTool(mcp.NewTool("tool_name",
    mcp.WithDescription("description"),
    mcp.WithString("param", mcp.Description("desc"), mcp.Required()),
), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // implementation
})
```
Parameter extraction: `request.GetArguments()` returns `map[string]interface{}`, use `request.RequireString("param")` or type assertions.

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
Operations that modify > 30% of file or have > 50 occurrences get MEDIUM risk. > 50% or > 100 occurrences = HIGH (requires `force: true`). > 90% = CRITICAL.

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
- `--debug` / `--log-level` - Logging

## Security Notes

- All temp files use `crypto/rand` for names (not predictable timestamps)
- Backup IDs sanitized: only alphanumeric, `-`, `_` allowed
- `isPathAllowed()` resolves symlinks before containment check
- `copyDirectory()` skips symlinks
- Temp files and backup metadata use 0600 permissions
- No `unsafe` package usage in production code
