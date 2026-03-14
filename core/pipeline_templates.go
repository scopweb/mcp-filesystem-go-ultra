package core

import (
	"fmt"
	"regexp"
	"strings"
)

// templatePattern matches {{step_id.field}} references
var templatePattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_-]+)\.([a-zA-Z_]+)\}\}`)

// Supported template fields
const (
	FieldCount      = "count"       // Total count (sum of Counts map or len(FilesMatched))
	FieldFilesCount = "files_count" // Number of files matched
	FieldFiles      = "files"       // Comma-separated list of matched files
	FieldRisk       = "risk"        // Risk level string
	FieldEdits      = "edits"       // Number of edits applied
)

// ResolveTemplates walks a params map and replaces {{step_id.field}} references
// with actual values from prior step results
func ResolveTemplates(params map[string]interface{}, pCtx *PipelineContext) map[string]interface{} {
	if params == nil {
		return nil
	}
	resolved := make(map[string]interface{}, len(params))
	for k, v := range params {
		resolved[k] = resolveValue(v, pCtx)
	}
	return resolved
}

// resolveValue recursively resolves template references in a value
func resolveValue(v interface{}, pCtx *PipelineContext) interface{} {
	switch val := v.(type) {
	case string:
		return resolveString(val, pCtx)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = resolveValue(item, pCtx)
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, item := range val {
			result[k] = resolveValue(item, pCtx)
		}
		return result
	default:
		return v
	}
}

// resolveString replaces all {{step_id.field}} in a string
func resolveString(s string, pCtx *PipelineContext) string {
	if !strings.Contains(s, "{{") {
		return s
	}

	return templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := templatePattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match // no replacement
		}

		stepID := parts[1]
		field := parts[2]

		ref, exists := pCtx.GetStepResult(stepID)
		if !exists {
			return match // leave unresolved
		}

		return resolveField(ref, field, match)
	})
}

// resolveField extracts a field value from a StepResult
func resolveField(ref *StepResult, field string, fallback string) string {
	switch field {
	case FieldCount:
		total := 0
		for _, c := range ref.Counts {
			total += c
		}
		if len(ref.Counts) == 0 {
			total = len(ref.FilesMatched)
		}
		return fmt.Sprintf("%d", total)

	case FieldFilesCount:
		return fmt.Sprintf("%d", len(ref.FilesMatched))

	case FieldFiles:
		return strings.Join(ref.FilesMatched, ",")

	case FieldRisk:
		if ref.RiskLevel != "" {
			return ref.RiskLevel
		}
		return "LOW"

	case FieldEdits:
		return fmt.Sprintf("%d", ref.EditsApplied)

	default:
		return fallback
	}
}
