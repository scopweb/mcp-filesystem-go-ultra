package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PipelineRequest represents a multi-step file transformation pipeline
type PipelineRequest struct {
	Name         string         `json:"name"`          // Required: pipeline name
	StopOnError  bool           `json:"stop_on_error"` // Default: true - stop on first error
	DryRun       bool           `json:"dry_run"`       // Default: false - preview changes without applying
	CreateBackup bool           `json:"create_backup"` // Default: true if destructive steps present
	Force        bool           `json:"force"`         // Bypass risk warnings
	Verbose      bool           `json:"verbose"`       // Return intermediate data (contents, per-file counts)
	Parallel     bool           `json:"parallel,omitempty"` // Enable parallel execution via DAG scheduling
	Steps        []PipelineStep `json:"steps"`         // Pipeline steps to execute
	validated    bool           // Internal: validation cache
}

// PipelineStep represents a single operation in the pipeline.
// Accepts both "action" and "type" field names (Claude Desktop sometimes uses "type").
type PipelineStep struct {
	ID           string                 `json:"id"`                      // Unique identifier (alphanumeric + - _)
	Action       string                 `json:"action"`                  // Action type: search, edit, etc.
	InputFrom    string                 `json:"input_from,omitempty"`    // ID of previous step to get input from
	InputFromAll []string               `json:"input_from_all,omitempty"` // IDs of multiple steps (for aggregate/merge)
	Params       map[string]interface{} `json:"params"`                  // Action-specific parameters
	Condition    *StepCondition         `json:"condition,omitempty"`     // Optional condition for conditional execution
}

// UnmarshalJSON implements custom JSON unmarshaling to accept "type" as alias for "action"
// and auto-generate missing step IDs. Claude Desktop often sends "type" instead of "action"
// and omits the "id" field.
func (ps *PipelineStep) UnmarshalJSON(data []byte) error {
	// Use a type alias to avoid infinite recursion
	type pipelineStepAlias PipelineStep
	var raw pipelineStepAlias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*ps = PipelineStep(raw)

	// Accept "type" as alias for "action" (Bug #22: Claude Desktop convention)
	if ps.Action == "" {
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(data, &rawMap); err == nil {
			if typeVal, ok := rawMap["type"]; ok {
				var t string
				if json.Unmarshal(typeVal, &t) == nil {
					ps.Action = t
				}
			}
		}
	}

	return nil
}

// StepResult represents the result of a single pipeline step
type StepResult struct {
	StepID       string            `json:"step_id"`
	Action       string            `json:"action"`
	Success      bool              `json:"success"`
	Skipped      bool              `json:"skipped,omitempty"`         // True if condition evaluated to false
	SkipReason   string            `json:"skip_reason,omitempty"`     // Why the step was skipped
	FilesMatched []string          `json:"files_matched,omitempty"`   // Files found/affected
	Content      map[string]string `json:"content,omitempty"`         // path -> content
	EditsApplied int               `json:"edits_applied,omitempty"`   // Number of edits made
	Counts       map[string]int    `json:"counts,omitempty"`          // path -> occurrence count
	Error        string            `json:"error,omitempty"`           // Error message if failed
	Duration     time.Duration     `json:"duration"`                  // Step execution time
	RiskLevel          string            `json:"risk_level,omitempty"`          // LOW/MEDIUM/HIGH/CRITICAL
	AggregatedContent  string            `json:"aggregated_content,omitempty"`  // Combined content from aggregate/merge
	internalData       interface{}       `json:"-"`                             // Internal data not serialized
}

// PipelineResult represents the final result of pipeline execution
type PipelineResult struct {
	Name              string        `json:"name"`
	Success           bool          `json:"success"`
	TotalSteps        int           `json:"total_steps"`
	CompletedSteps    int           `json:"completed_steps"`
	Results           []StepResult  `json:"results"`
	BackupID          string        `json:"backup_id,omitempty"`
	TotalDuration     time.Duration `json:"total_duration"`
	DryRun            bool          `json:"dry_run"`
	Verbose           bool          `json:"verbose"`
	OverallRiskLevel  string        `json:"overall_risk_level,omitempty"`
	FilesAffected     []string      `json:"files_affected,omitempty"`
	TotalEdits        int           `json:"total_edits,omitempty"`
	RollbackPerformed bool          `json:"rollback_performed,omitempty"`
}

