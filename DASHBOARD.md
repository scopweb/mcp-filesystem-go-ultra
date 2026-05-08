# DASHBOARD.md

Web UI for MCP filesystem-ultra logs, metrics, and backups.

## Run

```bash
dashboard.exe --log-dir=C:\temp\mcp-ultra-logs --proxy-log-dir=C:\temp\mcp-proxy-logs --backup-dir=C:\backups\filesystem-ultra --port=9100
```

Or use `run-dashboard.bat` (preset paths).

## Port

Default: **9100** → http://localhost:9100

Override with `--port=NNNN`.

## Flags

| Flag | Required | Default |
|------|----------|---------|
| `--log-dir` | Yes | — |
| `--proxy-log-dir` | No | — |
| `--backup-dir` | No | — |
| `--port` | No | `9100` |

## Pages

- **Dashboard** — live metrics (ops/sec, errors, latency)
- **Operations** — audit log (`operations.jsonl`) with SSE live feed
- **Backups** — search, filter, grep-inside, restore
- **Statistics** — aggregates per tool / per path
- **Proxy / Tokens** — model + tool breakdowns (needs `--proxy-log-dir`)
- **ROI / Savings** — token savings dashboard

## Troubleshooting

If dashboard doesn't start:
1. Confirm `--log-dir` exists and contains `operations.jsonl` (the MCP server must be run with `--log-dir=` pointing to the same dir).
2. Check port 9100 isn't already taken: `netstat -ano | findstr :9100`.
3. Rebuild: `go build -ldflags="-s -w" -trimpath -o dashboard.exe ./cmd/dashboard/`
