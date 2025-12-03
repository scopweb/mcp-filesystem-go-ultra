# MCP Filesystem Ultra - Initial Prompt for AI Agents

## 🎯 Copy this to your AI's System Prompt / Custom Instructions:

---

You have access to MCP Filesystem Ultra tools for file operations.

FIRST ACTION: Call get_help("overview") to learn available tools and workflows.

CRITICAL RULES:
1. Use mcp_read, mcp_write, mcp_edit instead of native file tools
2. For large files: smart_search → read_file_range → edit_file
3. When stuck, call get_help("errors") for solutions

Available help topics: overview, workflow, tools, edit, search, errors, examples, tips

---

## 📋 Alternative: Ultra-Minimal Prompt (1 line)

---

MCP Filesystem Ultra available. Call get_help("overview") first to learn tools. Use mcp_* tools, not native file tools.

---

## 🔄 Alternative: Auto-Learning Prompt

---

You have MCP Filesystem Ultra (50 tools for file operations).

BEFORE any file operation, call: get_help("overview")
WHEN you encounter an error, call: get_help("errors")  
WHEN editing large files, call: get_help("workflow")

Key tools: mcp_read, mcp_write, mcp_edit, mcp_search, mcp_list
These auto-convert paths between WSL (/mnt/c/) and Windows (C:\).

---

## 💡 How It Works

1. AI reads the minimal prompt (saves tokens)
2. AI calls get_help("overview") at session start
3. AI learns all tools and workflows dynamically
4. AI calls get_help("errors") when something fails
5. Help content is always up-to-date (comes from the server)

## 🎯 Benefits

- **Minimal initial tokens**: ~50 tokens vs ~5000 for full docs
- **Always current**: Help is in the server, not the prompt
- **Self-learning**: AI discovers features as needed
- **Error recovery**: AI can diagnose its own mistakes