// PipelineContext maintains state during pipeline execution
type PipelineContext struct {
	stepResults   map[string]*StepResult
	affectedFiles map[string]bool
	regexCache    map[string]*regexp.Regexp
	backupID      string
	mu            sync.RWMutex
}

// NewPipelineContext creates a new pipeline execution context
func NewPipelineContext() *PipelineContext {
	return &PipelineContext{
		stepResults:   make(map[string]*StepResult),
		affectedFiles: make(map[string]bool),
		regexCache:    make(map[string]*regexp.Regexp),
	}
}

// GetStepResult retrieves a step result by ID (thread-safe)
func (pc *PipelineContext) GetStepResult(stepID string) (*StepResult, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	result, exists := pc.stepResults[stepID]
	return result, exists
}

// SetStepResult stores a step result (thread-safe)
func (pc *PipelineContext) SetStepResult(stepID string, result *StepResult) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.stepResults[stepID] = result
}

// AddAffectedFiles adds files to the affected set (thread-safe)
func (pc *PipelineContext) AddAffectedFiles(files []string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	for _, f := range files {
		pc.affectedFiles[f] = true
	}
}

// GetAffectedFiles returns all unique affected files
func (pc *PipelineContext) GetAffectedFiles() []string {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	files := make([]string, 0, len(pc.affectedFiles))
	for f := range pc.affectedFiles {
		files = append(files, f)
	}
	return files
}

// SetBackupID stores the backup ID
func (pc *PipelineContext) SetBackupID(id string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.backupID = id
}

// GetBackupID retrieves the backup ID
func (pc *PipelineContext) GetBackupID() string {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.backupID
}

// Supported pipeline actions
var supportedActions = map[string]bool{
	"search":             true,
	"read_ranges":        true,
	"edit":               true,
	"multi_edit":         true,
	"count_occurrences":  true,
	"regex_transform":    true,
	"copy":               true,
	"rename":             true,
	"delete":             true,
	"aggregate":          true,
	"diff":               true,
	"merge":              true,
}

// Validate validates the entire pipeline request
func (pr *PipelineRequest) Validate() error {
	if pr.validated {
		return nil // Already validated
	}

	// Validate name
	if strings.TrimSpace(pr.Name) == "" {
		return &ValidationError{
			Field:   "name",
			Message: "pipeline name is required",
		}
	}

	// Normalize steps: auto-generate missing IDs (Bug #22)
	// Claude Desktop often omits step IDs. Generate "step-0", "step-1", etc.
	for i := range pr.Steps {
		if strings.TrimSpace(pr.Steps[i].ID) == "" {
			pr.Steps[i].ID = fmt.Sprintf("step-%d", i)
		}
	}

	// Validate steps
	if len(pr.Steps) == 0 {
		return &ValidationError{
			Field:   "steps",
			Message: "at least one step is required",
		}
	}

	if len(pr.Steps) > MaxPipelineSteps {
		return &ValidationError{
			Field:   "steps",
			Message: fmt.Sprintf("too many steps (max %d, got %d)", MaxPipelineSteps, len(pr.Steps)),
		}
	}

	// Validate unique step IDs and build dependency graph
	stepIDs := make(map[string]int) // ID -> index
	for i, step := range pr.Steps {
		// Validate step
		if err := step.Validate(); err != nil {
			return fmt.Errorf("step %d (%s): %w", i, step.ID, err)
		}

		// Check for duplicate IDs
		if prevIdx, exists := stepIDs[step.ID]; exists {
			return &ValidationError{
				Field:   "steps",
				Message: fmt.Sprintf("duplicate step ID '%s' at indices %d and %d", step.ID, prevIdx, i),
			}
		}
		stepIDs[step.ID] = i

		// Validate input_from references
		if step.InputFrom != "" {
			refIdx, exists := stepIDs[step.InputFrom]
			if !exists {
				return &ValidationError{
					Field:   "input_from",
					Message: fmt.Sprintf("step '%s' references non-existent step '%s'", step.ID, step.InputFrom),
				}
			}
			// Verify backward reference only (no forward references)
			if refIdx >= i {
				return &ValidationError{
					Field:   "input_from",
					Message: fmt.Sprintf("step '%s' has forward reference to step '%s' (only backward references allowed)", step.ID, step.InputFrom),
				}
			}
		}

		// Validate input_from_all references
		for _, ref := range step.InputFromAll {
			refIdx, exists := stepIDs[ref]
			if !exists {
				return &ValidationError{
					Field:   "input_from_all",
					Message: fmt.Sprintf("step '%s' references non-existent step '%s'", step.ID, ref),
				}
			}
			if refIdx >= i {
				return &ValidationError{
					Field:   "input_from_all",
					Message: fmt.Sprintf("step '%s' has forward reference to step '%s'", step.ID, ref),
				}
			}
		}

		// Validate condition references
		if step.Condition != nil {
			if err := ValidateCondition(step.Condition, step.ID, stepIDs); err != nil {
				return err
			}
		}
	}

	pr.validated = true
	return nil
}

