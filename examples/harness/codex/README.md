# Codex + filesystem-ultra

Do not treat ultra `apply_patch` as Codex `*** Begin Patch`. Ultra wants a unified diff, one file per call. No Begin-Patch adapter in this pack. Ultra is for Claude Code / OpenCode / Desktop.

`~/.codex/config.toml` (placeholders — one instance per workspace):

```toml
[mcp_servers.filesystem-ultra]
command = "C:\\path\\to\\filesystem-ultra-v4.exe"
args = ["--profile", "strict", "--compact-mode", "--roots-mode", "union", "C:\\path\\to\\workspace"]
```

Linux: `command = "/path/to/filesystem-ultra"` and a POSIX workspace path.

One role = one disk family. Native Codex `apply_patch` / shell file I/O is a different sandbox — do not mix with ultra in the same agent.

Handoff: roots, path, content_hash, backup_id, profile. The child re-reads.
