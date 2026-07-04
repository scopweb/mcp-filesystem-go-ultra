MCP Filesystem Ultra - Release Notes (v4.5.25)
=============================================

This archive contains pre-built binaries for the MCP Filesystem Server Ultra.

Server exposes 20 MCP tools: 17 core (read_file, write_file, edit_file,
list_directory, search_files, get_file_info, move_file, copy_file,
delete_file, create_directory, batch_operations, backup, analyze_operation,
wsl, server_info, multi_edit, project_replace) + git + minify_js + help.

Aliases and the fs super-tool are DISABLED in v4.5.x — use canonical names.

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
- filesystem-ultra-v4-embed_rg.exe   Main server with embedded ripgrep (best performance)
- filesystem-ultra-v4.exe            Main server (standard)
- mcp-proxy.exe                      Logging proxy (adds --model and request logging)
- dashboard.exe                      Web dashboard for logs, metrics, backups and Trash

NEW IN v4.5.25 (2026-07-04)
---------------------------
- list_directory: new output_format (compact|json|tree) and max_depth
- multi_edit: new diff_format (auto|full|summary|stat|none) with aggregate
  batch diff rendering; dry_run now declared in schema
- Go bumped to 1.26.4 (GO-2026-5039, GO-2026-5037 CVE fixes)

See https://github.com/scopweb/mcp-filesystem-go-ultra/releases/tag/v4.5.25
for the complete changelog.

Build information:
- Compiled with Go 1.26.4
- Optimized (-ldflags="-s -w" -trimpath)
- Output from official build scripts

Need help?
- Open an issue on GitHub
- Check SECURITY.md for security model and best practices
- Documentation: https://github.com/scopweb/mcp-filesystem-go-ultra