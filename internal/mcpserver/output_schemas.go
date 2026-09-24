package mcpserver

import "encoding/json"

// Output schemas (MCP outputSchema) for the core tools. Published once per
// session via tools/list; the descriptions here are the interop contract that
// replaces CLAUDE.md for third-party MCP clients.
//
// KEEP IN SYNC with editStructured / multiEditStructured / writeStructured /
// readStructured / searchStructured / listStructured / batchStructured /
// backupStructured. Guarded by output_schema_test.go and output_schema_sweep_test.go.

var readFileOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "content": {"type": "string", "description": "File body (possibly truncated; truncation is annotated inline and via truncated)"},
    "content_hash": {"type": "string", "description": "FNV-1a 8-hex hash of the FULL file bytes from the same snapshot as content. Pass as expected_hash on a subsequent edit. On batch reads this field is omitted at the top level; each files[] entry has its own content_hash."},
    "path": {"type": "string", "description": "Canonical path actually read (single-file reads)"},
    "status": {"type": "string", "description": "ok | partial | truncated | failed"},
    "truncated": {"type": "boolean", "description": "True when the returned body is not the full file (range/head/tail/auto-cap/line-width)"},
    "start_line": {"type": "integer", "description": "First line of a range/head/tail read (1-based, actual)"},
    "end_line": {"type": "integer", "description": "Last line returned (1-based, inclusive, clamped to file length)"},
    "total_lines": {"type": "integer", "description": "Total lines in the full file"},
    "files": {"type": "array", "description": "Per-file results for batch reads (paths). Item errors are listed here; status=partial when mixed.", "items": {"type": "object"}},
    "continuation": {"type": "object", "description": "How to read the rest when truncated (start_line, max_lines, hint)"}
  },
  "required": ["content"]
}`)

var writeFileOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Absolute canonical host path observed after the write"},
    "bytes_written": {"type": "integer", "description": "Actual byte length observed by reopening the final host file after hooks and EOL preservation"},
    "verified": {"type": "boolean", "description": "True only when the final host file was independently reopened and measured after the atomic write"},
    "content_hash": {"type": "string", "description": "FNV-1a 8-hex hash computed from the reopened host file. Pass as expected_hash on a subsequent edit_file/multi_edit to chain operations without re-reading."},
    "backup_id": {"type": "string", "description": "Present when a safety backup was auto-created (adaptive guard). Full ID for backup(action:'restore') or backup(action:'undo_last')."},
    "feedback": {"type": "string", "description": "Non-blocking warning (truncation/inflation/rewrite heuristics or failed post-write read-back)"},
    "status": {"type": "string", "description": "applied (write_file has no dry_run)"},
    "message": {"type": "string", "description": "Human-readable summary, identical to the text content block"}
  },
  "required": ["path", "bytes_written", "verified", "message"]
}`)

var editFileOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "replacements": {"type": "integer", "description": "Number of replacements applied"},
    "lines_added": {"type": "integer"},
    "lines_removed": {"type": "integer"},
    "total_lines": {"type": "integer", "description": "Total lines in the file after the edit"},
    "content_hash": {"type": "string", "description": "Post-edit FNV-1a 8-hex hash. Pass as expected_hash on the NEXT edit to chain edits without re-reading."},
    "backup_id": {"type": "string", "description": "Full backup ID for backup(action:'restore') / undo"},
    "parent_backup_id": {"type": "string", "description": "Previous backup in the undo chain (step-through undo)"},
    "risk_warning": {"type": "string"},
    "structure_warning": {"type": "string", "description": "Delimiter/balance warning introduced by this edit"},
    "integrity": {"type": "string", "description": "Post-edit integrity verification result (HIGH/CRITICAL ops)"},
    "external_change": {"type": "string", "description": "Auto-OCC notice: file changed on disk since last session read/write"},
    "match_method": {"type": "string", "description": "How old_text was matched: exact, tolerant_whitespace, fallback, regex, occurrence, search_replace, range, insert"},
    "status": {"type": "string", "description": "applied or simulated (dry_run)"},
    "current_hash": {"type": "string", "description": "Hash of the file before a dry_run (same as on-disk)"},
    "predicted_hash": {"type": "string", "description": "Hash the file would have after applying the dry_run"},
    "message": {"type": "string", "description": "Human-readable summary incl. diff, identical to the text content block"}
  },
  "required": ["path", "replacements", "lines_added", "lines_removed", "total_lines", "message"]
}`)

var multiEditOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "successful_edits": {"type": "integer"},
    "total_edits": {"type": "integer"},
    "lines_added": {"type": "integer"},
    "lines_removed": {"type": "integer"},
    "total_lines": {"type": "integer"},
    "content_hash": {"type": "string", "description": "Post-edit FNV-1a 8-hex hash (OCC token for the next edit)"},
    "backup_id": {"type": "string"},
    "parent_backup_id": {"type": "string"},
    "risk_warning": {"type": "string"},
    "structure_warning": {"type": "string"},
    "integrity": {"type": "string"},
    "status": {"type": "string", "description": "applied or simulated (dry_run)"},
    "current_hash": {"type": "string", "description": "Hash of the file before a dry_run"},
    "predicted_hash": {"type": "string", "description": "Hash the file would have after applying the dry_run"},
    "message": {"type": "string"}
  },
  "required": ["path", "successful_edits", "total_edits", "message"]
}`)

var searchFilesOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "description": "ok | empty | truncated"},
    "match_count": {"type": "integer", "description": "Exact total when the walk finished; otherwise the count known so far (not a guessed total). May exceed len(matches) when this page was capped after a complete scan."},
    "truncated": {"type": "boolean", "description": "True when more matches exist beyond this page or a byte budget cut the response"},
    "hidden_count": {"type": "integer", "description": "Entries skipped by .gitignore/.cursorignore/.fsultraignore (0 when no_ignore:true)"},
    "matches": {"type": "array", "description": "Structured hits {path, line, text}. Empty on filename-only/count_only when not parseable as line hits.", "items": {"type": "object"}},
    "scope": {"type": "object", "description": "Path/pattern/filters actually used"},
    "continuation": {"type": "object", "description": "When truncated: next page of the same path/pattern. next_offset and max_results are integers. hint is the readable instruction. Never means retry the same mutation.", "properties": {"next_offset": {"type": "integer"}, "max_results": {"type": "integer"}, "hint": {"type": "string"}}},
    "message": {"type": "string", "description": "Human-readable text fallback, identical to the text content block"}
  },
  "required": ["status", "match_count", "truncated", "hidden_count", "message"]
}`)

var listDirectoryOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "status": {"type": "string", "description": "ok | empty | truncated"},
    "truncated": {"type": "boolean"},
    "hidden_count": {"type": "integer", "description": "Entries skipped by gitignore/exclude (tree). Explicit on directory_tree."},
    "format": {"type": "string", "description": "compact | json | tree | sizes"},
    "entries": {"type": "array", "description": "Directory entries {name, type, size, modified} when available", "items": {"type": "object"}},
    "entry_count": {"type": "integer"},
    "continuation": {"type": "string"},
    "message": {"type": "string", "description": "Human-readable listing, identical to the text content block"}
  },
  "required": ["path", "status", "truncated", "message"]
}`)

var batchOperationsOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "description": "ok | partial | failed | simulated"},
    "kind": {"type": "string", "description": "request | pipeline | rename"},
    "success": {"type": "boolean"},
    "total": {"type": "integer"},
    "completed": {"type": "integer"},
    "failed": {"type": "integer"},
    "items": {"type": "array", "description": "Per-item result {index, type, path, success, error, skipped, dest}", "items": {"type": "object"}},
    "backup_id": {"type": "string"},
    "rollback_status": {"type": "string", "description": "complete | partial | failed"},
    "retryable": {"type": "boolean", "description": "False when any write may already have been applied — do not replay blindly"},
    "message": {"type": "string"}
  },
  "required": ["status", "success", "message"]
}`)

var listAllowedDirectoriesOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "description": "ok | empty (insecure-open with no roots listed)"},
    "paths": {"type": "array", "description": "Sandbox roots this server may read and write", "items": {"type": "string"}},
    "source": {"type": "string", "description": "How roots were chosen (flags, roots, insecure, ...)"},
    "insecure_open": {"type": "boolean", "description": "True when the sandbox is disabled"},
    "profile": {"type": "string", "description": "Tool catalog: strict | ultra"},
    "roots_mode": {"type": "string", "description": "How MCP client Roots combine with CLI paths: replace | union | ignore"},
    "readonly": {"type": "boolean", "description": "True when --readonly is set"},
    "tool_count": {"type": "integer", "description": "Number of tools registered on this server"},
    "version": {"type": "string", "description": "Server version. initialize already sends it; this is the copy the model sees."},
    "commit": {"type": "string", "description": "Build commit, or dev when go build did not stamp -ldflags."},
    "build_date": {"type": "string", "description": "Build date stamped at link time, or unknown."},
    "message": {"type": "string", "description": "Human-readable listing, identical to the text content block"}
  },
  "required": ["status", "paths", "version", "commit", "build_date", "message"]
}`)

var diffFilesOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "description": "ok | identical"},
    "path_a": {"type": "string"},
    "path_b": {"type": "string"},
    "identical": {"type": "boolean"},
    "added": {"type": "integer"},
    "removed": {"type": "integer"},
    "against": {"type": "string", "description": "Present when comparing against a backup"},
    "message": {"type": "string", "description": "Unified diff or identical notice, identical to the text content block"}
  },
  "required": ["status", "message"]
}`)

var applyPatchOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "description": "applied or simulated (dry_run)"},
    "path": {"type": "string"},
    "files": {"type": "array", "description": "Per-file results for a multi-file patch", "items": {"type": "object"}},
    "lines_added": {"type": "integer"},
    "lines_removed": {"type": "integer"},
    "content_hash": {"type": "string", "description": "Post-write OCC hash when applied and verified"},
    "backup_id": {"type": "string"},
    "current_hash": {"type": "string", "description": "On-disk hash before a dry_run"},
    "predicted_hash": {"type": "string", "description": "Hash the file would have after applying the dry_run"},
    "message": {"type": "string", "description": "Human-readable summary, identical to the text content block"}
  },
  "required": ["status", "path", "message"]
}`)

var backupOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "description": "ok | empty"},
    "action": {"type": "string"},
    "items": {"type": "array", "description": "Backup/trash entries for list-like actions", "items": {"type": "object"}},
    "backup_id": {"type": "string"},
    "message": {"type": "string"}
  },
  "required": ["status", "action", "message"]
}`)

var analyzeCodeOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "description": "ok | empty | unavailable | error | partial"},
    "action": {"type": "string", "description": "symbols | lint | sec | impact"},
    "findings": {"type": "array", "items": {"type": "object"}, "description": "Hits {path,line,col,symbol,kind,message,severity,suggestion}"},
    "truncated": {"type": "boolean"},
    "tool": {"type": "string", "description": "go-ast | go-vet | staticcheck | govulncheck | search"},
    "retryable": {"type": "boolean"},
    "message": {"type": "string", "description": "Text fallback, identical to the text content block"}
  },
  "required": ["status", "action", "findings", "truncated", "tool", "retryable", "message"]
}`)
