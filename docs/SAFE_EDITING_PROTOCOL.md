# 🛡️ SAFE EDITING PROTOCOL

**For: Claude Code, Claude Desktop, and developers**
**Purpose: 0 RISK of file destruction**
**Last Updated: 2025-12-20**

---

## ⚡ Quick Start (Copy-Paste Checklist)

Use this checklist for ANY file edit operation:

```markdown
## Before Editing File: [FILENAME]

### STEP 1: Read & Verify (5 min)
- [ ] Open file with `read_file_range()` to see exact content
- [ ] Copy the EXACT text you want to change (including spaces/tabs)
- [ ] Note the line numbers
- [ ] Paste here: [EXACT_TEXT_HERE]

### STEP 2: Confirm Pattern (2 min)
- [ ] Run `count_occurrences()` to find how many times it appears
- [ ] Expected count: [NUMBER]
- [ ] Actual count: [NUMBER]
- [ ] ✅ Counts match: YES / NO

### STEP 3: Validate Edit (2 min)
- [ ] Small file (< 1 MB): Use `recovery_edit()` or `intelligent_edit()`
- [ ] Large file (> 1 MB): Use `batch_operations` with atomic=true
- [ ] Critical change: Use `batch_operations` even if small
- [ ] Multiple changes: ALWAYS use `batch_operations`
- [ ] ✅ Method chosen: [METHOD]

### STEP 4: Execute Edit (1 min)
- [ ] For recovery_edit:
  ```
  path: [FILE_PATH]
  old_text: [COPY_FROM_READ_FILE_RANGE]
  new_text: [REPLACEMENT_TEXT]
  force: false (unless risk warning)
  ```
- [ ] For batch_operations:
  ```json
  {
    "operations": [
      {
        "type": "edit",
        "path": "[FILE_PATH]",
        "old_text": "[FROM_READ_FILE_RANGE]",
        "new_text": "[REPLACEMENT]"
      }
    ],
    "atomic": true
  }
  ```
- [ ] ✅ Operation executed

### STEP 5: Verify Result (2 min)
- [ ] File size didn't change drastically
- [ ] New text appears in file: Use `read_file_range()` to check
- [ ] Old text is gone: Use `count_occurrences()` to confirm 0 matches
- [ ] ✅ All verifications passed: YES / NO

**Total Time: ~12 minutes for safe edit**
```

---

## 🔴 CRITICAL RULES (NEVER Break These)

### Rule 1: NEVER Use Fuzzy Matching for Critical Edits
❌ **BAD**: Copy text from memory and hope it matches
✅ **GOOD**: Use `read_file_range()` to get exact text

### Rule 2: NEVER Skip Line Ending Verification
❌ **BAD**: Assume file uses Unix line endings
✅ **GOOD**: Check `read_file_range()` output for `\r\n` or `\n`

### Rule 3: NEVER Edit Without Backup
❌ **BAD**: Use `recovery_edit()` without `force=true` when at risk
✅ **GOOD**: Always have backup created automatically (v3.8.0+)

### Rule 4: NEVER Do Multiple Changes in One Edit
❌ **BAD**:
```
old_text: "line1\nline2\nline3\nline4\nline5"
new_text: "line1_new\nline2_new"
```

✅ **GOOD**:
```
operations: [
  { type: "edit", old_text: "line1", new_text: "line1_new" },
  { type: "edit", old_text: "line2", new_text: "line2_new" },
  { type: "edit", old_text: "line3", new_text: "" }
]
```

### Rule 5: NEVER Skip Verification After Edit
❌ **BAD**: Edit completes, move on immediately
✅ **GOOD**: Verify with `count_occurrences()` that old text is gone

---

## 📋 DECISION TREE

```
START: "I need to edit a file"
  │
  ├─ Is it a CRITICAL file? (config, code, database schema)
  │  ├─ YES → Use batch_operations with atomic=true
  │  └─ NO ─┐
  │         └─ Go to next check
  │
  ├─ Does the old_text span MULTIPLE LINES?
  │  ├─ YES (3+ lines) → Use batch_operations
  │  └─ NO (1-2 lines) ─┐
  │                     └─ Go to next check
  │
  ├─ Are you making MULTIPLE CHANGES?
  │  ├─ YES (2+ edits) → Use batch_operations
  │  └─ NO (1 edit) ─┐
  │                  └─ Go to next check
  │
  ├─ Is the FILE SIZE > 1 MB?
  │  ├─ YES → Use batch_operations
  │  └─ NO ─┐
  │         └─ Go to next check
  │
  └─ DECISION: Use recovery_edit() or intelligent_edit()
```

---

## 🛠️ Tool Reference

### 1. read_file_range() - Get Exact Content
```python
response = await client.call_tool(
    "filesystem-ultra:read_file_range",
    {
        "path": "/path/to/file.cs",
        "start_line": 10,
        "end_line": 20
    }
)
# Output shows EXACT text with line numbers
# COPY THIS EXACTLY for old_text parameter
```

**Why**: Guarantees you have the exact text including whitespace

---