// Validate validates a single pipeline step
func (ps *PipelineStep) Validate() error {
	// Validate ID
	if strings.TrimSpace(ps.ID) == "" {
		return &ValidationError{
			Field:   "id",
			Message: "step ID is required",
		}
	}

	// Validate ID format (alphanumeric + - _)
	if !isValidStepID(ps.ID) {
		return &ValidationError{
			Field:   "id",
			Message: fmt.Sprintf("invalid step ID '%s' (only alphanumeric, -, and _ allowed)", ps.ID),
		}
	}

	// Validate action
	if !supportedActions[ps.Action] {
		return &ValidationError{
			Field:   "action",
			Message: fmt.Sprintf("unsupported action '%s'", ps.Action),
		}
	}

	// Validate action-specific parameters
	if err := ps.validateActionParams(); err != nil {
		return err
	}

	return nil
}

// isValidStepID checks if step ID contains only alphanumeric, -, and _
func isValidStepID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// validateActionParams validates action-specific parameters
func (ps *PipelineStep) validateActionParams() error {
	if ps.Params == nil {
		ps.Params = make(map[string]interface{})
	}

	// hasInputFiles checks for input_from, files, or path params
	hasInputFiles := func() bool {
		if ps.InputFrom != "" {
			return true
		}
		if _, ok := ps.Params["files"]; ok {
			return true
		}
		if _, ok := ps.Params["path"]; ok {
			return true
		}
		return false
	}

	switch ps.Action {
	case "search":
		// Requires: pattern
		if _, ok := ps.Params["pattern"]; !ok {
			return &ValidationError{
				Field:   "params.pattern",
				Message: "search action requires 'pattern' parameter",
			}
		}

	case "read_ranges":
		// Requires: input_from OR files/path
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "read_ranges action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "edit":
		// Requires: old_text, new_text, and (input_from OR files/path)
		if _, ok := ps.Params["old_text"]; !ok {
			return &ValidationError{
				Field:   "params.old_text",
				Message: "edit action requires 'old_text' parameter",
			}
		}
		if _, ok := ps.Params["new_text"]; !ok {
			return &ValidationError{
				Field:   "params.new_text",
				Message: "edit action requires 'new_text' parameter",
			}
		}
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "edit action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "multi_edit":
		// Requires: edits array, and (input_from OR files/path)
		if _, ok := ps.Params["edits"]; !ok {
			return &ValidationError{
				Field:   "params.edits",
				Message: "multi_edit action requires 'edits' parameter",
			}
		}
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "multi_edit action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "count_occurrences":
		// Requires: pattern, and (input_from OR files/path)
		if _, ok := ps.Params["pattern"]; !ok {
			return &ValidationError{
				Field:   "params.pattern",
				Message: "count_occurrences action requires 'pattern' parameter",
			}
		}
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "count_occurrences action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "regex_transform":
		// Requires: patterns array, and (input_from OR files/path)
		if _, ok := ps.Params["patterns"]; !ok {
			return &ValidationError{
				Field:   "params.patterns",
				Message: "regex_transform action requires 'patterns' parameter",
			}
		}
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "regex_transform action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "copy":
		// Requires: destination, and (input_from OR files/path)
		if _, ok := ps.Params["destination"]; !ok {
			return &ValidationError{
				Field:   "params.destination",
				Message: "copy action requires 'destination' parameter",
			}
		}
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "copy action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "rename":
		// Requires: destination, and (input_from OR files/path)
		if _, ok := ps.Params["destination"]; !ok {
			return &ValidationError{
				Field:   "params.destination",
				Message: "rename action requires 'destination' parameter",
			}
		}
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "rename action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "delete":
		// Requires: input_from OR files/path
		if !hasInputFiles() {
			return &ValidationError{
				Field:   "params.files",
				Message: "delete action requires 'input_from', 'files', or 'path' parameter",
			}
		}

	case "aggregate":
		// Requires: input_from_all (array of step IDs)
		if len(ps.InputFromAll) == 0 && ps.InputFrom == "" {
			return &ValidationError{
				Field:   "input_from_all",
				Message: "aggregate action requires 'input_from_all' or 'input_from'",
			}
		}

	case "diff":
		// Requires: two files or input_from with 2 files
		if ps.InputFrom == "" {
			if _, ok := ps.Params["file_a"]; !ok {
				return &ValidationError{
					Field:   "params.file_a",
					Message: "diff action requires 'input_from' or 'file_a' and 'file_b' parameters",
				}
			}
			if _, ok := ps.Params["file_b"]; !ok {
				return &ValidationError{
					Field:   "params.file_b",
					Message: "diff action requires 'file_b' parameter",
				}
			}
		}

	case "merge":
		// Requires: input_from_all (array of step IDs)
		if len(ps.InputFromAll) == 0 && ps.InputFrom == "" {
			return &ValidationError{
				Field:   "input_from_all",
				Message: "merge action requires 'input_from_all' or 'input_from'",
			}
		}
	}

	return nil
}

