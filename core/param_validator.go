package core

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ParamType represents the expected JSON type of a tool parameter.
type ParamType int

const (
	ParamString ParamType = iota
	ParamNumber           // JSON numbers arrive as float64
	ParamBoolean
	ParamArray
	ParamObject
	ParamStringOrArray // E4: native array or legacy JSON-array string
)

func (t ParamType) String() string {
	switch t {
	case ParamString:
		return "string"
	case ParamNumber:
		return "number"
	case ParamBoolean:
		return "boolean"
	case ParamArray:
		return "array"
	case ParamObject:
		return "object"
	case ParamStringOrArray:
		return "array or JSON string"
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
		"path":            {Type: ParamString, Required: true},
		"paths":           {Type: ParamStringOrArray},
		"max_lines":       {Type: ParamNumber},
		"max_line_length": {Type: ParamNumber},
		"mode":            {Type: ParamString},
		"start_line":      {Type: ParamNumber},
		"end_line":        {Type: ParamNumber},
		"encoding":        {Type: ParamString},
	},
	"write_file": {
		"path":           {Type: ParamString, Required: true},
		"content":        {Type: ParamString},
		"content_base64": {Type: ParamString},
		"encoding":       {Type: ParamString},
		"mode":           {Type: ParamString},
	},
	"diff_files": {
		"path_a":  {ParamString, false},
		"path_b":  {ParamString, false},
		"path":    {ParamString, false},
		"against": {ParamString, false},
	},
	"apply_patch": {
		"path":          {ParamString, true},
		"patch":         {ParamString, true},
		"dry_run":       {ParamBoolean, false},
		"expected_hash": {ParamString, false},
		"allow_rewrite": {ParamBoolean, false},
		"create_backup": {ParamBoolean, false},
	},
	"edit_file": {
		"path":                {Type: ParamString, Required: true},
		"old_text":            {Type: ParamString},
		"new_text":            {Type: ParamString},
		"old_str":             {Type: ParamString},
		"new_str":             {Type: ParamString},
		"force":               {Type: ParamBoolean},
		"allow_rewrite":       {Type: ParamBoolean},
		"mode":                {Type: ParamString},
		"occurrence":          {Type: ParamNumber},
		"start_line":          {Type: ParamNumber},
		"end_line":            {Type: ParamNumber},
		"line_count":          {Type: ParamNumber},
		"pattern":             {Type: ParamString},
		"replacement":         {Type: ParamString},
		"patterns":            {Type: ParamArray},
		"patterns_json":       {Type: ParamString},
		"case_sensitive":      {Type: ParamBoolean},
		"create_backup":       {Type: ParamBoolean},
		"dry_run":             {Type: ParamBoolean},
		"diff_format":         {Type: ParamString},
		"whole_word":          {Type: ParamBoolean},
		"expected_hash":       {Type: ParamString},
		"tolerant_whitespace": {Type: ParamBoolean},
		"anchor":              {Type: ParamString},
		"position":            {Type: ParamString},
		"strict":              {Type: ParamBoolean},
		"expected_matches":    {Type: ParamNumber},
	},
	"list_directory": {
		"path":           {ParamString, true},
		"output_format":  {ParamString, false},
		"max_depth":      {ParamNumber, false},
		"exclude":        {ParamString, false},
		"respect_ignore": {ParamBoolean, false},
		"max_nodes":      {ParamNumber, false},
	},
	"directory_tree": {
		"path":           {ParamString, true},
		"output_format":  {ParamString, false},
		"max_depth":      {ParamNumber, false},
		"exclude":        {ParamString, false},
		"respect_ignore": {ParamBoolean, false},
		"max_nodes":      {ParamNumber, false},
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
		"return_lines":    {ParamBoolean, false}, // bool or string "true"/"false"
		"include":         {ParamString, false},  // glob pattern (alias for file_types)
		"output_format":   {ParamString, false},  // "text" or "json"
		"output":          {ParamString, false},  // alias for output_format
		"max_results":     {ParamNumber, false},  // cap filenames returned (v4.5.26, fix #3)
		"no_ignore":       {ParamBoolean, false},
	},

	// ---- EDIT+ (1) ----
	"multi_edit": {
		"path":                {Type: ParamString, Required: true},
		"edits":               {Type: ParamArray},
		"edits_json":          {Type: ParamString, Required: true},
		"force":               {Type: ParamBoolean},
		"tolerant_whitespace": {Type: ParamBoolean},
		"dry_run":             {Type: ParamBoolean},
		"expected_hash":       {Type: ParamString},
		"diff_format":         {Type: ParamString},
		"strict":              {Type: ParamBoolean},
		"expected_matches":    {Type: ParamNumber},
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
		"paths":     {Type: ParamStringOrArray},
		"permanent": {ParamBoolean, false},
	},
	"create_directory": {
		"path": {ParamString, true},
	},

	// ---- BATCH (1) ----
	"batch_operations": {
		"request":       {Type: ParamObject},
		"request_json":  {Type: ParamString},
		"pipeline":      {Type: ParamObject},
		"pipeline_json": {Type: ParamString},
		"rename":        {Type: ParamObject},
		"rename_json":   {Type: ParamString},
	},

	// ---- BACKUP (1) ----
	"backup": {
		"action":           {ParamString, false},
		"backup_id":        {ParamString, false},
		"sd_id":            {ParamString, false},
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
		"paths": {Type: ParamStringOrArray},
	},

	// ---- VERSION CONTROL (1) ----
	"git": {
		"action":    {ParamString, true},
		"path":      {ParamString, false},
		"paths":     {ParamArray, false},
		"output":    {ParamString, false}, // "stat" | "name-only" | "full" (diff); "name-only" | "full" (status); "oneline" | "full" (log); "stat" | "name-only" | "full" (show)
		"max_lines": {ParamNumber, false}, // default 200
		"limit":     {ParamNumber, false}, // log: default 10
		"rev":       {ParamString, false}, // single rev or range; replaces commit_range + source
		"staged":    {ParamBoolean, false},
		"message":   {ParamString, false},
		"name":      {ParamString, false},  // branch: list when empty
		"checkout":  {ParamBoolean, false}, // branch: true → git switch
		"delete":    {ParamBoolean, false}, // branch: true → git branch -d (required to delete)
		"force":     {ParamBoolean, false}, // branch: with delete, true → -D; push: --force-with-lease
		"remote":    {ParamString, false},  // push/fetch: remote name (default origin)
		"prune":     {ParamBoolean, false}, // fetch: true → --prune
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
		"include":         {ParamString, false},
		"output_format":   {ParamString, false},
		"output":          {ParamString, false},
	},
	"edit": {
		"path":                {ParamString, true},
		"old_text":            {ParamString, false},
		"new_text":            {ParamString, false},
		"old_str":             {ParamString, false},
		"new_str":             {ParamString, false},
		"force":               {ParamBoolean, false},
		"mode":                {ParamString, false},
		"occurrence":          {ParamNumber, false},
		"pattern":             {ParamString, false},
		"replacement":         {ParamString, false},
		"patterns_json":       {ParamString, false},
		"case_sensitive":      {ParamBoolean, false},
		"create_backup":       {ParamBoolean, false},
		"dry_run":             {ParamBoolean, false},
		"whole_word":          {ParamBoolean, false},
		"expected_hash":       {ParamString, false},  // B3 alias
		"tolerant_whitespace": {ParamBoolean, false}, // mirror of edit_file
	},
	"write": {
		"path":           {ParamString, true},
		"content":        {ParamString, false},
		"content_base64": {ParamString, false},
		"encoding":       {ParamString, false},
	},
	"help": {
		"topic": {ParamString, false},
		"tool":  {ParamString, false},
	},
	"list_allowed_directories": {},
	"minify_js": {
		"path":                {ParamString, true},
		"output_path":         {ParamString, false},
		"remove_comments":     {ParamBoolean, false},
		"collapse_whitespace": {ParamBoolean, false},
		"single_line":         {ParamBoolean, false},
		"create_backup":       {ParamBoolean, false},
		"dry_run":             {ParamBoolean, false},
	},
	"project_replace": {
		"path":           {ParamString, true},
		"find":           {ParamString, true},
		"replace":        {ParamString, true},
		"literal":        {ParamBoolean, false},
		"case_sensitive": {ParamBoolean, false},
		"file_types":     {ParamString, false},
		"include_paths":  {ParamString, false},
		"exclude_paths":  {ParamString, false},
		"preview":        {ParamBoolean, false},
		"create_backup":  {ParamBoolean, false},
		"parallel":       {ParamBoolean, false},
		"max_files":      {ParamNumber, false},
		"force":          {ParamBoolean, false},
	},
}

