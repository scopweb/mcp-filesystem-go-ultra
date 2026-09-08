// Command filesystem-ultra is the MCP filesystem server entry point.
//
// It holds nothing but the process entry point: configuration, tool
// registration and the stdio serve loop live in internal/mcpserver, so the
// server package and its tests can be built and exercised on their own.
package main

import "github.com/mcp/filesystem-ultra/internal/mcpserver"

func main() {
	mcpserver.Run()
}
