package core

import (
	"fmt"
	"sort"
	"strings"
)

// ParamType represents the expected JSON type of a tool parameter.
type ParamType int

const (
	ParamString  ParamType = iota
	ParamNumber            // JSON numbers arrive as float64
	ParamBoolean
)

func (t ParamType) String() string {
	switch t {
	case ParamString:
		return "string"
	case ParamNumber:
		return "number"
	case ParamBoolean:
		return "boolean"
	default:
		return "unknown"
	}
}

// ParamDef defines the expected type and requirement of a single parameter.
type ParamDef struct {
	Type     ParamType
	Required bool
}

// ToolParamSchema maps parameter names to their definitions.
type ToolParamSchema map[string]ParamDef

// toolSchemas is the authoritative registry of every parameter accepted by each
// MCP tool. Unknown parameters are rejected at validation time.
var toolSchemas = map[string]ToolParamSchema{
	// ---- CORE (5) ----
	"read_file": {
		"path":       {ParamString, true},
		"paths":      {ParamString, false}, // batch: JSON array of paths
		"max_lines":  {ParamNumber, false},
		"mode":       {ParamString, false},
		"start_line": {ParamNumber, false},
		"end_line":   {ParamNumber, false},
		"encoding":   {ParamString, false},
	},
	"write_file": {
		"path":           {ParamString, true},
		"content":        {ParamString, false},
		"content_base64": {ParamString, false},
		"encoding":       {ParamString, false},
	},
	"edit_file": {
		"path":           {ParamString, true},
		"old_text":       {ParamString, false},
		"new_text":       {ParamString, false},
		"old_str":        {ParamString, false}, // alias → old_text (normalizer)
		"new_str":        {ParamString, false}, // alias → new_text (normalizer)
		"force":          {ParamBoolean, false},
		"mode":           {ParamString, false},
		"occurrence":     {ParamNumber, false},
		"pattern":        {ParamString, false},
		"replacement":    {ParamString, false},
		"patterns_json":  {ParamString, false},
		"case_sensitive": {ParamBoolean, false},
		"create_backup":  {ParamBoolean, false},
		"dry_run":        {ParamBoolean, false},
		"whole_word":     {ParamBoolean, false},
	},
	"list_directory": {
		"path": {ParamString, true},
	},
	"search_files": {
		"path":            {ParamString, true},
		"pattern":         {ParamString, true},
		"include_content": {ParamBoolean, false},
		"file_types":      {ParamString, false},
		"case_sensitive":  {ParamBoolean, false},
		"whole_word":      {ParamBoolean, false},
		"include_context": {ParamBoolean, false},
		"context_lines":   {ParamNumber, false},
		"count_only":      {ParamBoolean, false},
		"return_lines":    {ParamBoolean, false}, // normalizer coerces string→bool
	},

	// ---- EDIT+ (1) ----
	"multi_edit": {
		"path":       {ParamString, true},
		"edits_json": {ParamString, true},
		"force":      {ParamBoolean, false},
	},

	// ---- FILES (4) ----
	"move_file": {
		"source_path": {ParamString, true},
		"dest_path":   {ParamString, true},
	},
	"copy_file": {
		"source_path": {ParamString, true},
		"dest_path":   {ParamString, true},
	},
	"delete_file": {
		"path":      {ParamString, true},
		"paths":     {ParamString, false}, // batch: JSON array of paths
		"permanent": {ParamBoolean, false},
	},
	"create_directory": {
		"path": {ParamString, true},
	},

	// ---- BATCH (1) ----
	"batch_operations": {
		"request_json":  {ParamString, false},
		"pipeline_json": {ParamString, false},
		"rename_json":   {ParamString, false},
	},

	// ---- BACKUP (1) ----
	"backup": {
		"action":           {ParamString, false},
		"backup_id":        {ParamString, false},
		"file_path":        {ParamString, false},
		"limit":            {ParamNumber, false},
		"filter_operation": {ParamString, false},
		"filter_path":      {ParamString, false},
		"newer_than_hours": {ParamNumber, false},
		"older_than_days":  {ParamNumber, false},
		"dry_run":          {ParamBoolean, false},
		"preview":          {ParamBoolean, false},
	},

	// ---- ANALYSIS (1) ----
	"analyze_operation": {
		"operation": {ParamString, true},
		"path":      {ParamString, true},
		"content":   {ParamString, false},
		"old_text":  {ParamString, false},
		"new_text":  {ParamString, false},
	},

	// ---- WSL (1) ----
	"wsl": {
		"action":         {ParamString, false},
		"wsl_path":       {ParamString, false},
		"windows_path":   {ParamString, false},
		"direction":      {ParamString, false},
		"create_dirs":    {ParamBoolean, false},
		"filter_pattern": {ParamString, false},
		"dry_run":        {ParamBoolean, false},
		"enabled":        {ParamBoolean, false},
		"sync_on_write":  {ParamBoolean, false},
		"sync_on_edit":   {ParamBoolean, false},
		"silent":         {ParamBoolean, false},
	},

	// ---- UTIL (1) ----
	"server_info": {
		"action":     {ParamString, false},
		"topic":      {ParamString, false},
		"sub_action": {ParamString, false},
		"content":    {ParamString, false},
		"path":       {ParamString, false},
	},

	// ---- INFO (1) ----
	"get_file_info": {
		"path":  {ParamString, true},
		"paths": {ParamString, false}, // batch: JSON array of paths
	},

	// ---- ALIASES ----
	"search": {
		"path":            {ParamString, true},
		"pattern":         {ParamString, true},
		"include_content": {ParamBoolean, false},
		"file_types":      {ParamString, false},
		"case_sensitive":  {ParamBoolean, false},
		"whole_word":      {ParamBoolean, false},
		"include_context": {ParamBoolean, false},
		"context_lines":   {ParamNumber, false},
		"count_only":      {ParamBoolean, false},
		"return_lines":    {ParamBoolean, false},
	},
	"edit": {
		"path":           {ParamString, true},
		"old_text":       {ParamString, false},
		"new_text":       {ParamString, false},
		"old_str":        {ParamString, false},
		"new_str":        {ParamString, false},
		"force":          {ParamBoolean, false},
		"mode":           {ParamString, false},
		"occurrence":     {ParamNumber, false},
		"pattern":        {ParamString, false},
		"replacement":    {ParamString, false},
		"patterns_json":  {ParamString, false},
		"case_sensitive": {ParamBoolean, false},
		"create_backup":  {ParamBoolean, false},
		"dry_run":        {ParamBoolean, false},
		"whole_word":     {ParamBoolean, false},
	},
	"write": {
		"path":           {ParamString, true},
		"content":        {ParamString, false},
		"content_base64": {ParamString, false},
		"encoding":       {ParamString, false},
	},
	"help": {
		"topic": {ParamString, false},
	},
}

