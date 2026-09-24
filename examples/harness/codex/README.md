# Codex + filesystem-ultra

Do not treat ultra `apply_patch` as Codex `*** Begin Patch`. Ultra wants a unified diff. A file path is one diff; a directory path is a multi-file unified diff (in-process transaction, not crash-durable). `expected_hash` is single-file only. No Begin-Patch adapter in this pack. A binary that returns `multi-file patch not supported` is an older build, not this main. Ultra is for Claude Code / OpenCode / Desktop.

`~/.codex/config.toml` (placeholders — one instance per workspace):

```toml
[mcp_servers.filesystem-ultra]
command = "C:\\path\\to\\filesystem-ultra-v4.exe"
args = ["--profile", "strict", "--compact-mode", "--roots-mode", "union", "C:\\path\\to\\workspace"]
```

Linux: `command = "/path/to/filesystem-ultra"` and a POSIX workspace path.

One role = one disk family. Native Codex `apply_patch` / shell file I/O is a different sandbox — do not mix with ultra in the same agent.

Handoff: roots, path, content_hash, backup_id, profile. The child re-reads.
