# Claude Desktop Setup Guide

**Version:** 4.1.2

## Configuration File Location

**Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

## Recommended Configuration

```json
{
  "mcpServers": {
    "filesystem-ultra": {
      "command": "C:\\path\\to\\filesystem-ultra-v4.exe",
      "args": [
        "--compact-mode",
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "error",
        "--log-dir", "C:\\logs\\mcp-filesystem",
        "C:\\your\\project\\",
        "C:\\other\\allowed\\path\\"
      ]
    }
  }
}
```

The positional arguments at the end are **allowed paths**. Only these directories (and their children) will be accessible. Omitting paths disables access control entirely.

---

## Key Parameters

### Token Optimization

| Parameter | Description | Token Savings |
|-----------|-------------|---------------|
| `--compact-mode` | Minimal responses | **65-75%** |

### Performance

| Parameter | Default | Description |
|-----------|---------|-------------|
| `--cache-size` | 100MB | In-memory file cache |
| `--parallel-ops` | 2x CPU (max 16) | Concurrent operations |
| `--log-level` | info | Log verbosity: debug, info, warn, error |

### Backup & Safety

| Parameter | Default | Description |
|-----------|---------|-------------|
| `--backup-dir` | system temp | Where backups are stored |
| `--backup-max-age` | 72h | Retention period |
| `--backup-max-count` | 50 | Max backups per file |
| `--risk-threshold-medium` | 30 | % change that triggers warning |
| `--risk-threshold-high` | 50 | % change that requires `force: true` |

### Hooks

| Parameter | Description |
|-----------|-------------|
| `--hooks-enabled` | Enable pre/post operation hooks |
| `--hooks-config` | Path to hooks configuration JSON |

### Audit Logging

| Parameter | Description |
|-----------|-------------|
| `--log-dir` | Directory for audit logs + metrics (enables operation logging) |

---

## Configuration Profiles

### A: Ultra-Optimized (Recommended for high-volume sessions)
```json
{
  "args": [
    "--compact-mode",
    "--cache-size", "200MB",
    "--parallel-ops", "8",
    "--log-level", "error"
  ]
}
```
- Minimal token usage
- Maximum speed
- Compact responses

### B: Balanced (General use)
```json
{
  "args": [
    "--compact-mode",
    "--cache-size", "200MB",
    "--parallel-ops", "8",
    "--log-level", "info"
  ]
}
```
- Good token savings (50-60%)
- More diagnostic information

### C: Verbose (Debugging)
```json
{
  "args": [
    "--cache-size", "200MB",
    "--parallel-ops", "8",
    "--log-level", "debug"
  ]
}
```
- Full detail in responses
- Higher token usage

---

## Token Savings Examples

### Directory Listing

**Without compact-mode (~350 tokens):**
```
Directory listing for: C:\project\src
[DIR]  components (0 bytes)
[FILE] index.js (1024 bytes)
[FILE] app.js (2048 bytes)
```

**With compact-mode (~50 tokens):**
```
C:\project\src: components/, index.js(1KB), app.js(2KB)
```

### Search Results

**Without compact-mode (~8,000 tokens):**
```
Found 127 matches for pattern 'TODO':
C:\project\src\file1.js:42
   // TODO: implement feature
   Context: ...
```

**With compact-mode (~150 tokens):**
```
127 matches (first 20): file1.js:42, file2.js:15, ... (107 more)
```

### Session Total (100 operations)

| Mode | Tokens | Savings |
|------|--------|---------|
| Verbose | ~81,000 | — |
| Compact | ~5,900 | **92.7%** |

---

## Backup Configuration

To enable the backup system with a custom directory:

```json
{
  "args": [
    "--compact-mode",
    "--backup-dir", "C:\\backups\\mcp",
    "--backup-max-age", "14",
    "--backup-max-count", "200",
    "C:\\your\\project",
    "C:\\backups\\mcp"
  ]
}
```

**Important:** Include the backup directory in the allowed paths.

---

## Dashboard Setup

Build and run the dashboard binary for real-time observability:

```bash
go build -ldflags="-s -w" -trimpath -o dashboard.exe ./cmd/dashboard/
dashboard.exe --log-dir=C:\logs\mcp-filesystem --backup-dir=C:\backups\mcp --port=9100
```

Requires `--log-dir` to be set on the MCP server.

---

## Troubleshooting

### Claude shows long responses
Verify `--compact-mode` is in the args without JSON syntax errors.

### Cannot find the executable
Verify the full path in `command`. Use double backslashes `\\` on Windows.

### Access denied to files
Add the required paths as positional arguments at the end of `args`.

### Responses too short, missing information
Remove `--compact-mode` to return to verbose mode.

---

*Version: 4.1.2 | Updated: 2026-03-17*
