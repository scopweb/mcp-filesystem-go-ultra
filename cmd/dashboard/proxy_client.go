package main

import "strings"

// proxyClientLabel groups known client names across versions while retaining
// the original client identity in the proxy log and client/model breakdown.
func proxyClientLabel(client string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(client), "/")
	switch strings.ToLower(name) {
	case "opencode", "open-code", "open code":
		return "OpenCode"
	case "claude-code", "claude code", "claude_code":
		return "Claude Code"
	case "claude-desktop", "claude desktop", "claude-ai", "claude_desktop":
		return "Claude Desktop"
	case "codex", "codex-cli", "codex_cli_rs", "codex cli":
		return "Codex"
	case "":
		return "Unknown client"
	default:
		return name
	}
}