// ValidateToolParams checks the incoming arguments against the tool's schema.
// Returns nil if everything is valid, or a list of human-readable errors.
func ValidateToolParams(toolName string, args map[string]interface{}) []string {
	schema, ok := toolSchemas[toolName]
	if !ok {
		return nil // no schema registered → skip validation
	}

	var errs []string

	// 1. Reject unknown parameters
	for k := range args {
		if _, known := schema[k]; !known {
			errs = append(errs, fmt.Sprintf("unknown parameter %q (valid: %s)", k, knownParamNames(schema)))
		}
	}

	// 2. Check required parameters
	for name, def := range schema {
		if def.Required {
			if _, present := args[name]; !present {
				// Only flag missing required if there's no batch alternative
				// e.g., read_file requires "path" OR "paths"
				if name == "path" {
					if _, hasPaths := args["paths"]; hasPaths {
						continue
					}
				}
				errs = append(errs, fmt.Sprintf("missing required parameter %q", name))
			}
		}
	}

	// 3. Type-check present parameters
	for k, v := range args {
		def, known := schema[k]
		if !known {
			continue // already flagged above
		}
		if !typeMatches(def.Type, v) {
			errs = append(errs, fmt.Sprintf("parameter %q: expected %s, got %T", k, def.Type, v))
		}
	}

	return errs
}

// typeMatches checks if the value v has the expected JSON type.
func typeMatches(expected ParamType, v interface{}) bool {
	switch expected {
	case ParamString:
		_, ok := v.(string)
		return ok
	case ParamNumber:
		_, ok := v.(float64)
		return ok
	case ParamBoolean:
		_, ok := v.(bool)
		return ok
	}
	return true
}

// knownParamNames returns a sorted, comma-separated list of valid parameter names.
func knownParamNames(schema ToolParamSchema) string {
	names := make([]string, 0, len(schema))
	for k := range schema {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
