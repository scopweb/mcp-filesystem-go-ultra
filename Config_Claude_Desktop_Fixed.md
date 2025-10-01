# MCP Filesystem Ultra-Fast Configuration

Add this to your Claude Desktop config at:
`C:\Users\David\AppData\Roaming\Claude\claude_desktop_config.json`

```json
"filesystem-enhanced-ultra": {
  "command": "C:\\MCPs\\clone\\mcp-filesystem-go-ultra\\mcp-filesystem-ultra.exe",
  "args": [
    "--cache-size", "500MB",
    "--parallel-ops", "16", 
    "--binary-threshold", "2MB",
    "--log-level", "error",
    "--allowed-paths", "C:\\MCPs\\clone\\,C:\\temp\\,C:\\__REPOS\\jotajotape\\ENCUESTA\\,C:\\__REPOS\\jotajotape\\TRANSFER\\,C:\\__REPOS\\jotajotape\\NEWS\\",
    "--vscode-api"
  ],
  "env": {
    "NODE_ENV": "production"
  }
}
```

## Build Instructions

1. **Run the build script:**
   ```cmd
   cd C:\MCPs\clone\mcp-filesystem-go-ultra
   build.bat
   ```

2. **Update Claude Desktop config** with the JSON above

3. **Restart Claude Desktop completely**
   - Close Claude Desktop
   - Kill any zombie processes: `taskkill /f /im Claude.exe`
   - Open Claude Desktop again

4. **Test the server:**
   - Ask Claude to list files in C:\temp\
   - Try reading/writing files
   - Check performance stats

## Tools Available

- `read_file` - Ultra-fast file reading with caching
- `write_file` - Atomic file writing with backup
- `list_directory` - Directory listing with intelligent caching  
- `edit_file` - Smart file editing like Cline
- `performance_stats` - Real-time performance metrics

## Troubleshooting

If the server fails to start:

1. Check the Claude Desktop logs for error messages
2. Verify the executable exists: `dir mcp-filesystem-ultra.exe`
3. Test manually: `mcp-filesystem-ultra.exe --version`
4. Check Go dependencies: `go mod verify`

## Performance Testing

Once working, we can run REAL benchmarks to compare against mark3labs and validate your performance claims.
