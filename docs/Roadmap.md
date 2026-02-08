# Roadmap: MCP Filesystem Ultra

**Last updated:** February 2026
**Current version:** v3.13.2
**Status:** Production-ready, Security Hardened

---

## Current Version: v3.13.2

### Achievements

- **Security Audit and Hardening** - Go toolchain updated to go1.25.7 (8 CVEs resolved)
- **5 CRITICAL security fixes** - Symlink traversal bypass, access control in EditFile, StreamingWriteFile, ChunkedReadFile, SmartEditFile
- **3 HIGH security fixes** - MultiEdit access control, deadlock in ListBackups, path traversal via backup IDs
- **5 MEDIUM security fixes** - Temp files with crypto/rand, secure backup IDs, permissions preserved, symlinks in copyDirectory, metadata 0600
- **56 MCP tools** registered
- **Complete modernization** - Go 1.21+ features, slog logging, custom error types
- **Optimized performance** - 30-40% memory savings, 2-3x speed improvements
- **Zero breaking changes** since v3.11.0

### Metrics

| Metric | Status |
|--------|--------|
| MCP Tools | 56 |
| CVEs resolved (Go toolchain) | 8 |
| Critical vulnerabilities fixed | 5 |
| High vulnerabilities fixed | 3 |
| Medium vulnerabilities fixed | 5 |
| Passing tests | 53+ |
| Regressions | 0 |

---

## Completed Features

The following features are fully implemented and tested:

### Streaming and Large File Support

| Feature | Tool | Status |
|---------|------|--------|
| Streaming writes | `streaming_write_file` | Implemented |
| Chunked reads | `chunked_read_file` | Implemented |
| Large file editing | `smart_edit_file` | Implemented |
| Intelligent auto-selection | `intelligent_read`, `intelligent_write`, `intelligent_edit` | Implemented |
| Large file processor | Internal engine | Implemented with tests |

### Advanced Editing

| Feature | Tool | Status |
|---------|------|--------|
| Recovery editing | `recovery_edit` | Implemented |
| Multi-edit | `multi_edit` | Implemented |
| Regex transformations | `regex_transform_file` | Implemented with tests |
| Batch operations | `batch_operations` | Implemented |
| Batch renaming | `batch_rename_files` | Implemented with preview mode |

### Analysis and Telemetry

| Feature | Tool | Status |
|---------|------|--------|
| File analysis | `analyze_file` | Implemented |
| Write analysis | `analyze_write` | Implemented |
| Edit analysis | `analyze_edit` | Implemented |
| Delete analysis | `analyze_delete` | Implemented |
| Edit telemetry | `get_edit_telemetry` | Implemented |
| Optimization suggestions | `get_optimization_suggestion` | Implemented |
| Performance stats | `performance_stats` | Implemented |

### Backup System

| Feature | Tool | Status |
|---------|------|--------|
| List backups | `list_backups` | Implemented |
| Restore with preview | `restore_backup` | Implemented with preview mode |
| Compare with backup | `compare_with_backup` | Implemented |
| Cleanup backups | `cleanup_backups` | Implemented |
| Backup info | `get_backup_info` | Implemented |

### WSL Integration

| Feature | Tool | Status |
|---------|------|--------|
| WSL to Windows copy | `wsl_to_windows_copy` | Implemented |
| Windows to WSL copy | `windows_to_wsl_copy` | Implemented |
| Workspace sync | `sync_claude_workspace` | Implemented |
| Status check | `wsl_windows_status` | Implemented |
| Auto-sync config | `configure_autosync`, `autosync_status` | Implemented |

---

## Recent Version History

### v3.11.0 (2025-12-21): "Performance & Modernization"
- Custom error types (PathError, ValidationError, CacheError, EditError, ContextError)
- Context cancellation in search operations
- Environment detection caching (5min TTL)
- Buffer pool helper, BigCache config fix, regex compilation cache
- bufio.Scanner for 30-40% memory savings on large files
- Go 1.21+ built-in min/max, slog structured logging
- **47 passing tests**

### v3.12.0 (Partial): "Code Editing Excellence"
- **Phase 1 completed**: Coordinate tracking in search results
  - `calculateCharacterOffset()` with `regexp.FindStringIndex()`
  - Coordinates in `performSmartSearch()` and `performAdvancedTextSearch()`
  - Bug #2 fix: multiple occurrences on same line
  - 7 new tests (53 total)
- **Phases 2-4 pending** (see next section)

### v3.13.0 (2026-01-31): "Security Audit & Hardening"
- Go toolchain go1.24.6 -> go1.24.12 (8 CVEs)
- 13 security fixes (5 CRITICAL + 3 HIGH + 5 MEDIUM)
- Symlink resolution before containment checks
- crypto/rand for all temp files and backup IDs
- File permissions preserved on write operations
- Build fix in tests/security/

### v3.13.2 (2026-02): "Performance & Extensions"
- Go toolchain updated to go1.25.7
- Optimized isTextFile() function
- Added 70+ file extensions support
- Tests for large_file_processor and regex_transformer

---

## In Progress: v3.14.0

### Theme: "Code Editing Excellence Completion"

The coordinate tracking (Phase 1) was completed in v3.12.0. Remaining phases:

### Phase 2: Diff-Based Editing (Planned)

New tool `apply_diff_patch`:
- Send only changes (2KB) instead of full content (500KB)
- Use coordinates from Phase 1 for precise targeting
- **Estimated impact:** 60% token reduction
- Approximately 300 lines of code

### Phase 3: Preview Mode (Partially Complete)

Preview mode (`preview: true` or `dry_run: true` parameter):

