package main

import (
	"fmt"
	"strings"
)

// getHelpContent returns help content for the specified topic
func getHelpContent(topic string, compactMode bool) string {
	var sb strings.Builder

	switch topic {
	case "overview":
		sb.WriteString(`# MCP Filesystem Ultra v4.2.0 - Quick Start

## CRITICAL RULE
Always use MCP tools (read_file, write_file, edit_file) instead of native file tools.
These auto-convert paths between WSL (/mnt/c/) and Windows (C:\).

## THE GOLDEN RULE
Surgical edits save 98% tokens:
BAD:  read_file(large) -> write_file(large) = 250k tokens
GOOD: search_files -> read_file(start_line/end_line) -> edit_file = 2k tokens

## AVAILABLE TOPICS
Call server_info(topic) with:
- "workflow" - The 4-step efficient workflow
- "tools"    - Complete list of 16 tools
- "read"     - Reading files efficiently
- "write"    - Writing and creating files
- "edit"     - Editing files (most important!)
- "search"   - Finding content in files
- "batch"    - Multiple operations at once
- "errors"   - Common errors and fixes
- "examples" - Code examples
- "tips"     - Pro tips for efficiency
- "recovery" - Disaster recovery from bad edits
- "all"      - Everything (long output)
`)

	case "workflow":
		sb.WriteString(`# THE 4-STEP EFFICIENT WORKFLOW

Use this workflow for ANY file >1000 lines:

## Step 1: LOCATE
search_files(file, "function_name")
-> Returns: "Found at lines 45-67"
-> Cost: ~500 tokens

## Step 2: READ (Only what you need)
read_file(file, start_line=45, end_line=67)
-> Returns: Only those 22 lines
-> Cost: ~1000 tokens

## Step 3: EDIT (Surgically)
edit_file(file, "old_text", "new_text")
-> Returns: "OK: 1 changes"
-> Cost: ~500 tokens

## Step 4: VERIFY (Optional)
server_info(action:"stats")
-> Goal: >80% targeted_edits

## FILE SIZE RULES
<1000 lines  -> read_file() is OK
1000-5000    -> MUST use this workflow
>5000 lines  -> CRITICAL - never read entire file

## TOTAL COST: ~2k tokens vs 250k (98% savings!)
`)

	case "tools":
		sb.WriteString(`# COMPLETE TOOL LIST (16 Tools)

Use this topic when the client hides full tool schemas. Each entry lists the most important parameters only.

## Core (5)

read_file
- Purpose: Read a full file, line range, head/tail, or base64 content
- Key params: path, start_line, end_line, max_lines, mode, encoding
- Examples: read_file(path), read_file(path, start_line=10, end_line=30)

write_file
- Purpose: Create or overwrite a file atomically
- Key params: path, content, content_base64, encoding
- Examples: write_file(path, content="text")

edit_file
- Purpose: Modify existing files with backup and risk checks
- Key params: path, old_text, new_text, mode, pattern, replacement, occurrence, force
- Modes: replace (default), search_replace, regex
- Examples: edit_file(path, old_text, new_text), edit_file(path, mode:"search_replace", pattern, replacement)

list_directory
- Purpose: List directory contents
- Key params: path
- Examples: list_directory(path)

search_files
- Purpose: Search by filename or file content
- Key params: path, pattern, file_types, include_content, include_context, case_sensitive, count_only
- Examples: search_files(path, pattern), search_files(path, pattern, include_content=true)

## Edit+ (1)

multi_edit
- Purpose: Apply multiple exact replacements to the same file atomically
- Key params: path, edits_json
- Examples: multi_edit(path, edits_json='[{"old_text":"A","new_text":"B"}]')

## Files (4)

move_file
- Purpose: Move or rename a file or directory
- Key params: source_path, dest_path
- Examples: move_file(source_path, dest_path)

copy_file
- Purpose: Copy a file or directory
- Key params: source_path, dest_path
- Examples: copy_file(source_path, dest_path)

delete_file
- Purpose: Soft-delete by default, or hard-delete permanently
- Key params: path, permanent
- Examples: delete_file(path), delete_file(path, permanent=true)

create_directory
- Purpose: Create a directory tree like mkdir -p
- Key params: path
- Examples: create_directory(path)

## Analysis (2)

get_file_info
- Purpose: Return metadata for a file or directory
- Key params: path
- Examples: get_file_info(path)

analyze_operation
- Purpose: Preview risk and impact before acting
- Key params: path, operation
- Valid operations: file, optimize, write, edit, delete
- Examples: analyze_operation(path, operation:"edit")

## Batch (1)

batch_operations
- Purpose: Run atomic multi-file ops, pipelines, or batch rename
- Key params: request_json, pipeline_json, rename_json
- Examples: batch_operations(request_json='{"operations":[...]}')

## Backup (1)

backup
- Purpose: List, inspect, compare, restore, clean up, or undo backups
- Key params: action, backup_id, file_path, preview, filter_path
- Valid actions: list, info, compare, cleanup, restore, undo_last
- Examples: backup(action:"list"), backup(action:"undo_last")

## WSL (1)

wsl
- Purpose: WSL/Windows sync, status, and autosync operations
- Key params: action, wsl_path, windows_path, direction, auto_create_dirs
- Valid actions: status, sync
- Examples: wsl(action:"status"), wsl(wsl_path, windows_path, direction)

## Info (1)

server_info
- Purpose: Help topics, performance stats, and artifact management
- Key params: action, topic, sub_action, content, path
- Valid actions: help, stats, artifact
- Examples: server_info(action:"stats"), server_info(action:"help", topic:"edit")

## Aliases (6 + help)

read_text_file -> read_file
search -> search_files
edit -> edit_file
write -> write_file
create_file -> write_file
directory_tree -> list_directory
help -> standalone discovery tool, not an alias
`)

	case "read":
		sb.WriteString(`# READING FILES EFFICIENTLY

## Quick Reference
| File Size    | How to Read                    |
|--------------|-------------------------------|
| <1000 lines  | read_file(path)               |
| >1000 lines  | read_file(path, start_line=N, end_line=M) |
| Binary file  | read_file(path, encoding:"base64") |

## Best Practice: Line Range
# Read only lines 100-150 of a large file
read_file(path, start_line=100, end_line=150)

## Why This Matters
5000-line file:
- read_file: ~125,000 tokens
- read_file with start_line/end_line (50 lines): ~2,500 tokens
- Savings: 98%!

## Workflow
1. search_files(file, pattern) -> find line numbers
2. read_file(file, start_line=N, end_line=M) -> read only those
3. Never read more than you need!
`)

	case "write":
		sb.WriteString(`# WRITING FILES

## Quick Reference
| Task           | How                          |
|----------------|------------------------------|
| Create file    | write_file(path, content)    |
| Overwrite file | write_file(path, content)    |
| Binary file    | write_file(path, content_base64="...") |

## Examples

# Create or overwrite a file
write_file("/path/to/file.txt", content="content here")

# Write binary from base64
write_file("/path/to/image.png", content_base64="iVBOR...")

## IMPORTANT
- write_file OVERWRITES the entire file
- For small changes, use edit_file instead!
- write_file also creates parent directories automatically

## Path Handling
All tools auto-convert paths:
/mnt/c/Users/... -> C:\Users\... (on Windows)
C:\Users\... -> /mnt/c/Users/... (on WSL)
`)

	case "edit":
		sb.WriteString(`# EDITING FILES (MOST IMPORTANT!)

## THE GOLDEN RULE
Use edit_file for changes, NOT write_file!

## Quick Reference
| Task                    | How                                          |
|-------------------------|----------------------------------------------|
| Single replacement      | edit_file(path, old_text, new_text)          |
| Multiple replacements   | multi_edit(path, edits_json)                 |
| Replace specific match  | edit_file(path, old_text, new_text, occurrence=N) |
| Regex transformation    | edit_file(path, mode:"regex", patterns_json) |
| Search & replace all    | edit_file(path, mode:"search_replace", pattern, replacement) |

## Examples

# Simple edit
edit_file("/path/file.py", old_text="old_function()", new_text="new_function()")

# Multiple edits in one file (EFFICIENT!)
multi_edit("/path/file.py", edits_json='[
  {"old_text": "foo", "new_text": "bar"},
  {"old_text": "baz", "new_text": "qux"}
]')

# Replace only the LAST occurrence
edit_file("/path/file.py", old_text="TODO", new_text="DONE", occurrence=-1)
# occurrence: 1=first, 2=second, -1=last, -2=second-to-last

## COMMON ERRORS

"no match found"
-> Text doesn't exist exactly. Check whitespace!
-> Use search_files first to verify

"context validation failed"
-> File changed since you read it
-> Re-run search_files + read_file with start_line/end_line

"multiple matches found"
-> Use occurrence=N to target specific one

## PRO TIP
For files >1000 lines, ALWAYS:
1. search_files first
2. read_file with start_line/end_line to see context
3. edit_file with exact text
`)

	case "search":
		sb.WriteString(`# SEARCHING FILES

## Quick Reference
| Task                  | How                                          |
|-----------------------|----------------------------------------------|
| Find location         | search_files(path, pattern)                  |
| Count matches         | search_files(path, pattern, count_only=true) |
| Search with context   | search_files(path, pattern, include_context=true) |
| Find and replace all  | edit_file(path, mode:"search_replace", pattern, replacement) |

## Examples

# Find where a function is defined
search_files("/path/to/project", "def my_function")
-> Returns: "Found at lines 45-67 in file.py"

# Count how many TODOs exist
search_files("/path/file.py", "TODO", count_only=true)
-> Returns: "15 matches at lines: 10, 25, 48, ..."

# Search with surrounding context
search_files("/path", "error", include_context=true, context_lines=3)

# Replace all occurrences in multiple files
edit_file("/path/to/project", mode:"search_replace", pattern:"old_name", replacement:"new_name")

## WORKFLOW TIP
Always search BEFORE editing large files:
1. search_files -> find exact location
2. read_file with start_line/end_line -> see the context
3. edit_file -> make the change
`)

	case "batch":
		sb.WriteString(`# BATCH OPERATIONS

## When to Use
- Multiple file operations that should succeed or fail together
- Creating multiple files at once
- Complex refactoring across files
- Pipeline transformations

## Example: Batch Operations
batch_operations(request_json='{
  "operations": [
    {"type": "write", "path": "file1.txt", "content": "..."},
    {"type": "write", "path": "file2.txt", "content": "..."},
    {"type": "copy", "source": "file1.txt", "destination": "backup.txt"},
    {"type": "edit", "path": "file3.txt", "old_text": "x", "new_text": "y"}
  ],
  "atomic": true,
  "create_backup": true
}')

## Example: Pipeline
batch_operations(pipeline_json='{
  "name": "refactor",
  "steps": [
    {"id": "find", "action": "search", "params": {"pattern": "old"}},
    {"id": "fix", "action": "edit", "input_from": "find", "params": {"old_text": "old", "new_text": "new"}}
  ]
}')

## Example: Batch Rename
batch_operations(rename_json='{
  "path": "/dir", "mode": "find_replace",
  "find": "old", "replace": "new", "preview": true
}')

## Batch Operation Types
- write, edit, copy, move, delete, create_directory

## Pipeline Actions
- search, read_ranges, count_occurrences, edit, multi_edit
- regex_transform, copy, rename, delete, aggregate, diff, merge

## Options
- atomic: true = All succeed or all rollback
- create_backup: true = Backup before changes
- validate_only: true = Dry run (no changes)
`)

	case "errors":
		sb.WriteString(`# COMMON ERRORS AND FIXES

## "no match found for old_text"
CAUSE: The exact text doesn't exist in the file
FIXES:
1. Use search_files to verify the text exists
2. Check for whitespace/indentation differences
3. Copy the EXACT text from read_file output

## "context validation failed"
CAUSE: File was modified since you read it
FIX: Re-run search_files + read_file with start_line/end_line to get fresh content

## "multiple matches found"
CAUSE: Same text appears multiple times
FIX: Use edit_file with occurrence=N:
- occurrence=1 (first)
- occurrence=-1 (last)
- occurrence=2 (second), etc.

## "access denied" / "permission error"
CAUSE: Path not in allowed paths or file locked
FIXES:
1. Check --allowed-paths configuration
2. Close any programs using the file
3. Use list_directory to verify path exists

## "path not found"
CAUSE: Path format issue (WSL vs Windows)
FIX: All tools auto-convert paths:
- /mnt/c/Users/... <-> C:\Users\...

## "Tool not found: create_file"
FIX: Use write_file instead (it creates files too)
`)

	case "examples":
		sb.WriteString(`# PRACTICAL EXAMPLES

## Example 1: Edit a function in a large file
# Step 1: Find where the function is
search_files("src/app.py", "def calculate_total")
# -> "Found at lines 234-256"

# Step 2: Read only those lines
read_file("src/app.py", start_line=234, end_line=256)

# Step 3: Make the edit
edit_file("src/app.py",
  old_text="def calculate_total(items):",
  new_text="def calculate_total(items, tax_rate=0.1):")

## Example 2: Multiple edits in one file
multi_edit("src/config.py", edits_json='[
  {"old_text": "DEBUG = True", "new_text": "DEBUG = False"},
  {"old_text": "VERSION = \"1.0\"", "new_text": "VERSION = \"1.1\""},
  {"old_text": "API_URL = \"http://dev\"", "new_text": "API_URL = \"http://prod\""}
]')

## Example 3: Replace only the last TODO
edit_file("src/main.py", old_text="TODO", new_text="DONE", occurrence=-1)

## Example 4: Create multiple files atomically
batch_operations(request_json='{
  "operations": [
    {"type": "create_directory", "path": "src/components"},
    {"type": "write", "path": "src/components/Button.tsx", "content": "..."},
    {"type": "write", "path": "src/components/Input.tsx", "content": "..."}
  ],
  "atomic": true
}')

## Example 5: Count before replacing
# First, see how many matches
search_files("src/legacy.py", "old_api_call", count_only=true)
# -> "47 matches"

# If too many, be more specific or use occurrence=N
`)

	case "tips":
		sb.WriteString(`# PRO TIPS FOR EFFICIENCY

## 1. Never Read Large Files Entirely
GOOD: search_files -> read_file(start_line, end_line) -> edit_file
BAD:  read_file on 5000-line files (wastes 125k tokens!)

## 2. Use multi_edit for Multiple Changes
GOOD: One multi_edit call with 5 edits
BAD:  Five separate edit_file calls (5x slower)

## 3. Search Before Editing
GOOD: search_files first, then edit
BAD:  Guessing line numbers or text

## 4. Use count_only Before Bulk Replace
GOOD: search_files(count_only=true) to check how many matches first
BAD:  Blind replace that affects unexpected locations

## 5. Check Your Efficiency
server_info(action:"stats")
-> Goal: >80% targeted_edits
-> If <50%, you're not using the workflow correctly

## 6. Use occurrence for Precision
GOOD: edit_file with occurrence=1 or occurrence=-1
BAD:  edit_file when there are multiple matches

## 7. Batch Operations for Atomicity
GOOD: batch_operations with atomic=true
BAD:  Multiple operations that could partially fail

## 8. Dry Run Before Destructive Operations
GOOD: analyze_operation(operation:"edit") first
BAD:  delete_file without checking

## 9. Monitor Performance
server_info(action:"stats") -> See cache hit rate, ops/sec
-> Goal: >95% cache hit rate

## 10. Use Regex for Complex Transforms
edit_file(mode:"regex", patterns_json='[{"pattern":"(\\w+)Error","replacement":"${1}Exception"}]')
`)

	case "recovery":
		sb.WriteString(`# DISASTER RECOVERY

## Quick Undo (last edit)
backup(action:"undo_last")
-> Restores the most recent backup automatically
-> Use preview:true first to see what will be restored

## Undo Specific Edit
Every edit_file/multi_edit response includes:
  UNDO: backup(action:"restore", backup_id:"...")
Copy that command to restore.

## Find Backups for a File
backup(action:"list", filter_path:"filename.cs")
-> Shows all backups containing that file
-> Then: backup(action:"restore", backup_id:"...")

## Compare Before Restoring
backup(action:"compare", backup_id:"...", file_path:"path/to/file")
-> Shows diff between backup and current file

## Check System Status
server_info(action:"stats")
-> Shows backup directory, count, and latest backup

## Before Repairing Damage
1. backup(action:"list", filter_path:"broken-file") -> find clean backup
2. copy_file(source, "/tmp/manual-backup") -> extra safety copy
3. get_file_info(path) -> note file size as reference
4. search_files(path, pattern, count_only:true) -> count key elements
5. THEN start fixing

## Known Issues
- read_file with only start_line (no end_line) now reads to end of file (fixed in v4.1.2)
- If file seems truncated, verify with mode:"tail" before editing
- If >15% of file changes in one edit, STOP and verify

## Golden Rule
If edits make things WORSE, STOP editing and RESTORE from backup.
Repeated edits on a broken file make recovery harder.
`)

	case "all":
		// Return all topics
		sb.WriteString(getHelpContent("overview", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("workflow", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("tools", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("edit", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("errors", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("tips", compactMode))
		sb.WriteString("\n---\n\n")
		sb.WriteString(getHelpContent("recovery", compactMode))

	default:
		sb.WriteString(fmt.Sprintf(`# Unknown topic: "%s"

Available topics:
- overview  - Quick start guide
- workflow  - The 4-step efficient workflow
- tools     - Complete list of 16 tools
- read      - Reading files efficiently
- write     - Writing and creating files
- edit      - Editing files (most important!)
- search    - Finding content in files
- batch     - Multiple operations at once
- errors    - Common errors and fixes
- examples  - Code examples
- tips      - Pro tips for efficiency
- all       - Everything (long output)

Example: server_info(topic:"edit")
`, topic))
	}

	return sb.String()
}
