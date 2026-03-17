# Dashboard & Proxy Setup

The MCP Filesystem Ultra dashboard is a standalone HTTP binary that provides real-time observability over the MCP server. It includes a **Proxy / Tokens** page for tracking token usage when running behind an MCP proxy.

---

## 1. Enable Audit Logging on the MCP Server

The dashboard reads log files written by the MCP server. Add `--log-dir` to your MCP server config:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\path\\to\\filesystem-ultra-v4.exe",
      "args": [
        "--log-dir", "C:\\logs\\mcp-filesystem",
        "--backup-dir", "C:\\backups\\mcp-filesystem",
        "C:\\your\\project"
      ]
    }
  }
}
```

This produces two files in the log directory:
- `operations.jsonl` — one JSON line per tool call (auto-rotates at 10 MB, keeps last 3)
- `metrics.json` — performance snapshot updated every 30 seconds

---

## 2. Build and Run the Dashboard

```bash
# Build
go build -ldflags="-s -w" -trimpath -o dashboard.exe ./cmd/dashboard/

# Run
dashboard.exe --log-dir=C:\logs\mcp-filesystem --backup-dir=C:\backups\mcp-filesystem --port=9100
```

Open `http://localhost:9100` in your browser.

### Dashboard flags

| Flag | Default | Description |
|------|---------|-------------|
| `--log-dir` | *(required)* | Directory containing MCP server logs |
| `--backup-dir` | — | Directory containing MCP server backups |
| `--proxy-log-dir` | — | Directory containing proxy logs (`proxy.jsonl`) |
| `--port` | 9100 | HTTP port |

### Dashboard pages

| Page | Description |
|------|-------------|
| **Dashboard** | Real-time metrics cards (ops/sec, cache hit rate, avg latency) |
| **Operations** | Audit log table with live SSE updates |
| **Backups** | Search, filter, and restore backups |
| **Statistics** | Aggregated stats, error patterns, request normalizer |
| **Proxy / Tokens** | Token usage by model and tool (requires `--proxy-log-dir`) |
| **Edit Analysis** | Risk distribution and edit patterns |

---

## 3. Proxy / Tokens Page

The **Proxy / Tokens** page visualizes token consumption when the MCP server runs behind a proxy (e.g., `mcp-proxy`). It reads a `proxy.jsonl` file that the proxy writes.

### Expected log format

Each line in `proxy.jsonl` must be a JSON object with these fields:

```json
{
  "ts": "2026-03-17T10:30:00Z",
  "model": "claude-sonnet-4-6",
  "tool": "edit_file",
  "path": "/project/src/main.go",
  "bytes_in": 1234,
  "bytes_out": 5678,
  "tokens_in": 450,
  "tokens_out": 120,
  "duration_ms": 230,
  "status": "ok",
  "error": ""
}
```

### What the page shows

- **Summary cards**: Total tool calls, tokens in/out, total tokens, error rate, time span
- **By Model**: Calls, tokens, errors, average duration per model
- **Tokens by Tool**: Calls, tokens, error rate per MCP tool
- **Token Distribution chart**: Visual bar chart by model

### Setup

1. Configure your MCP proxy to write `proxy.jsonl` to a directory
2. Pass that directory to the dashboard:

```bash
dashboard.exe \
  --log-dir=C:\logs\mcp-filesystem \
  --backup-dir=C:\backups\mcp-filesystem \
  --proxy-log-dir=C:\logs\mcp-proxy \
  --port=9100
```

If `--proxy-log-dir` is not set or the file doesn't exist, the Proxy / Tokens page shows "No proxy data" — no errors are raised.

---

## 4. Windows Batch Script

For convenience, create a `run-dashboard.bat`:

```bat
@echo off
dashboard.exe ^
  --log-dir=C:\logs\mcp-filesystem ^
  --backup-dir=C:\backups\mcp-filesystem ^
  --proxy-log-dir=C:\logs\mcp-proxy ^
  --port=9100
pause
```

---

## 5. Architecture Notes

- The dashboard is a **separate binary** — no runtime coupling with the MCP server
- Communication is file-based only (reads `.jsonl` and `.json` files)
- All web assets are embedded via `go:embed` (single binary, no dependencies)
- Real-time updates use Server-Sent Events (SSE) on the Operations page
- Backup cache has a 30-second TTL to avoid repeated disk scans