### 2. count_occurrences() - Verify Pattern Exists
```python
response = await client.call_tool(
    "filesystem-ultra:count_occurrences",
    {
        "path": "/path/to/file.cs",
        "pattern": "exact_text_from_read_file_range",
        "return_line_numbers": true
    }
)
# Output shows how many matches and which lines
# Must be > 0 before attempting edit
```

**Why**: Confirms the text exists exactly as you expect

---

### 3. recovery_edit() - Single Safe Edit
```python
response = await client.call_tool(
    "filesystem-ultra:recovery_edit",
    {
        "path": "/path/to/file.cs",
        "old_text": "COPY_EXACTLY_FROM_read_file_range",
        "new_text": "replacement text",
        "force": False  # Set to True only if risk warning appears
    }
)
# Returns: Modified file, backup ID, lines affected
# After: Verify with count_occurrences() that old text is gone
```

**When to use**: Single-line or 1-2 line replacements on small files
**When NOT to use**: Multiple changes, large edits, critical files

---

### 4. batch_operations() - Multiple Safe Edits
```python
response = await client.call_tool(
    "filesystem-ultra:batch_operations",
    {
        "operations": [
            {
                "type": "edit",
                "path": "/path/to/file.cs",
                "old_text": "line1 to replace",
                "new_text": "line1 replacement"
            },
            {
                "type": "edit",
                "path": "/path/to/file.cs",
                "old_text": "line2 to replace",
                "new_text": "line2 replacement"
            }
        ],
        "atomic": True  # ALWAYS true for safety
    }
)
# All operations apply together, or none if any fails
# After: Verify each change with count_occurrences()
```

**When to use**: ALWAYS for critical edits, multiple changes, large files
**Advantage**: Atomic (all-or-nothing), automatic backup

---

### 5. smart_search() - Find Before Editing
```python
response = await client.call_tool(
    "filesystem-ultra:smart_search",
    {
        "path": "/path/to/file.cs",
        "pattern": "partial_text_to_find",
        "context_lines": 3
    }
)
# Shows matches with surrounding code
# Use to understand the context before editing
```

**Why**: Understand code context before making changes

---

## 🔄 Complete Workflow Example

### Scenario: Edit C# File (from Bug #8)

**BEFORE** (❌ No validation - file got corrupted):
```python
# Read file
response1 = client.call_tool("intelligent_read", {"path": "file.cs"})

# Try to edit without checking if text exists
response2 = client.call_tool("recovery_edit", {
    "path": "file.cs",
    "old_text": "...some multiline text...",
    "new_text": "replacement"
})
# ❌ FAILS: "old_text not found"
```

**AFTER** (✅ Safe validation - guaranteed success):

```python
# STEP 1: Read exact content
response1 = await client.call_tool(
    "filesystem-ultra:read_file_range",
    {"path": "file.cs", "start_line": 10, "end_line": 20}
)
# Output:
# Line 10:     // Orders list
# Line 11:     public List<C1Pedidos> Orders { get; set; } = new();
# Line 12:     private List<C1Pedidos> filteredOrders = new();
# Line 13:     private bool isFiltered = false;
# Line 14:     private Dictionary<int, bool> listadoValidados = new Dictionary<int, bool>();

# STEP 2: Copy EXACTLY and verify it exists
exact_text = """    // Orders list
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;
    private Dictionary<int, bool> listadoValidados = new Dictionary<int, bool>();"""

response2 = await client.call_tool(
    "filesystem-ultra:count_occurrences",
    {"path": "file.cs", "pattern": exact_text, "return_line_numbers": true}
)
# Output: Found 1 match at line 10
# ✅ Confirmed: Text exists exactly as expected

# STEP 3: Use batch_operations (safer for multiline)
response3 = await client.call_tool(
    "filesystem-ultra:batch_operations",
    {
        "operations": [
            {
                "type": "edit",
                "path": "file.cs",
                "old_text": exact_text,
                "new_text": """    // Orders list
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;"""
            }
        ],
        "atomic": True
    }
)
# ✅ SUCCESS: 1 replacement, file updated

# STEP 4: Verify the result
response4 = await client.call_tool(
    "filesystem-ultra:count_occurrences",
    {"path": "file.cs", "pattern": "private Dictionary<int, bool>"}
)
# Output: Found 0 matches
# ✅ Confirmed: Old text is gone

response5 = await client.call_tool(
    "filesystem-ultra:read_file_range",
    {"path": "file.cs", "start_line": 10, "end_line": 20}
)
# Output shows updated content without the Dictionary line
# ✅ SUCCESS: Edit verified
```

---

## ⚠️ Common Mistakes & How to Avoid Them

### Mistake 1: Different Line Endings
```
❌ BAD:
old_text = "line1\nline2"  # Unix
# But file has: "line1\r\nline2"  # Windows
# Result: NO MATCH

✅ GOOD:
# Check read_file_range() output for line ending type
# Match exactly what's shown
```

