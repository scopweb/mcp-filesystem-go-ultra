package core

import (
	"fmt"
	"os"
	"strconv"
)

// StepCondition defines when a step should execute
type StepCondition struct {
	Type    string `json:"type"`               // Condition type
	StepRef string `json:"step_ref,omitempty"` // Reference to a prior step ID
	Value   string `json:"value,omitempty"`    // Comparison value (for count_gt, count_lt, count_eq)
	Path    string `json:"path,omitempty"`     // File path (for file_exists, file_not_exists)
}

// Supported condition types
const (
	CondHasMatches    = "has_matches"
	CondNoMatches     = "no_matches"
	CondCountGT       = "count_gt"
	CondCountLT       = "count_lt"
	CondCountEQ       = "count_eq"
	CondFileExists    = "file_exists"
	CondFileNotExists = "file_not_exists"
	CondStepSucceeded = "step_succeeded"
	CondStepFailed    = "step_failed"
)

var validConditionTypes = map[string]bool{
	CondHasMatches:    true,
	CondNoMatches:     true,
	CondCountGT:       true,
	CondCountLT:       true,
	CondCountEQ:       true,
	CondFileExists:    true,
	CondFileNotExists: true,
	CondStepSucceeded: true,
	CondStepFailed:    true,
}

// ValidateCondition validates a step condition's fields
func ValidateCondition(cond *StepCondition, stepID string, priorStepIDs map[string]int) error {
	if cond == nil {
		return nil
	}

	if !validConditionTypes[cond.Type] {
		return &PipelineStepError{
			StepID:  stepID,
			Action:  "condition",
			Param:   "type",
			Message: fmt.Sprintf("unsupported condition type '%s'", cond.Type),
		}
	}

	// Conditions that require step_ref
	switch cond.Type {
	case CondHasMatches, CondNoMatches, CondCountGT, CondCountLT, CondCountEQ, CondStepSucceeded, CondStepFailed:
		if cond.StepRef == "" {
			return &PipelineStepError{
				StepID:  stepID,
				Action:  "condition",
				Param:   "step_ref",
				Message: fmt.Sprintf("condition type '%s' requires step_ref", cond.Type),
			}
		}
		if _, exists := priorStepIDs[cond.StepRef]; !exists {
			return &PipelineStepError{
				StepID:     stepID,
				Action:     "condition",
				Param:      "step_ref",
				Message:    fmt.Sprintf("step_ref '%s' not found in prior steps", cond.StepRef),
				Suggestion: "step_ref must reference a step defined before this one",
			}
		}
	}

	// Conditions that require value
	switch cond.Type {
	case CondCountGT, CondCountLT, CondCountEQ:
		if cond.Value == "" {
			return &PipelineStepError{
				StepID:  stepID,
				Action:  "condition",
				Param:   "value",
				Message: fmt.Sprintf("condition type '%s' requires value", cond.Type),
			}
		}
		if _, err := strconv.Atoi(cond.Value); err != nil {
			return &PipelineStepError{
				StepID:  stepID,
				Action:  "condition",
				Param:   "value",
				Message: fmt.Sprintf("value '%s' must be an integer", cond.Value),
			}
		}
	}

	// Conditions that require path
	switch cond.Type {
	case CondFileExists, CondFileNotExists:
		if cond.Path == "" {
			return &PipelineStepError{
				StepID:  stepID,
				Action:  "condition",
				Param:   "path",
				Message: fmt.Sprintf("condition type '%s' requires path", cond.Type),
			}
		}
	}

	return nil
}

// EvaluateCondition evaluates whether a step should run based on its condition
// Returns (shouldRun, reason)
func EvaluateCondition(cond *StepCondition, pCtx *PipelineContext) (bool, string) {
	if cond == nil {
		return true, ""
	}

	switch cond.Type {
	case CondHasMatches:
		ref, exists := pCtx.GetStepResult(cond.StepRef)
		if !exists {
			return false, fmt.Sprintf("referenced step '%s' not found", cond.StepRef)
		}
		if len(ref.FilesMatched) > 0 {
			return true, ""
		}
		return false, fmt.Sprintf("step '%s' had no matches", cond.StepRef)

	case CondNoMatches:
		ref, exists := pCtx.GetStepResult(cond.StepRef)
		if !exists {
			return false, fmt.Sprintf("referenced step '%s' not found", cond.StepRef)
		}
		if len(ref.FilesMatched) == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("step '%s' had %d matches", cond.StepRef, len(ref.FilesMatched))

	case CondCountGT:
		return evalCountCondition(cond, pCtx, func(total, threshold int) bool { return total > threshold })

	case CondCountLT:
		return evalCountCondition(cond, pCtx, func(total, threshold int) bool { return total < threshold })

	case CondCountEQ:
		return evalCountCondition(cond, pCtx, func(total, threshold int) bool { return total == threshold })

	case CondFileExists:
		path := NormalizePath(cond.Path)
		if _, err := os.Stat(path); err == nil {
			return true, ""
		}
		return false, fmt.Sprintf("file '%s' does not exist", cond.Path)

	case CondFileNotExists:
		path := NormalizePath(cond.Path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return true, ""
		}
		return false, fmt.Sprintf("file '%s' exists", cond.Path)

	case CondStepSucceeded:
		ref, exists := pCtx.GetStepResult(cond.StepRef)
		if !exists {
			return false, fmt.Sprintf("referenced step '%s' not found", cond.StepRef)
		}
		if ref.Success {
			return true, ""
		}
		return false, fmt.Sprintf("step '%s' did not succeed", cond.StepRef)

	case CondStepFailed:
		ref, exists := pCtx.GetStepResult(cond.StepRef)
		if !exists {
			return false, fmt.Sprintf("referenced step '%s' not found", cond.StepRef)
		}
		if !ref.Success {
			return true, ""
		}
		return false, fmt.Sprintf("step '%s' succeeded", cond.StepRef)
	}

	return false, fmt.Sprintf("unknown condition type '%s'", cond.Type)
}

// evalCountCondition evaluates count-based conditions (count_gt, count_lt, count_eq)
func evalCountCondition(cond *StepCondition, pCtx *PipelineContext, compare func(total, threshold int) bool) (bool, string) {
	ref, exists := pCtx.GetStepResult(cond.StepRef)
	if !exists {
		return false, fmt.Sprintf("referenced step '%s' not found", cond.StepRef)
	}

	threshold, _ := strconv.Atoi(cond.Value) // already validated

	// Sum all counts from the referenced step
	total := 0
	for _, c := range ref.Counts {
		total += c
	}
	// If no counts map, fall back to files matched count
	if ref.Counts == nil || len(ref.Counts) == 0 {
		total = len(ref.FilesMatched)
	}

	if compare(total, threshold) {
		return true, ""
	}
	return false, fmt.Sprintf("count %d did not satisfy condition (threshold: %s)", total, cond.Value)
}