// benignParams are metadata parameters some clients attach to tool calls
// (e.g. a human-readable "description" of the edit, sent by opencode/Claude
// Code style harnesses). They carry no semantics for the engine, so they are
// accepted and silently ignored instead of failing the whole call — the proxy
// logs showed ~1 in 5 validation errors came from this single parameter.
var benignParams = map[string]bool{
	"description": true,
}

// ValidateToolParams checks the incoming arguments against the tool's schema.
// Returns nil if everything is valid, or a list of human-readable errors.
func ValidateToolParams(toolName string, args map[string]interface{}) []string {
	schema, ok := toolSchemas[toolName]
	if !ok {
		return nil // no schema registered → skip validation
	}

	var errs []string
	if c, ok := contracts[toolName]; ok {
		for _, group := range c.AnyOf {
			found := false
			for _, k := range group {
				if _, ok := args[k]; ok {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("missing one of %s", strings.Join(group, ", ")))
			}
		}
	}

	// 1. Reject unknown parameters
	for k := range args {
		if _, known := schema[k]; !known {
			if benignParams[k] {
				continue
			}
			msg := fmt.Sprintf("unknown parameter %q", k)
			if suggestion := closestParam(k, schema); suggestion != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", suggestion)
			}
			msg += fmt.Sprintf(" (valid: %s)", knownParamNames(schema))
			errs = append(errs, msg)
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
				if name == "edits_json" {
					if _, hasEdits := args["edits"]; hasEdits {
						continue
					}
				}
				if c, ok := contracts[toolName]; ok && inAnyOf(name, c.AnyOf) {
					continue
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
			errs = append(errs, fmt.Sprintf("parameter %q: expected %s, got %T — convert the value or use the documented field", k, def.Type, v))
			continue
		}
		if c, ok := contracts[toolName]; ok {
			if p, ok := c.Params[k]; ok && len(p.Enum) > 0 {
				if s, ok := v.(string); ok && s != "" && !enumHas(p.Enum, s) {
					errs = append(errs, fmt.Sprintf("parameter %q: invalid value %q (valid: %s)", k, s, strings.Join(p.Enum, ", ")))
				}
			}
		}
	}

	errs = append(errs, validateCombinations(toolName, args)...)
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
		if _, ok := v.(bool); ok {
			return true
		}
		// Also accept string "true"/"false" for params that coerce from string
		if s, ok := v.(string); ok {
			return s == "true" || s == "false"
		}
		return false
	case ParamArray:
		switch v.(type) {
		case []interface{}, []string:
			return true
		default:
			return false
		}
	case ParamObject:
		_, ok := v.(map[string]interface{})
		return ok
	case ParamStringOrArray:
		if _, ok := v.(string); ok {
			return true
		}
		switch v.(type) {
		case []interface{}, []string:
			return true
		default:
			return false
		}
	}
	return true
}

