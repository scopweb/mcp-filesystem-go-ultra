# 🤖 MCP Filesystem Ultra - Instructions for AI Agents (v3.7.0)

> **This document is designed to be included in AI agent system prompts or context.**
> Copy this entire content to your AI's custom instructions or memory.
>
> 💡 **TIP**: You can also call `get_help()` at runtime to get this information dynamically!

---

## 🆕 SELF-LEARNING: Use `get_help()` Tool

Instead of reading all documentation, you can call the `get_help` tool anytime:

```
get_help("overview")  → Quick start guide
get_help("workflow")  → The 4-step efficient workflow  
get_help("tools")     → Complete list of 50 tools
get_help("edit")      → Editing files (most important!)
get_help("errors")    → Common errors and fixes
get_help("examples")  → Practical code examples
get_help("tips")      → Pro tips for efficiency
```

---

## ⚡ CRITICAL: USE MCP TOOLS, NOT NATIVE FILE TOOLS

When you have access to **mcp-filesystem-ultra** tools, **ALWAYS prefer them** over native file operations:

### ✅ USE THESE (MCP Tools)
```
mcp_read, mcp_write, mcp_edit, mcp_list, mcp_search
```
Or their original names:
```
read_file, write_file, edit_file, list_directory, smart_search
```

### ❌ AVOID THESE (Native/WSL)
- Native file reading tools
- Direct WSL commands for file operations
- Any tool that doesn't handle `/mnt/c/` ↔ `C:\` conversion

**Why?** MCP tools automatically convert paths between WSL and Windows formats.

---

## 🎯 THE GOLDEN RULE: Surgical Edits Save 98% Tokens

### ❌ WASTEFUL (Never do this)
```
read_file(entire_large_file) → write_file(entire_large_file)
5000-line file = 250,000+ tokens wasted
```

### ✅ EFFICIENT (Always do this)
```
smart_search(file, pattern) → read_file_range(start, end) → edit_file(old, new)
5000-line file = 2,000 tokens (98% savings!)
```

---

## 📋 COMPLETE TOOL LIST (49 Tools in v3.7.0)

### 🆕 MCP-Prefixed Aliases (NEW in v3.7.0)
Use these to avoid conflicts with native tools:

| Tool | Description |
|------|-------------|
| `mcp_read` | Read file with WSL↔Windows path conversion |
| `mcp_write` | Atomic write with auto path conversion |
| `mcp_edit` | Smart edit with backup + path conversion |
| `mcp_list` | Cached directory listing |
| `mcp_search` | File/content search |

### 📖 Reading Files
| Tool | When to Use |
|------|-------------|
| `read_file` | Small files (<1000 lines) |
| `read_file_range` | **PREFERRED** - Read only lines N to M |
| `intelligent_read` | Auto-optimizes based on file size |
| `chunked_read_file` | Very large files (>1MB) |

### ✏️ Writing & Editing
| Tool | When to Use |
|------|-------------|
| `write_file` | Create or overwrite files |
| `create_file` | Alias for write_file |
| `edit_file` | **PREFERRED** - Surgical text replacement |
| `multi_edit` | Multiple edits in one atomic operation |
| `replace_nth_occurrence` | Replace specific occurrence (1st, last, etc.) |
| `intelligent_write` | Auto-optimizes based on size |
| `intelligent_edit` | Auto-optimizes based on size |
| `streaming_write_file` | Very large files |
| `smart_edit_file` | Large file editing |
| `recovery_edit` | Edit with error recovery |

### 🔍 Search
| Tool | When to Use |
|------|-------------|
| `smart_search` | Find file location (returns line numbers) |
| `mcp_search` | Same with explicit MCP naming |
| `advanced_text_search` | Complex pattern search |
| `search_and_replace` | Bulk find & replace |
| `count_occurrences` | Count matches without reading file |

### 📁 File Operations
| Tool | When to Use |
|------|-------------|
| `copy_file` | Duplicate file/directory |
| `move_file` | Move to new location |
| `rename_file` | Rename file/directory |
| `delete_file` | Permanent delete |
| `soft_delete_file` | Safe delete (to trash) |
| `get_file_info` | File metadata (size, date, etc.) |

### 📂 Directory Operations
| Tool | When to Use |
|------|-------------|
| `list_directory` | List contents |
| `mcp_list` | Same with explicit MCP naming |
| `create_directory` | Create dir (+ parents) |

### 🔄 WSL ↔ Windows Sync
| Tool | When to Use |
|------|-------------|
| `wsl_to_windows_copy` | Copy from WSL to Windows |
| `windows_to_wsl_copy` | Copy from Windows to WSL |
| `sync_claude_workspace` | Sync entire workspace |
| `wsl_windows_status` | Check sync status |
| `configure_autosync` | Enable/disable auto-sync |
| `autosync_status` | Check auto-sync config |

### 📊 Analysis & Monitoring
| Tool | When to Use |
|------|-------------|
| `analyze_file` | Get optimization recommendations |
| `analyze_write` | Dry-run write analysis |
| `analyze_edit` | Dry-run edit analysis |
| `analyze_delete` | Dry-run delete analysis |
| `get_edit_telemetry` | Monitor edit efficiency |
| `get_optimization_suggestion` | Get tips |
| `performance_stats` | Server performance |

