# CLAUDE.md — MCP Proxy

## What This Is

A transparent stdio proxy that sits between an MCP client (Claude Desktop, Claude Code) and the MCP filesystem server. It intercepts JSON-RPC traffic, logs every `tools/call` with timing and token estimates, and writes `proxy.jsonl` for the dashboard's **Proxy / Tokens** page.

## Build

```bash
go build -ldflags="-s -w" -trimpath -o mcp-proxy.exe ./cmd/proxy/
```

Single file: `cmd/proxy/main.go` (~300 lines, zero external dependencies beyond stdlib).

## Usage

```bash
mcp-proxy --model "sonnet-4" --log-dir C:\logs\mcp-proxy -- C:\path\to\filesystem-ultra-v4.exe --compact-mode C:\project
```

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--model` | No | Model name tag for logs (e.g., `opus-4`, `sonnet-4`) |
| `--log-dir` | Yes | Directory where `proxy.jsonl` is written |

Everything after `--` is the target MCP server command + args. The proxy spawns it as a child process.

### Claude Desktop Config

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\path\\to\\mcp-proxy.exe",
      "args": [
        "--model", "sonnet-4",
        "--log-dir", "C:\\logs\\mcp-proxy",
        "--",
        "C:\\path\\to\\filesystem-ultra-v4.exe",
        "--compact-mode",
        "--log-dir", "C:\\logs\\mcp-filesystem",
        "C:\\your\\project"
      ]
    }
  }
}
```

## Architecture

```
Claude Desktop ──stdin──▶ mcp-proxy ──stdin──▶ filesystem-ultra-v4.exe
                ◀─stdout──           ◀─stdout──
                          │
                          ▼
                   proxy.jsonl (JSONL log)
```

### How it works

1. **stdin relay** (goroutine): Reads lines from Claude, forwards to child stdin. If the line is a `tools/call` JSON-RPC request, it records the request ID, tool name, path, and input bytes in a `pending` map.
2. **stdout relay** (main goroutine): Reads lines from child stdout, forwards to Claude. If the line is a JSON-RPC response matching a pending request ID, it calculates duration, output bytes, status (ok/error), and writes the log entry.
3. **stderr**: Child stderr is piped directly to proxy stderr (no interception).

### Token estimation

Tokens are approximated as `bytes / 4` — not exact, but good enough for relative comparisons and trend monitoring. The dashboard aggregates these into per-model and per-tool breakdowns.

## Log Format

`proxy.jsonl` — one JSON line per completed tool call:

```json
{
  "ts": "2026-03-17T10:30:00.123Z",
  "model": "sonnet-4",
  "tool": "edit_file",
  "path": "/project/src/main.go",
  "bytes_in": 1234,
  "bytes_out": 5678,
  "tokens_in": 308,
  "tokens_out": 1419,
  "duration_ms": 230,
  "status": "ok",
  "request_id": "42"
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `ts` | datetime | When the request was sent |
| `model` | string | From `--model` flag (empty if not set) |
| `tool` | string | MCP tool name (e.g., `read_file`, `edit_file`) |
| `path` | string | Extracted from tool arguments `path` field |
| `bytes_in` | int | Serialized argument size |
| `bytes_out` | int | Response line size |
| `tokens_in` | int | Estimated: `bytes_in / 4` |
| `tokens_out` | int | Estimated: `bytes_out / 4` |
| `duration_ms` | int | Round-trip time (request → response) |
| `status` | string | `"ok"` or `"error"` |
| `error` | string | Error message (only if status is error) |
| `request_id` | string | JSON-RPC request ID |

### Rotation

Log rotates at 10 MB. Rotated files: `proxy-YYYYMMDD-HHMMSS.jsonl`. Keeps last 3 rotated files.

## Integration with Dashboard

Point the dashboard at the proxy log dir:

```bash
dashboard.exe --log-dir=C:\logs\mcp-filesystem --proxy-log-dir=C:\logs\mcp-proxy --port=9100
```

The dashboard's **Proxy / Tokens** page reads `proxy.jsonl` and shows:
- Total calls, tokens in/out, error rate, time span
- Breakdown by model (calls, tokens, avg duration)
- Breakdown by tool (calls, tokens, error rate)
- Token distribution bar chart

## Key Implementation Details

- **10 MB scanner buffer**: Both stdin and stdout scanners use 10 MB buffers to handle large JSON-RPC messages (e.g., `read_file` responses with big content).
- **Zero latency impact**: Lines are forwarded immediately before parsing. Logging happens after forwarding.
- **Thread safety**: `pending` map protected by `sync.Mutex`. Logger has its own mutex.
- **Graceful shutdown**: Deferred `logger.Close()`. Child process waited via `cmd.Wait()`.
- **File permissions**: Log file created with `0600`.
