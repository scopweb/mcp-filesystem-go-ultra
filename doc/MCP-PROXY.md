# MCP-PROXY.md

Transparent stdio proxy between an MCP client (Claude Desktop / Code) and `filesystem-ultra-v4.exe`. Logs every `tools/call` to `proxy.jsonl` for the dashboard's **Proxy / Tokens** page.

## Run

```bash
mcp-proxy.exe --model sonnet-4 --log-dir C:\temp\mcp-proxy-logs -- C:\MCPs\clone\mcp-filesystem-go-ultra\filesystem-ultra-v4.exe --compact-mode C:\project
```

Everything after `--` is the target MCP server command.

## Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--model` | No | Model tag for logs (e.g. `opus-4`, `sonnet-4`) |
| `--log-dir` | Yes | Directory where `proxy.jsonl` is written |
| `--call-timeout` | No | Per `tools/call` timeout (default `60s`). On expiry the client gets an MCP `isError`; late child replies are swallowed. `0` waits forever. |
| `--timeout` | No | Kill the child if this long passes with any request still pending (default `0` = off). Distinct from `--call-timeout`. |
| `--reap-stale` | No | On start, kill old-hash and orphaned processes of the same logical server (default `true`). Disable with `--reap-stale=false`. |

## Claude Desktop config

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\MCPs\\clone\\mcp-filesystem-go-ultra\\mcp-proxy.exe",
      "args": [
        "--model", "sonnet-4",
        "--log-dir", "C:\\temp\\mcp-proxy-logs",
        "--",
        "C:\\MCPs\\clone\\mcp-filesystem-go-ultra\\filesystem-ultra-v4.exe",
        "--compact-mode",
        "--log-dir", "C:\\temp\\mcp-ultra-logs",
        "C:\\your\\project"
      ]
    }
  }
}
```

## Log format (`proxy.jsonl`)

One JSON line per call: `ts`, `model`, `client`, `tool`, `path`, `bytes_in/out`, `tokens_in/out` (≈ bytes/4), `duration_ms`, `status`, `request_id`.

Rotates at 10 MB, keeps last 3.

## Child lifecycle

1:1 stdio relay — not a pool and not round-robin. Two living copies of the same hash mean two MCP clients (Desktop + OpenCode), not replicas. No per-tool routing (`apply_patch` is forwarded like every other call).

On Windows the child is assigned to a Job Object (`KILL_ON_JOB_CLOSE`) so it dies with the proxy. On start, `--reap-stale` terminates other builds of the same logical server and same-hash orphans (parent already dead). Orphans holding IPC path locks are why `apply_patch` could hang through the proxy while `server_info` still answered.

### Redeploy checklist

1. Build the new hashed binary (e.g. `filesystem-ultra-<gitsha>.exe`).
2. Point Claude Desktop / OpenCode args at that path (keep `mcp-proxy.exe` as `command`).
3. Restart the MCP connection.
4. `tasklist /FI "IMAGENAME eq filesystem-ultra*"` — only the new hash should remain.
5. Repeat a second rebuild/restart and confirm again.
6. `apply_patch` with a mismatching hunk must return `PATCH_FAILED` in well under 1s.

## Notes

- `model` comes from the `--model` flag (MCP protocol doesn't transmit it).
- `client` is auto-detected from the `initialize` handshake (e.g. `Claude Desktop/0.9.2`).
- Zero latency impact — lines are forwarded before logging.
