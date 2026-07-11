package main

import "encoding/json"

// Output schemas (MCP outputSchema) for the core tools. Published once per
// session via tools/list; the descriptions here are the interop contract that
// replaces CLAUDE.md for third-party MCP clients.
//
// KEEP IN SYNC with editStructured / multiEditStructured / writeStructured /
// the read_file handlers. Guarded by output_schema_test.go.

var readFileOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "content": {"type": "string", "description": "File body (possibly truncated; truncation is annotated inline)"},
    "content_hash": {"type": "string", "description": "FNV-1a 8-hex hash of the FULL file on disk. Pass as expected_hash on a subsequent edit_file/multi_edit to detect concurrent external changes (OCC). Absent on multi-file reads."}
  },
  "required": ["content"]
}`)

var writeFileOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Absolute normalized path the file was written to"},
    "bytes_written": {"type": "integer"},
    "content_hash": {"type": "string", "description": "FNV-1a 8-hex hash of the file as written. Pass as expected_hash on a subsequent edit_file/multi_edit to chain operations without re-reading."},
    "backup_id": {"type": "string", "description": "Present when a safety backup was auto-created (adaptive guard). Full ID for backup(action:'restore') or backup(action:'undo_last')."},
    "feedback": {"type": "string", "description": "Non-blocking warning (truncation/inflation/rewrite heuristics)"},
    "message": {"type": "string", "description": "Human-readable summary, identical to the text content block"}
  },
  "required": ["path", "bytes_written", "message"]
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
    "message": {"type": "string"}
  },
  "required": ["path", "successful_edits", "total_edits", "message"]
}`)