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
    "content_hash": {"type": "string", "description": "FNV-1a 8-hex hash of the FULL file on disk. Pass as expected_hash on a subsequent edit_file/multi_edit to detect concurrent external changes (OCC). Absent on multi-file reads."},
    "path": {"type": "string", "description": "Canonical path actually read (single-file reads)"},
    "status": {"type": "string", "description": "ok | partial | truncated | failed"},
    "truncated": {"type": "boolean", "description": "True when the returned body is not the full file (range/head/tail/auto-cap/line-width)"},
    "start_line": {"type": "integer", "description": "First line of a range/head/tail read (1-based)"},
    "end_line": {"type": "integer", "description": "Last line of a range read (1-based, inclusive)"},
    "files": {"type": "array", "description": "Per-file results for batch reads (paths). Item errors are listed here; status=partial when mixed.", "items": {"type": "object"}},
    "continuation": {"type": "object", "description": "How to read the rest when truncated (start_line + hint)"}
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
    "match_count": {"type": "integer", "description": "Number of matches (may exceed len(matches) when truncated)"},
    "truncated": {"type": "boolean", "description": "True when output was capped (max_results or response budget)"},
    "matches": {"type": "array", "description": "Structured hits {path, line, text}. Empty on filename-only/count_only when not parseable as line hits.", "items": {"type": "object"}},
    "scope": {"type": "object", "description": "Path/pattern/filters actually used"},
    "continuation": {"type": "string", "description": "How to continue when truncated. Never means 'retry the same mutation'."},
    "message": {"type": "string", "description": "Human-readable text fallback, identical to the text content block"}
  },
  "required": ["status", "match_count", "truncated", "message"]
}`)

var listDirectoryOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "status": {"type": "string", "description": "ok | empty | truncated"},
    "truncated": {"type": "boolean"},
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
