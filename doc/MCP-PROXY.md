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

## Notes

- `model` comes from the `--model` flag (MCP protocol doesn't transmit it).
- `client` is auto-detected from the `initialize` handshake (e.g. `Claude Desktop/0.9.2`).
- Zero latency impact — lines are forwarded before logging.