### 📦 Batch Operations
| Tool | When to Use |
|------|-------------|
| `batch_operations` | Multiple ops atomically |

### 💾 Artifacts
| Tool | When to Use |
|------|-------------|
| `capture_last_artifact` | Store code in memory |
| `write_last_artifact` | Write stored code to file |
| `artifact_info` | Info about stored artifact |

---

## 🔄 THE 4-STEP EFFICIENT WORKFLOW

For ANY file edit, follow this workflow:

### Step 1: LOCATE
```
smart_search(file, "function_name")
→ Returns: "Found at lines 45-67"
```

### Step 2: READ (Only what you need)
```
read_file_range(file, 45, 67)
→ Returns: Only those 22 lines
```

### Step 3: EDIT (Surgically)
```
edit_file(file, "old_text", "new_text")
→ Returns: "OK: 1 changes"
```

### Step 4: VERIFY (Optional)
```
get_edit_telemetry()
→ Goal: >80% targeted_edits
```

---

## 📏 FILE SIZE DECISION TREE

```
Is file < 1000 lines?
├── YES → read_file() is OK
└── NO  → MUST use smart_search + read_file_range + edit_file

Is file > 5000 lines?
├── NO  → Standard workflow is fine
└── YES → CRITICAL: Never read entire file
```

---

## ⚠️ COMMON ERRORS & SOLUTIONS

### "context validation failed"
**Cause:** File changed since you read it
**Fix:** Re-run `smart_search()` + `read_file_range()` to get fresh content

### "no match found"
**Cause:** Text doesn't exist exactly as specified
**Fix:** 
1. Use `smart_search()` to verify location
2. Check for whitespace/indentation differences
3. Use `count_occurrences()` to verify text exists

### "multiple matches found"
**Cause:** Same text appears multiple times
**Fix:** Use `replace_nth_occurrence(file, pattern, new, occurrence=-1)`
- `1` = first, `2` = second, `-1` = last, `-2` = penultimate

### "Tool not found: create_file"
**Cause:** `create_file` was previously an alias
**Fix:** Use `write_file()` instead - it creates files if they don't exist

### Path errors with /mnt/c/ or C:\
**Cause:** Path format mismatch
**Fix:** Use MCP tools - they auto-convert paths. Use `mcp_read`, `mcp_write`, etc.

---

## 🎯 QUICK REFERENCE TABLE

| I want to... | Use this tool |
|--------------|---------------|
| Read a small file | `mcp_read` or `read_file` |
| Read specific lines | `read_file_range` ⭐ |
| Create a new file | `mcp_write` or `write_file` |
| Edit text in a file | `mcp_edit` or `edit_file` ⭐ |
| Make multiple edits | `multi_edit` ⭐ |
| Find where code is | `mcp_search` or `smart_search` |
| Count occurrences | `count_occurrences` |
| Replace last match only | `replace_nth_occurrence` |
| List directory | `mcp_list` or `list_directory` |
| Copy/Move files | `copy_file`, `move_file` |
| Delete safely | `soft_delete_file` |
| Multiple operations | `batch_operations` |
| Check my efficiency | `get_edit_telemetry` |

⭐ = Recommended for token efficiency

---

## 💡 TOKEN EFFICIENCY EXAMPLES

### Example 1: Edit a function in a 5000-line file

**❌ Wasteful approach: ~250,000 tokens**
```
read_file("large.py")        # 125,000 tokens
# ... process ...
write_file("large.py", all)  # 125,000 tokens
```

**✅ Efficient approach: ~2,500 tokens**
```
smart_search("large.py", "def my_function")  # 500 tokens
read_file_range("large.py", 234, 256)        # 1,000 tokens
edit_file("large.py", "old", "new")          # 500 tokens
```

**Savings: 247,500 tokens (99% reduction!)**

### Example 2: Multiple edits in one file

**❌ Wasteful: 5 separate edit_file calls**
```
edit_file(path, old1, new1)  # Read → Edit → Write
edit_file(path, old2, new2)  # Read → Edit → Write (again!)
edit_file(path, old3, new3)  # Read → Edit → Write (again!)
...
```

**✅ Efficient: 1 multi_edit call**
```
multi_edit(path, [
  {"old_text": "old1", "new_text": "new1"},
  {"old_text": "old2", "new_text": "new2"},
  {"old_text": "old3", "new_text": "new3"}
])
# File read ONCE, all edits applied, written ONCE
```

**Savings: ~80% fewer file operations**

---

## 🔧 PATH HANDLING

All MCP tools automatically handle path conversion:

| You provide | Tool converts to |
|-------------|------------------|
| `/mnt/c/Users/John/file.txt` | `C:\Users\John\file.txt` (on Windows) |
| `C:\Users\John\file.txt` | `/mnt/c/Users/John/file.txt` (on WSL) |

**No manual conversion needed!**

---

## 📌 REMEMBER

1. **Always prefer `mcp_*` tools** over native file operations
2. **Never read entire large files** - use `read_file_range`
3. **Use `edit_file` not `write_file`** for changes
4. **Use `multi_edit`** for multiple changes in one file
5. **Use `smart_search` first** to find exact locations
6. **Check `get_edit_telemetry`** to monitor your efficiency

---

*Version: 3.7.0 | Last Updated: 2025-11-30*