| Tool | Preview Mode |
|------|--------------|
| `restore_backup` | Implemented |
| `batch_rename_files` | Implemented |
| `regex_transform_file` | Implemented (via `dry_run`) |
| `intelligent_write` | Planned |
| `smart_edit_file` | Planned |

Remaining work: Add preview mode to `intelligent_write` and `smart_edit_file`.

### Phase 4: High-Level Tools (Planned)

Wrapper functions for common tasks:
- `find_and_replace(path, find, replace, scope)`
- `replace_function_body(path, name, newBody)`
- `rename_symbol(path, oldName, newName, scope)`
- **Estimated impact:** 30% workflow simplification
- Approximately 150 lines of code

### v3.14.0 Summary

| Aspect | Details |
|--------|---------|
| **Breaking Changes** | None |
| **Risk** | LOW (incremental improvements) |
| **Token Impact** | 60-80% reduction in editing workflows |

---

## Future Roadmap

### v3.15.0: "Advanced Search"

Enhanced search capabilities:
- [ ] Full-text search with indexes
- [ ] AST-aware code search (functions, variables, types)
- [ ] Optimized multi-file search
- [ ] Complete Phase 4 high-level tools

---

### v3.16.0: "AI-Assisted Refactoring"

Intelligent refactoring:
- [ ] Code pattern detection and rewrite
- [ ] Namespace/package renaming
- [ ] Automatic import optimization
- [ ] Dependency graph analysis

---

### v4.0.0: "Enterprise Grade"

Enterprise features:
- [ ] Role-based access control (RBAC)
- [ ] Audit logging with compliance
- [ ] Encryption at rest and in transit
- [ ] Multi-user concurrent access
- [ ] File versioning and merge conflict resolution

---

## Known Issues

| Issue | Description | Severity |
|-------|-------------|----------|
| `create_backup` not exposed | The `CreateBackup` function is public but not exposed as an independent MCP tool. Backups are created automatically during edit operations. | LOW (functionality available indirectly) |

---

## Evolution Metrics

### MCP Tools

```
v2.0.0  ████░░░░░░ (32 tools)
v3.1.0  █████░░░░░ (36 tools)
v3.4.0  ██████░░░░ (45 tools)
v3.7.0  ███████░░░ (50 tools)
v3.8.0  ████████░░ (55 tools)
v3.13.2 ████████░░ (56 tools) <- Current
```

### Security

```
v3.8.0  ████░░░░░░ (Risk assessment, backups)
v3.9.0  ██████░░░░ (Security tests, OWASP, fuzzing)
v3.10.0 ███████░░░ (Edit safety layer)
v3.13.0 ██████████ (Full security audit, 13 fixes, crypto/rand)
```

### Performance

```
v3.5.0  ██████░░░░ (Memory-efficient I/O, buffer pools)
v3.6.0  ███████░░░ (multi_edit, cache prefetching)
v3.11.0 █████████░ (bufio.Scanner, regex cache, slog)
v3.13.2 █████████░ (Maintained, no regressions)
```

### Test Coverage

```
v3.8.0  ██░░░░░░░░ (~18%)
v3.11.0 ██░░░░░░░░ (~18%)
v3.12.0 ███░░░░░░░ (~22%, +7 coordinate tests)
v3.13.2 ████░░░░░░ (~25%, large_file + regex tests)
```

---

## Architectural Decisions

### Maintain

- MCP protocol 2025-11-25 compliant (mcp-go v0.43.2)
- Tools capability with listChanged notifications
- BigCache for file content caching (3-tier cache)
- Buffer pools (sync.Pool) for memory efficiency
- Windows/WSL compatibility layer
- ants/v2 worker pool for concurrency
- crypto/rand for secure ID generation

### Improved (Completed)

- Custom error types with Go 1.13+ wrapping (v3.11.0)
- slog structured logging (v3.11.0)
- Go 1.21 built-in functions (v3.11.0)
- Full security audit with 13 fixes (v3.13.0)
- crypto/rand for all temp files (v3.13.0)
- Tests for large file processor and regex transformer (v3.13.2)
- MCP 2025-11-25 specification compliance verified (v3.13.2)

### Pending Improvements

- Search operations (add structure awareness, AST)
- Edit operations (add diff-based approach)
- Test coverage (increase from ~25% to 50%+)
- Tool titles with `WithTitleAnnotation()` for better UI display (optional, MCP 2025-11-25)
- Output schemas for structured tool results (optional, MCP 2025-11-25)

---

## Release Schedule

| Version | Status | Theme |
|---------|--------|-------|
| v3.11.0 | Released (2025-12-21) | Performance and Modernization |
| v3.12.0 | Partial (Phase 1 completed) | Code Editing Excellence |
| v3.13.0 | Released (2026-01-31) | Security Audit and Hardening |
| v3.13.2 | Released (2026-02) | Performance optimization, extended file extensions |
| v3.14.0 | Next | Complete Code Editing |
| v3.15.0 | Planned | Advanced Search |
| v3.16.0 | Planned | AI-Assisted Refactoring |
| v4.0.0 | Vision | Enterprise Grade |

**Note:** No estimated dates for future versions. Quality over speed.

---

## How to Contribute

1. Report issues/ideas as GitHub issues
2. Review the list of known issues above
3. Feature requests with: specific use case, expected impact, estimated effort, risk assessment
4. Security issues through Security Policy

---

**Last Updated:** February 2026
**Maintained By:** David Prats

---

*This roadmap is a living guide that evolves with the project. Significant changes will be communicated in CHANGELOG.md*
