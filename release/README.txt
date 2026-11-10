MCP Filesystem Ultra - Binary releases
=====================================

Choose the archive for your operating system and CPU:
- Windows: windows-amd64.zip or windows-arm64.zip
- Linux: linux-amd64.tar.gz or linux-arm64.tar.gz
- macOS: darwin-amd64.tar.gz or darwin-arm64.tar.gz (Apple Silicon)
The archive name also includes the release version. Verify checksums.txt.

BINARIES
--------
- filesystem-ultra-v4       Main server; filesystem-ultra is an identical copy
- filesystem-ultra-v4-embed_rg  Server with embedded ripgrep
- mcp-proxy                 Optional stdio logging proxy
- filesystem-ultra-v4-dashboard  Web dashboard
Windows names end in .exe. Windows arm64 includes the standard server only;
install ripgrep separately for accelerated search (Go search remains available).
Linux arm64 embedded ripgrep uses the upstream GNU/Linux build and needs glibc.

QUICK START
-----------
Minimum configuration is an allowed directory. Recommended product profile:
  filesystem-ultra-v4 --preset=minimal /absolute/path/to/project
Windows:
  filesystem-ultra-v4.exe --preset=minimal C:\path\to\project

Claude Desktop example (adjust the executable and allowed directory):
{
  "mcpServers": {
    "filesystem": {
      "command": "C:\\path\\to\\filesystem-ultra-v4.exe",
      "args": ["--preset=minimal", "C:\\path\\to\\project"]
    }
  }
}
On Linux/macOS use an absolute executable path without .exe.

PROFILES
--------
strict: 18 tools (recommended); agent: strict plus Git; ultra: 34 tools.
Aliases and the fs super-tool are disabled. Use canonical tool names.
Start with list_allowed_directories, then project_map, then the specific tool.

BUILD AND DOCUMENTATION
-----------------------
The release workflow builds with the Go version pinned in go.mod and stamps
commit/date metadata. Run the server with --version to identify the release.
Release history: https://github.com/scopweb/mcp-filesystem-go-ultra/releases
Documentation: https://github.com/scopweb/mcp-filesystem-go-ultra