### Mistake 2: Extra Whitespace
```
❌ BAD:
old_text = "public List Orders { get; set; }"
# But file has: "    public List Orders { get; set; }"
#               (4 spaces at start)
# Result: NO MATCH

✅ GOOD:
old_text = "    public List Orders { get; set; }"
# Copy from read_file_range() output with indentation
```

### Mistake 3: TAB vs SPACES
```
❌ BAD:
old_text = "    property = value"  # 4 spaces
# But file has: "\tproperty = value"  # 1 tab
# Result: NO MATCH

✅ GOOD:
# Use read_file_range() which shows [SPACE] and [TAB] clearly
```

### Mistake 4: Editing Without Verification
```
❌ BAD:
# Edit multiple lines and hope it works
# No verification afterwards

✅ GOOD:
# After each edit, use count_occurrences() on old text
# Should return 0
# Use read_file_range() to view the actual result
```

### Mistake 5: One Big Edit Instead of Smaller Ones
```
❌ BAD:
old_text = """entire_method_body_here
                lots_of_lines
                might_fail"""
new_text = "simplified_replacement"

✅ GOOD:
# Split into multiple edits if possible
# Use batch_operations for all changes
# One operation per logical change
```

---

## 🔧 Troubleshooting

### "old_text not found in current file"

**Diagnosis**: The exact text doesn't exist

**Steps**:
1. Run `read_file_range()` around the area you think it is
2. Copy the text EXACTLY (spaces, tabs, line endings)
3. Use `count_occurrences()` first to verify it exists
4. If found, try again with exact text

### "Context validation failed"

**Diagnosis**: File was modified since you read it

**Steps**:
1. Run `read_file_range()` again to get latest content
2. Verify the text still exists
3. Update your `old_text` if needed
4. Retry edit

### "OPERATION BLOCKED - Change impact is HIGH/CRITICAL"

**Diagnosis**: Edit is large/risky, need explicit approval

**Steps**:
1. Review the warning message carefully
2. Add `"force": true` if you're sure
3. Better: Use `batch_operations` with smaller edits
4. Better: Create backup first manually if needed

### "Expected recovery but file content changed unexpectedly"

**Diagnosis**: Something went wrong with file sync

**Steps**:
1. Don't edit anymore
2. Check backup files in `.mcp-filesystem-backups/`
3. Use `restore_backup()` if needed
4. Report this as a bug

---

## 📊 When to Use Which Tool

| Situation | Tool | Reason |
|-----------|------|--------|
| First time editing a file | `read_file_range()` | Get exact content |
| About to edit | `count_occurrences()` | Confirm text exists |
| Single line edit, small file | `recovery_edit()` | Simple, fast |
| Multiple line edit | `batch_operations` | Atomic, safer |
| Critical file | `batch_operations` | More reliable |
| File > 1 MB | `batch_operations` | Better performance |
| Don't know where to edit | `smart_search()` | Find first |
| Want to see context | `smart_search()` | Context included |
| After any edit | `count_occurrences()` | Verify success |
| Verify entire section | `read_file_range()` | See actual result |

---

## 🚨 Emergency Procedures

### If File Got Corrupted

```python
# 1. Stop immediately - don't make more edits

# 2. Check if backup exists
response = await client.call_tool(
    "filesystem-ultra:list_backups",
    {"file_path": "/path/to/file.cs"}
)

# 3. Restore from backup
response = await client.call_tool(
    "filesystem-ultra:restore_backup",
    {
        "backup_id": "backup_id_from_list_backups",
        "restore_to": "/path/to/file.cs"
    }
)

# 4. Verify restoration
response = await client.call_tool(
    "filesystem-ultra:read_file_range",
    {"path": "/path/to/file.cs", "start_line": 1, "end_line": 20}
)
```

### If You're Not Sure About an Edit

```
1. DON'T USE recovery_edit()
2. DON'T USE batch_operations without verification
3. DO USE: read_file_range() to see what's there
4. DO USE: count_occurrences() to confirm text exists
5. DO USE: smart_search() to understand context
6. THEN decide if edit is safe
```

---

## 📚 Related Documentation

- [Bug #8 Fix Details](docs/BUG8_FIX.md) - Complete technical explanation
- [Edit Operations Source](core/edit_operations.go) - Implementation details
- [Backup & Recovery System](docs/BUG10.md) - How backup works
- [Windows Filesystem Issues](guides/WINDOWS_FILESYSTEM_PERSISTENCE.md) - Platform-specific

---

## 🎯 Summary

**Remember the 5 Steps**:

1. **Read** - Use `read_file_range()` to see exact content
2. **Verify** - Use `count_occurrences()` to confirm pattern exists
3. **Choose** - Use `recovery_edit()` or `batch_operations`
4. **Execute** - Run the edit with exact text
5. **Confirm** - Use `count_occurrences()` to verify success

**Remember the 5 Rules**:

1. NEVER use fuzzy matching for critical edits
2. NEVER skip line ending verification
3. NEVER edit without backup
4. NEVER do multiple changes in one edit
5. NEVER skip verification after edit

**Use this Protocol for EVERY file edit**

---

*Updated: 2025-12-20 | Version: 3.10.0 | Status: ACTIVE*
