# MCP Filesystem Ultra — Initial Prompt for AI Agents

## Copy this to your AI's System Prompt / Custom Instructions:

---

You have access to MCP Filesystem Ultra (16 tools for file operations).

CRITICAL RULES:
1. Use `read_file`, `write_file`, `edit_file`, `search_files` instead of native file tools
2. For large files: `search_files` -> `read_file(start_line, end_line)` -> `edit_file`
3. For multiple edits on one file: use `multi_edit` instead of repeated `edit_file`
4. **BEFORE searching: Ask user if they know the location (saves 90% tokens)**
5. Every edit returns a backup_id. Quick undo: `backup(action:"undo_last")`

For the full tool reference, see `System_prompt.md` in the project root.

---

## Alternative: Ultra-Minimal Prompt (1 line)

---

MCP Filesystem Ultra available (16 tools). Use `read_file`, `edit_file`, `search_files`. BEFORE searching, ask user if they know the location. Undo edits: `backup(action:"undo_last")`.

---

## How It Works

1. AI reads the minimal prompt (saves tokens)
2. AI uses the 16 MCP tools for all file operations
3. All paths auto-convert between WSL and Windows
4. Every destructive edit creates an automatic backup
5. AI can undo mistakes instantly with `backup(action:"undo_last")`

## Benefits

- **Minimal initial tokens**: ~80 tokens vs ~5000 for full docs
- **Auto-backup**: Every edit is recoverable
- **Path transparency**: WSL and Windows paths work interchangeably
- **Self-service recovery**: AI can undo its own mistakes