// getInputFiles retrieves input files from either input_from or params
func (ps *PipelineStep) getInputFiles(ctx *PipelineContext) ([]string, error) {
	// Try input_from first
	if ps.InputFrom != "" {
		prevResult, exists := ctx.GetStepResult(ps.InputFrom)
		if !exists {
			return nil, fmt.Errorf("referenced step '%s' has not been executed", ps.InputFrom)
		}
		if !prevResult.Success {
			return nil, fmt.Errorf("referenced step '%s' failed: %s", ps.InputFrom, prevResult.Error)
		}
		if len(prevResult.FilesMatched) == 0 {
			return nil, fmt.Errorf("referenced step '%s' matched no files", ps.InputFrom)
		}
		return prevResult.FilesMatched, nil
	}

	// Try params["files"] or params["path"] (single-file alias)
	filesParam, ok := ps.Params["files"]
	if !ok {
		if pathParam, pok := ps.Params["path"]; pok {
			// Normalize "path" to a single-element files list
			filesParam = pathParam
			ok = true
		}
	}
	if ok {
		switch v := filesParam.(type) {
		case []string:
			return v, nil
		case []interface{}:
			files := make([]string, 0, len(v))
			for i, item := range v {
				str, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("files[%d] is not a string", i)
				}
				files = append(files, str)
			}
			return files, nil
		case string:
			return []string{v}, nil
		default:
			return nil, fmt.Errorf("files parameter has invalid type: %T", v)
		}
	}

	return nil, fmt.Errorf("no input files specified (missing input_from or files)")
}