func enumHas(enum []string, v string) bool {
	for _, e := range enum {
		if e == v {
			return true
		}
	}
	return false
}

func validateCombinations(tool string, args map[string]interface{}) []string {
	var errs []string
	intField := func(name string, min float64) {
		v, ok := args[name]
		if !ok {
			return
		}
		f, ok := v.(float64)
		if !ok {
			return
		}
		if f != math.Trunc(f) {
			errs = append(errs, fmt.Sprintf("parameter %q: must be an integer, got %v", name, f))
			return
		}
		if f < min {
			errs = append(errs, fmt.Sprintf("parameter %q: must be >= %g, got %v", name, min, f))
		}
	}
	intField("start_line", 1)
	intField("end_line", 1)
	intField("line_count", 1)
	intField("max_lines", 0)
	intField("max_line_length", 0)
	intField("expected_matches", 1)
	intField("max_depth", 0)
	intField("max_nodes", 1)
	intField("context_lines", 0)
	intField("max_results", 1)
	if _, hasEnd := args["end_line"]; hasEnd {
		if _, hasCount := args["max_lines"]; hasCount && tool == "read_file" {
			errs = append(errs, `incompatible parameters "end_line" and "max_lines": use end_line (absolute) OR start_line+max_lines (count), not both`)
		}
		if _, hasCount := args["line_count"]; hasCount && tool == "edit_file" {
			errs = append(errs, `incompatible parameters "end_line" and "line_count": use one`)
		}
	}
	if tool == "batch_operations" {
		families := 0
		if hasAny(args, "request", "request_json") {
			families++
		}
		if hasAny(args, "pipeline", "pipeline_json") {
			families++
		}
		if hasAny(args, "rename", "rename_json") {
			families++
		}
		if families > 1 {
			errs = append(errs, `incompatible batch payload: provide only one of request/request_json, pipeline/pipeline_json, or rename/rename_json`)
		}
	}
	return errs
}

func hasAny(args map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		if v, ok := args[k]; ok && v != nil {
			if s, isStr := v.(string); isStr && s == "" {
				continue
			}
			return true
		}
	}
	return false
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

// closestParam returns the schema parameter with the smallest Levenshtein
// distance to name, or "" if none is close enough (distance > 2). Used to
// suggest corrections for typos like "include>" → "include".
func closestParam(name string, schema ToolParamSchema) string {
	best := ""
	bestDist := 3 // strictly less than this threshold wins
	for k := range schema {
		if d := levenshtein(name, k); d < bestDist {
			bestDist = d
			best = k
		}
	}
	return best
}

// levenshtein computes the edit distance between a and b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if a > b {
		a = b
	}
	if a > c {
		a = c
	}
	return a
}
