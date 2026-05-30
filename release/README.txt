MCP Filesystem Ultra - Release Notes
======================================

This archive contains pre-built binaries for the MCP Filesystem Server Ultra.

RECOMMENDED SETUP FOR CLAUDE DESKTOP / CLAUDE CODE
--------------------------------------------------

1. Use the logging proxy (strongly recommended):
   - mcp-proxy.exe   (Windows)
   - mcp-proxy       (Linux)

2. Point the proxy at the main server:
   filesystem-ultra-v4-embed_rg.exe   (recommended - includes ripgrep)
   or
   filesystem-ultra-v4.exe

Example Claude Desktop configuration:

{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\path\\to\\mcp-proxy.exe",
      "args": [
        "--model", "opus-4",
        "--log-dir", "C:\\temp\\mcp-proxy-logs",
        "--",
        "C:\\path\\to\\filesystem-ultra-v4-embed_rg.exe",
        "--compact-mode",
        "--log-dir", "C:\\temp\\mcp-ultra-logs",
        "C:\\your\\allowed\\path"
      ]
    }
  }
}

IMPORTANT BINARIES
------------------
- filesystem-ultra-v4-embed_rg.exe   → Main server with embedded ripgrep (best performance)
- filesystem-ultra-v4.exe            → Main server (standard)
- mcp-proxy.exe                      → Logging proxy (adds --model and request logging)
- filesystem-ultra-v4-dashboard.exe  → Web dashboard for logs, metrics and backups

For full documentation:
https://github.com/scopweb/mcp-filesystem-go-ultra

Build information:
- Compiled with Go 1.26.3
- Optimized (-ldflags="-s -w" -trimpath)
- Output from official build scripts

Need help?
- Open an issue on GitHub
- Check SECURITY.md for security model and best practices
