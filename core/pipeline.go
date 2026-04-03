package core

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// toInt converts an interface{} (float64 from JSON, string, or int) to int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(math.Round(n))
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

// PipelineExecutor executes multi-step file transformation pipelines
type PipelineExecutor struct {
	engine     *UltraFastEngine
	OnProgress func(stepIndex, totalSteps int, result StepResult) // Optional per-step progress callback
}

// NewPipelineExecutor creates a new pipeline executor
func NewPipelineExecutor(engine *UltraFastEngine) *PipelineExecutor {
	return &PipelineExecutor{
		engine: engine,
	}
}

// Execute executes a complete pipeline
func (pe *PipelineExecutor) Execute(ctx context.Context, request PipelineRequest) (*PipelineResult, error) {
	startTime := time.Now()

	// Step 1: Validate request
	if err := request.Validate(); err != nil {
		return &PipelineResult{
			Name:           request.Name,
			Success:        false,
			TotalSteps:     len(request.Steps),
			CompletedSteps: 0,
			Results:        []StepResult{{Error: err.Error()}},
			DryRun:         request.DryRun,
			TotalDuration:  time.Since(startTime),
		}, err
	}

	// Step 2: Initialize context
	pipelineCtx := NewPipelineContext()

	// Track pipeline execution in audit sub_op
	AppendSubOp(ctx, fmt.Sprintf("pipeline:%d_steps", len(request.Steps)))

	// Step 3: Determine if backup is needed
	needsBackup := request.CreateBackup && pe.hasDestructiveSteps(request.Steps)
	var backupID string

	// Step 4: Pre-scan files if backup needed (and not dry-run)
	if needsBackup && !request.DryRun {
		affectedFiles, err := pe.estimateAffectedFiles(ctx, request, pipelineCtx)
		if err != nil {
			return &PipelineResult{
				Name:           request.Name,
				Success:        false,
				TotalSteps:     len(request.Steps),
				CompletedSteps: 0,
				Results:        []StepResult{{Error: fmt.Sprintf("pre-scan failed: %v", err)}},
				DryRun:         request.DryRun,
				TotalDuration:  time.Since(startTime),
			}, err
		}

		// Validate file count limit
		if len(affectedFiles) > MaxPipelineFiles {
			return &PipelineResult{
				Name:           request.Name,
				Success:        false,
				TotalSteps:     len(request.Steps),
				CompletedSteps: 0,
				Results: []StepResult{{
					Error: fmt.Sprintf("too many files affected (%d > %d). Use force=true to bypass or reduce scope",
						len(affectedFiles), MaxPipelineFiles),
				}},
				FilesAffected: affectedFiles,
				DryRun:        request.DryRun,
				TotalDuration: time.Since(startTime),
			}, fmt.Errorf("file count limit exceeded")
		}

		// Step 5: Create batch backup
		if len(affectedFiles) > 0 {
			backupID, err = pe.engine.backupManager.CreateBatchBackup(affectedFiles, "pipeline", request.Name)
			if err != nil {
				return &PipelineResult{
					Name:           request.Name,
					Success:        false,
					TotalSteps:     len(request.Steps),
					CompletedSteps: 0,
					Results:        []StepResult{{Error: fmt.Sprintf("backup creation failed: %v", err)}},
					FilesAffected:  affectedFiles,
					DryRun:         request.DryRun,
					TotalDuration:  time.Since(startTime),
				}, err
			}
			pipelineCtx.SetBackupID(backupID)
		}
	}

	// Step 6: Execute steps (parallel or sequential)
	if request.Parallel {
		return pe.executeParallelPath(ctx, request, pipelineCtx, backupID, startTime)
	}

	results := make([]StepResult, 0, len(request.Steps))
	completedSteps := 0
	allSuccess := true
	totalEdits := 0

	for i, step := range request.Steps {
		stepResult, err := pe.executeStep(ctx, step, pipelineCtx, request.DryRun, request.Force)

		// Track edits
		if stepResult.EditsApplied > 0 {
			totalEdits += stepResult.EditsApplied
		}

		// Track affected files
		if len(stepResult.FilesMatched) > 0 {
			pipelineCtx.AddAffectedFiles(stepResult.FilesMatched)
		}

		// Store result
		pipelineCtx.SetStepResult(step.ID, &stepResult)
		results = append(results, stepResult)

		// Fire progress callback
		if pe.OnProgress != nil {
			pe.OnProgress(i, len(request.Steps), stepResult)
		}

		if err != nil || !stepResult.Success {
			allSuccess = false
			if request.StopOnError {
				// Rollback if we have a backup
				if backupID != "" && !request.DryRun {
					if rollbackErr := pe.rollback(ctx, backupID); rollbackErr == nil {
						return &PipelineResult{
							Name:              request.Name,
							Success:           false,
							TotalSteps:        len(request.Steps),
							CompletedSteps:    i,
							Results:           results,
							BackupID:          backupID,
							FilesAffected:     pipelineCtx.GetAffectedFiles(),
							TotalEdits:        totalEdits,
							RollbackPerformed: true,
							DryRun:            request.DryRun,
							TotalDuration:     time.Since(startTime),
						}, fmt.Errorf("pipeline failed at step %d, rolled back", i+1)
					}
				}

				return &PipelineResult{
					Name:           request.Name,
					Success:        false,
					TotalSteps:     len(request.Steps),
					CompletedSteps: i,
					Results:        results,
					BackupID:       backupID,
					FilesAffected:  pipelineCtx.GetAffectedFiles(),
					TotalEdits:     totalEdits,
					DryRun:         request.DryRun,
					TotalDuration:  time.Since(startTime),
				}, fmt.Errorf("pipeline failed at step %d", i+1)
			}
		}

		if stepResult.Success {
			completedSteps++
		}
	}

	// Step 7: Calculate final metrics
	affectedFiles := pipelineCtx.GetAffectedFiles()
	overallRisk := pe.assessPipelineRisk(affectedFiles, totalEdits)

	// Determine overall success
	finalSuccess := allSuccess
	if !request.StopOnError && completedSteps > 0 {
		finalSuccess = true // At least some steps succeeded
	}

	return &PipelineResult{
		Name:             request.Name,
		Success:          finalSuccess,
		TotalSteps:       len(request.Steps),
		CompletedSteps:   completedSteps,
		Results:          results,
		BackupID:         backupID,
		FilesAffected:    affectedFiles,
		TotalEdits:       totalEdits,
		OverallRiskLevel: overallRisk,
		DryRun:           request.DryRun,
		Verbose:          request.Verbose,
		TotalDuration:    time.Since(startTime),
	}, nil
}

// executeStep executes a single pipeline step
func (pe *PipelineExecutor) executeStep(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, dryRun bool, force bool) (StepResult, error) {
	startTime := time.Now()
	result := StepResult{
		StepID:  step.ID,
		Action:  step.Action,
		Success: false,
	}

	// Evaluate condition (skip if false)
	if step.Condition != nil {
		shouldRun, reason := EvaluateCondition(step.Condition, pipelineCtx)
		if !shouldRun {
			result.Success = true
			result.Skipped = true
			result.SkipReason = reason
			result.Duration = time.Since(startTime)
			AppendSubOp(ctx, step.Action+":skipped")
			return result, nil
		}
	}

	// Resolve template variables in params
	step.Params = ResolveTemplates(step.Params, pipelineCtx)

	// Track each step action in audit sub_op chain
	AppendSubOp(ctx, step.Action)

	var err error

	switch step.Action {
	case "search":
		err = pe.executeSearch(ctx, step, pipelineCtx, &result)
	case "read_ranges":
		err = pe.executeReadRanges(ctx, step, pipelineCtx, &result)
	case "edit":
		err = pe.executeEdit(ctx, step, pipelineCtx, &result, dryRun, force)
	case "multi_edit":
		err = pe.executeMultiEdit(ctx, step, pipelineCtx, &result, dryRun, force)
	case "count_occurrences":
		err = pe.executeCountOccurrences(ctx, step, pipelineCtx, &result)
	case "regex_transform":
		err = pe.executeRegexTransform(ctx, step, pipelineCtx, &result, dryRun, force)
	case "copy":
		err = pe.executeCopy(ctx, step, pipelineCtx, &result, dryRun)
	case "rename":
		err = pe.executeRename(ctx, step, pipelineCtx, &result, dryRun)
	case "delete":
		err = pe.executeDelete(ctx, step, pipelineCtx, &result, dryRun)
	case "aggregate":
		err = pe.executeAggregate(ctx, step, pipelineCtx, &result)
	case "diff":
		err = pe.executeDiff(ctx, step, pipelineCtx, &result)
	case "merge":
		err = pe.executeMerge(ctx, step, pipelineCtx, &result)
	default:
		err = &PipelineStepError{
			StepID:    step.ID,
			StepIndex: 0, // index not available here; set by caller if needed
			Action:    step.Action,
			Message:   fmt.Sprintf("unsupported action: %s", step.Action),
		}
	}

	result.Duration = time.Since(startTime)

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		return result, err
	}

	result.Success = true
	return result, nil
}

// executeSearch performs a smart search operation
func (pe *PipelineExecutor) executeSearch(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult) error {
	// Extract parameters
	pattern, _ := step.Params["pattern"].(string)
	path, _ := step.Params["path"].(string)
	if path == "" {
		path = "."
	}

	includeContent := false
	if inc, ok := step.Params["include_content"].(bool); ok {
		includeContent = inc
	}

	var fileTypes []string
	if ft, ok := step.Params["file_types"]; ok {
		switch v := ft.(type) {
		case string:
			fileTypes = []string{v}
		case []string:
			fileTypes = v
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok {
					fileTypes = append(fileTypes, str)
				}
			}
		}
	}

	// Perform search
	matches, err := pe.performSmartSearchInternal(ctx, path, pattern, includeContent, fileTypes)
	if err != nil {
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "search",
			Param:   "pattern",
			Message: "search failed",
			Err:     err,
		}
	}

	// Populate result
	result.FilesMatched = make([]string, 0, len(matches))
	for _, match := range matches {
		result.FilesMatched = append(result.FilesMatched, match.FilePath)
	}

	// Store matches in internal data for potential content access
	result.internalData = matches

	return nil
}

// executeReadRanges reads file contents, optionally filtered by line ranges
func (pe *PipelineExecutor) executeReadRanges(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	result.Content = make(map[string]string)
	result.FilesMatched = files

	// Extract optional range parameters
	startLine := 0
	endLine := 0
	if sl, ok := step.Params["start_line"]; ok {
		startLine = toInt(sl)
	}
	if el, ok := step.Params["end_line"]; ok {
		endLine = toInt(el)
	}

	// Parse "ranges" param: array of {start, end} objects for multiple ranges
	type lineRange struct{ start, end int }
	var ranges []lineRange

	if r, ok := step.Params["ranges"]; ok {
		switch rv := r.(type) {
		case []interface{}:
			for _, item := range rv {
				if m, ok := item.(map[string]interface{}); ok {
					s := toInt(m["start"])
					e := toInt(m["end"])
					if s > 0 && e >= s {
						ranges = append(ranges, lineRange{s, e})
					}
				}
			}
		}
	}

	// If single start_line/end_line provided, use as a single range
	hasRange := startLine > 0 && endLine >= startLine
	if hasRange && len(ranges) == 0 {
		ranges = append(ranges, lineRange{startLine, endLine})
	}

	for _, filePath := range files {
		normalizedPath := NormalizePath(filePath)

		// Check access
		if len(pe.engine.config.AllowedPaths) > 0 {
			if !pe.engine.isPathAllowed(normalizedPath) {
				return &PathError{Op: "read_ranges", Path: normalizedPath, Err: fmt.Errorf("access denied")}
			}
		}

		if len(ranges) > 0 {
			// Read only requested line ranges using engine.ReadFileRange
			var combined strings.Builder
			for i, rng := range ranges {
				if i > 0 {
					combined.WriteString("\n...\n")
				}
				content, err := pe.engine.ReadFileRange(ctx, normalizedPath, rng.start, rng.end)
				if err != nil {
					return &PipelineStepError{
						StepID:  step.ID,
						Action:  "read_ranges",
						Param:   "ranges",
						Message: fmt.Sprintf("failed to read range [%d-%d] from %s", rng.start, rng.end, filePath),
						Err:     err,
					}
				}
				combined.WriteString(content)
			}
			result.Content[filePath] = combined.String()
		} else {
			// No range specified — read full file (backward compatible)
			content, err := os.ReadFile(normalizedPath)
			if err != nil {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "read_ranges",
					Param:   "path",
					Message: fmt.Sprintf("failed to read %s", filePath),
					Err:     err,
				}
			}
			result.Content[filePath] = string(content)
		}
	}

	return nil
}

// executeEdit performs a simple edit operation
func (pe *PipelineExecutor) executeEdit(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult, dryRun bool, force bool) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	oldText, _ := step.Params["old_text"].(string)
	newText, _ := step.Params["new_text"].(string)

	result.FilesMatched = files
	result.Counts = make(map[string]int)
	totalEdits := 0

	// Calculate batch impact for risk assessment
	operations := make([]BatchImpactInfo, 0, len(files))
	for _, filePath := range files {
		normalizedPath := NormalizePath(filePath)
		content, err := os.ReadFile(normalizedPath)
		if err != nil {
			continue // Skip unreadable files for risk assessment
		}
		operations = append(operations, BatchImpactInfo{
			FilePath: filePath,
			Content:  string(content),
			OldText:  oldText,
			NewText:  newText,
		})
	}
	batchImpact := CalculateBatchImpact(operations, pe.engine.riskThresholds)

	// Assess risk
	riskLevel := "LOW"
	if batchImpact.TotalOccurrences >= 1000 || batchImpact.TotalFiles >= 80 {
		riskLevel = "CRITICAL"
	} else if batchImpact.TotalOccurrences >= 500 || batchImpact.TotalFiles >= 50 {
		riskLevel = "HIGH"
	} else if batchImpact.TotalOccurrences >= 100 || batchImpact.TotalFiles >= 30 {
		riskLevel = "MEDIUM"
	}
	result.RiskLevel = riskLevel

	// Check if operation should be blocked
	if !force && !dryRun && (riskLevel == "HIGH" || riskLevel == "CRITICAL") {
		return &PipelineStepError{
			StepID:     step.ID,
			Action:     "edit",
			Message:    fmt.Sprintf("operation blocked due to %s risk (%d files, %d occurrences)", riskLevel, batchImpact.TotalFiles, batchImpact.TotalOccurrences),
			Suggestion: "Use force=true to proceed",
		}
	}

	// Execute edits (or count for dry-run)
	for _, filePath := range files {
		normalizedPath := NormalizePath(filePath)

		if dryRun {
			// Just count occurrences
			content, err := os.ReadFile(normalizedPath)
			if err != nil {
				continue // Skip unreadable files in dry-run
			}
			count := strings.Count(string(content), oldText)
			result.Counts[filePath] = count
			totalEdits += count
		} else {
			// Perform actual edit
			// Force=true because pipeline already assessed risk at batch level
			editResult, err := pe.engine.EditFile(ctx, normalizedPath, oldText, newText, true, dryRun)
			if err != nil {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "edit",
					Message: fmt.Sprintf("edit failed for %s", filePath),
					Err:     err,
				}
			}
			result.Counts[filePath] = editResult.ReplacementCount
			totalEdits += editResult.ReplacementCount
		}
	}

	result.EditsApplied = totalEdits
	return nil
}

// executeMultiEdit performs multiple edits on files
func (pe *PipelineExecutor) executeMultiEdit(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult, dryRun bool, force bool) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	// Parse edits array
	editsParam, ok := step.Params["edits"]
	if !ok {
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "multi_edit",
			Param:   "edits",
			Message: "edits parameter is required",
		}
	}

	var edits []MultiEditOperation
	switch v := editsParam.(type) {
	case []interface{}:
		for i, item := range v {
			editMap, ok := item.(map[string]interface{})
			if !ok {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "multi_edit",
					Param:   fmt.Sprintf("edits[%d]", i),
					Message: "not an object",
				}
			}
			oldText, _ := editMap["old_text"].(string)
			newText, _ := editMap["new_text"].(string)
			if oldText == "" {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "multi_edit",
					Param:   fmt.Sprintf("edits[%d].old_text", i),
					Message: "old_text is required",
				}
			}
			edits = append(edits, MultiEditOperation{OldText: oldText, NewText: newText})
		}
	default:
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "multi_edit",
			Param:   "edits",
			Message: fmt.Sprintf("invalid type: %T", v),
		}
	}

	result.FilesMatched = files
	result.Counts = make(map[string]int)
	totalEdits := 0

	// Calculate risk (simplified for multi-edit)
	riskLevel := "LOW"
	if len(files) >= 80 || len(edits) >= 100 {
		riskLevel = "CRITICAL"
	} else if len(files) >= 50 || len(edits) >= 50 {
		riskLevel = "HIGH"
	} else if len(files) >= 30 || len(edits) >= 20 {
		riskLevel = "MEDIUM"
	}
	result.RiskLevel = riskLevel

	if !force && !dryRun && (riskLevel == "HIGH" || riskLevel == "CRITICAL") {
		return &PipelineStepError{
			StepID:     step.ID,
			Action:     "multi_edit",
			Message:    fmt.Sprintf("operation blocked due to %s risk", riskLevel),
			Suggestion: "Use force=true to proceed",
		}
	}

	// Execute multi-edits
	for _, filePath := range files {
		normalizedPath := NormalizePath(filePath)

		if dryRun {
			// Count potential changes
			content, err := os.ReadFile(normalizedPath)
			if err != nil {
				continue
			}
			count := 0
			for _, edit := range edits {
				count += strings.Count(string(content), edit.OldText)
			}
			result.Counts[filePath] = count
			totalEdits += count
		} else {
			// Perform actual multi-edit
			editResult, err := pe.engine.MultiEdit(ctx, normalizedPath, edits, force, dryRun)
			if err != nil {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "multi_edit",
					Message: fmt.Sprintf("multi-edit failed for %s", filePath),
					Err:     err,
				}
			}
			result.Counts[filePath] = editResult.TotalEdits
			totalEdits += editResult.TotalEdits
		}
	}

	result.EditsApplied = totalEdits
	return nil
}

// executeCountOccurrences counts pattern occurrences in files
func (pe *PipelineExecutor) executeCountOccurrences(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	pattern, _ := step.Params["pattern"].(string)
	result.FilesMatched = files
	result.Counts = make(map[string]int)

	for _, filePath := range files {
		normalizedPath := NormalizePath(filePath)

		content, err := os.ReadFile(normalizedPath)
		if err != nil {
			continue // Skip unreadable files
		}

		count := strings.Count(string(content), pattern)
		result.Counts[filePath] = count
	}

	return nil
}

// executeRegexTransform performs regex-based transformations
func (pe *PipelineExecutor) executeRegexTransform(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult, dryRun bool, force bool) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	// Parse patterns array
	patternsParam, ok := step.Params["patterns"]
	if !ok {
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "regex_transform",
			Param:   "patterns",
			Message: "patterns parameter is required",
		}
	}

	var patterns []TransformPattern
	switch v := patternsParam.(type) {
	case []interface{}:
		for i, item := range v {
			patternMap, ok := item.(map[string]interface{})
			if !ok {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "regex_transform",
					Param:   fmt.Sprintf("patterns[%d]", i),
					Message: "not an object",
				}
			}
			pattern, _ := patternMap["pattern"].(string)
			replacement, _ := patternMap["replacement"].(string)
			if pattern == "" {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "regex_transform",
					Param:   fmt.Sprintf("patterns[%d].pattern", i),
					Message: "pattern is required",
				}
			}
			patterns = append(patterns, TransformPattern{Pattern: pattern, Replacement: replacement})
		}
	default:
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "regex_transform",
			Param:   "patterns",
			Message: fmt.Sprintf("invalid type: %T", v),
		}
	}

	result.FilesMatched = files
	result.Counts = make(map[string]int)
	totalEdits := 0

	// Risk assessment
	riskLevel := "LOW"
	if len(files) >= 80 {
		riskLevel = "CRITICAL"
	} else if len(files) >= 50 {
		riskLevel = "HIGH"
	} else if len(files) >= 30 {
		riskLevel = "MEDIUM"
	}
	result.RiskLevel = riskLevel

	if !force && !dryRun && (riskLevel == "HIGH" || riskLevel == "CRITICAL") {
		return &PipelineStepError{
			StepID:     step.ID,
			Action:     "regex_transform",
			Message:    fmt.Sprintf("operation blocked due to %s risk", riskLevel),
			Suggestion: "Use force=true to proceed",
		}
	}

	// Execute transformations
	transformer := NewRegexTransformer(pe.engine)
	for _, filePath := range files {
		normalizedPath := NormalizePath(filePath)

		config := RegexTransformConfig{
			FilePath: normalizedPath,
			Patterns: patterns,
			Mode:     ModeSequential,
			DryRun:   dryRun,
		}

		transformResult, err := transformer.Transform(ctx, config)
		if err != nil {
			if dryRun {
				continue // Skip errors in dry-run
			}
			return &PipelineStepError{
				StepID:  step.ID,
				Action:  "regex_transform",
				Message: fmt.Sprintf("regex transform failed for %s", filePath),
				Err:     err,
			}
		}
		result.Counts[filePath] = transformResult.TotalReplacements
		totalEdits += transformResult.TotalReplacements
	}

	result.EditsApplied = totalEdits
	return nil
}

// executeCopy copies files to a destination
func (pe *PipelineExecutor) executeCopy(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult, dryRun bool) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	destination, _ := step.Params["destination"].(string)
	if destination == "" {
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "copy",
			Param:   "destination",
			Message: "destination parameter is required",
		}
	}

	result.FilesMatched = make([]string, 0, len(files))

	for _, filePath := range files {
		normalizedSrc := NormalizePath(filePath)

		// Calculate destination path
		fileName := filepath.Base(normalizedSrc)
		normalizedDest := filepath.Join(destination, fileName)

		if dryRun {
			// Just validate paths
			result.FilesMatched = append(result.FilesMatched, normalizedDest)
		} else {
			// Perform copy
			if err := pe.engine.CopyFile(ctx, normalizedSrc, normalizedDest); err != nil {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "copy",
					Message: fmt.Sprintf("copy failed for %s", filePath),
					Err:     err,
				}
			}
			result.FilesMatched = append(result.FilesMatched, normalizedDest)
		}
	}

	return nil
}

// executeRename renames files
func (pe *PipelineExecutor) executeRename(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult, dryRun bool) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	destination, _ := step.Params["destination"].(string)
	if destination == "" {
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "rename",
			Param:   "destination",
			Message: "destination parameter is required",
		}
	}

	result.FilesMatched = make([]string, 0, len(files))

	for _, filePath := range files {
		normalizedSrc := NormalizePath(filePath)

		// Calculate destination path
		fileName := filepath.Base(normalizedSrc)
		normalizedDest := filepath.Join(destination, fileName)

		if dryRun {
			result.FilesMatched = append(result.FilesMatched, normalizedDest)
		} else {
			// Perform rename
			if err := pe.engine.RenameFile(ctx, normalizedSrc, normalizedDest); err != nil {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "rename",
					Message: fmt.Sprintf("rename failed for %s", filePath),
					Err:     err,
				}
			}
			result.FilesMatched = append(result.FilesMatched, normalizedDest)
		}
	}

	return nil
}

// executeDelete soft-deletes files
func (pe *PipelineExecutor) executeDelete(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult, dryRun bool) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	result.FilesMatched = files

	for _, filePath := range files {
		normalizedPath := NormalizePath(filePath)

		if !dryRun {
			// Perform soft delete
			if err := pe.engine.SoftDeleteFile(ctx, normalizedPath); err != nil {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "delete",
					Message: fmt.Sprintf("delete failed for %s", filePath),
					Err:     err,
				}
			}
		}
	}

	return nil
}

// hasDestructiveSteps checks if any steps modify/delete files
func (pe *PipelineExecutor) hasDestructiveSteps(steps []PipelineStep) bool {
	destructiveActions := map[string]bool{
		"edit":            true,
		"multi_edit":      true,
		"regex_transform": true,
		"delete":          true,
		"rename":          true,
	}

	for _, step := range steps {
		if destructiveActions[step.Action] {
			return true
		}
	}
	return false
}

// estimateAffectedFiles pre-scans to identify files that will be affected
func (pe *PipelineExecutor) estimateAffectedFiles(ctx context.Context, request PipelineRequest, pipelineCtx *PipelineContext) ([]string, error) {
	affectedMap := make(map[string]bool)

	// Execute search steps to discover files
	for _, step := range request.Steps {
		if step.Action == "search" {
			// Execute search in read-only mode
			result := StepResult{StepID: step.ID, Action: "search"}
			if err := pe.executeSearch(ctx, step, pipelineCtx, &result); err != nil {
				return nil, err
			}
			for _, f := range result.FilesMatched {
				affectedMap[f] = true
			}
			// Store result for later steps
			pipelineCtx.SetStepResult(step.ID, &result)
		} else if step.Action == "edit" || step.Action == "multi_edit" || step.Action == "regex_transform" || step.Action == "delete" {
			// Get files from input_from or params
			files, err := step.getInputFiles(pipelineCtx)
			if err != nil {
				// If we can't get files yet, try from params
				if filesParam, ok := step.Params["files"]; ok {
					switch v := filesParam.(type) {
					case []string:
						for _, f := range v {
							affectedMap[f] = true
						}
					case []interface{}:
						for _, item := range v {
							if str, ok := item.(string); ok {
								affectedMap[str] = true
							}
						}
					case string:
						affectedMap[v] = true
					}
				}
				continue
			}
			for _, f := range files {
				affectedMap[f] = true
			}
		}
	}

	// Convert map to slice
	affected := make([]string, 0, len(affectedMap))
	for f := range affectedMap {
		affected = append(affected, f)
	}
	return affected, nil
}

// assessPipelineRisk calculates overall pipeline risk level
func (pe *PipelineExecutor) assessPipelineRisk(filesAffected []string, totalEdits int) string {
	fileCount := len(filesAffected)

	// Critical thresholds
	if fileCount >= PipelineRiskCritical || totalEdits >= PipelineEditsCritical {
		return "CRITICAL"
	}

	// High thresholds
	if fileCount >= PipelineRiskHigh || totalEdits >= PipelineEditsHigh {
		return "HIGH"
	}

	// Medium thresholds
	if fileCount >= PipelineRiskMedium || totalEdits >= PipelineEditsMedium {
		return "MEDIUM"
	}

	return "LOW"
}

// rollback restores files from backup
func (pe *PipelineExecutor) rollback(ctx context.Context, backupID string) error {
	if backupID == "" {
		return fmt.Errorf("no backup ID provided")
	}
	_, err := pe.engine.backupManager.RestoreBackup(backupID, "", false)
	return err
}

// performSmartSearchInternal performs search and returns structured matches
func (pe *PipelineExecutor) performSmartSearchInternal(ctx context.Context, path string, pattern string, includeContent bool, fileTypes []string) ([]PipelineSearchMatch, error) {
	// Normalize path
	normalizedPath := NormalizePath(path)

	// Check access
	if len(pe.engine.config.AllowedPaths) > 0 {
		if !pe.engine.isPathAllowed(normalizedPath) {
			return nil, &PathError{Op: "search", Path: normalizedPath, Err: fmt.Errorf("access denied")}
		}
	}

	matches := []PipelineSearchMatch{}

	// Compile regex if it looks like a pattern
	var regexPattern *regexp.Regexp
	if strings.ContainsAny(pattern, ".*+?[]{}()^$|\\") {
		var compileErr error
		regexPattern, compileErr = regexp.Compile(pattern)
		if compileErr != nil {
			// Fall back to literal search
			regexPattern = nil
		}
	}

	// Walk directory
	err := filepath.Walk(normalizedPath, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // Skip errors
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Filter by file type if specified
		if len(fileTypes) > 0 {
			typeMatched := false
			for _, ext := range fileTypes {
				if strings.HasSuffix(filePath, ext) {
					typeMatched = true
					break
				}
			}
			if !typeMatched {
				return nil
			}
		}

		// Read file for content search
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil // Skip unreadable files
		}

		// Check if pattern matches
		contentMatched := false
		if regexPattern != nil {
			contentMatched = regexPattern.Match(content)
		} else {
			contentMatched = strings.Contains(string(content), pattern)
		}

		if contentMatched {
			match := PipelineSearchMatch{
				FilePath: filePath,
				Count:    strings.Count(string(content), pattern),
			}
			if includeContent {
				match.Content = string(content)
			}
			matches = append(matches, match)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("search walk failed: %w", err)
	}

	return matches, nil
}

// executeParallelPath handles the parallel execution branch from Execute()
func (pe *PipelineExecutor) executeParallelPath(ctx context.Context, request PipelineRequest, pipelineCtx *PipelineContext, backupID string, startTime time.Time) (*PipelineResult, error) {
	_, results, err := pe.ExecuteParallel(ctx, request, pipelineCtx)

	completedSteps := 0
	totalEdits := 0
	allSuccess := true
	for _, r := range results {
		if r.Success {
			completedSteps++
		} else if !r.Skipped {
			allSuccess = false
		}
		totalEdits += r.EditsApplied
		if len(r.FilesMatched) > 0 {
			pipelineCtx.AddAffectedFiles(r.FilesMatched)
		}
	}

	affectedFiles := pipelineCtx.GetAffectedFiles()
	overallRisk := pe.assessPipelineRisk(affectedFiles, totalEdits)

	finalSuccess := allSuccess
	if !request.StopOnError && completedSteps > 0 {
		finalSuccess = true
	}

	pResult := &PipelineResult{
		Name:             request.Name,
		Success:          finalSuccess,
		TotalSteps:       len(request.Steps),
		CompletedSteps:   completedSteps,
		Results:          results,
		BackupID:         backupID,
		FilesAffected:    affectedFiles,
		TotalEdits:       totalEdits,
		OverallRiskLevel: overallRisk,
		DryRun:           request.DryRun,
		Verbose:          request.Verbose,
		TotalDuration:    time.Since(startTime),
	}

	if err != nil && request.StopOnError && backupID != "" && !request.DryRun {
		if rollbackErr := pe.rollback(ctx, backupID); rollbackErr == nil {
			pResult.RollbackPerformed = true
		}
	}

	return pResult, err
}

// ExecuteParallel executes a pipeline using DAG-based parallel scheduling
func (pe *PipelineExecutor) ExecuteParallel(ctx context.Context, request PipelineRequest, pipelineCtx *PipelineContext) (*PipelineResult, []StepResult, error) {
	scheduler := NewPipelineScheduler()
	levels, err := scheduler.BuildExecutionPlan(request.Steps)
	if err != nil {
		return nil, nil, err
	}

	results := make([]StepResult, len(request.Steps))
	completedSteps := 0
	allSuccess := true
	totalEdits := 0

	AppendSubOp(ctx, fmt.Sprintf("parallel:%d_levels", len(levels)))

	for _, level := range levels {
		if len(level) == 1 {
			// Single step — run directly
			idx := level[0]
			step := request.Steps[idx]
			stepResult, stepErr := pe.executeStep(ctx, step, pipelineCtx, request.DryRun, request.Force)

			if stepResult.EditsApplied > 0 {
				totalEdits += stepResult.EditsApplied
			}
			if len(stepResult.FilesMatched) > 0 {
				pipelineCtx.AddAffectedFiles(stepResult.FilesMatched)
			}
			pipelineCtx.SetStepResult(step.ID, &stepResult)
			results[idx] = stepResult

			if stepErr != nil || !stepResult.Success {
				allSuccess = false
				if request.StopOnError && !stepResult.Skipped {
					return nil, results[:idx+1], stepErr
				}
			}
			if stepResult.Success {
				completedSteps++
			}
		} else {
			// Multiple steps — run in parallel using worker pool
			type indexedResult struct {
				idx    int
				result StepResult
				err    error
			}

			ch := make(chan indexedResult, len(level))

			for _, idx := range level {
				idx := idx // capture
				step := request.Steps[idx]

				pe.engine.workerPool.Submit(func() {
					stepResult, stepErr := pe.executeStep(ctx, step, pipelineCtx, request.DryRun, request.Force)
					ch <- indexedResult{idx: idx, result: stepResult, err: stepErr}
				})
			}

			// Collect results
			for range level {
				ir := <-ch
				step := request.Steps[ir.idx]

				if ir.result.EditsApplied > 0 {
					totalEdits += ir.result.EditsApplied
				}
				if len(ir.result.FilesMatched) > 0 {
					pipelineCtx.AddAffectedFiles(ir.result.FilesMatched)
				}
				pipelineCtx.SetStepResult(step.ID, &ir.result)
				results[ir.idx] = ir.result

				if ir.err != nil || !ir.result.Success {
					allSuccess = false
				}
				if ir.result.Success {
					completedSteps++
				}
			}

			// Check stop_on_error after level completes
			if !allSuccess && request.StopOnError {
				return nil, results[:], fmt.Errorf("pipeline failed during parallel level")
			}
		}
	}

	_ = allSuccess
	return nil, results, nil // caller assembles final PipelineResult
}

// executeAggregate combines content/files from multiple referenced steps
func (pe *PipelineExecutor) executeAggregate(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult) error {
	separator := "\n"
	if sep, ok := step.Params["separator"].(string); ok {
		separator = sep
	}

	refs := step.InputFromAll
	if len(refs) == 0 && step.InputFrom != "" {
		refs = []string{step.InputFrom}
	}

	var allFiles []string
	var contentParts []string
	filesSeen := make(map[string]bool)

	for _, ref := range refs {
		refResult, exists := pipelineCtx.GetStepResult(ref)
		if !exists {
			return &PipelineStepError{
				StepID:  step.ID,
				Action:  "aggregate",
				Param:   "input_from_all",
				Message: fmt.Sprintf("referenced step '%s' not found", ref),
			}
		}

		// Collect unique files
		for _, f := range refResult.FilesMatched {
			if !filesSeen[f] {
				filesSeen[f] = true
				allFiles = append(allFiles, f)
			}
		}

		// Collect content
		for _, content := range refResult.Content {
			contentParts = append(contentParts, content)
		}
	}

	result.FilesMatched = allFiles
	if len(contentParts) > 0 {
		result.AggregatedContent = strings.Join(contentParts, separator)
	}

	return nil
}

// executeDiff produces a unified diff between two files
func (pe *PipelineExecutor) executeDiff(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult) error {
	var fileA, fileB string

	if step.InputFrom != "" {
		// Get two files from previous step
		refResult, exists := pipelineCtx.GetStepResult(step.InputFrom)
		if !exists {
			return &PipelineStepError{
				StepID:  step.ID,
				Action:  "diff",
				Param:   "input_from",
				Message: fmt.Sprintf("referenced step '%s' not found", step.InputFrom),
			}
		}
		if len(refResult.FilesMatched) < 2 {
			return &PipelineStepError{
				StepID:     step.ID,
				Action:     "diff",
				Param:      "input_from",
				Message:    fmt.Sprintf("diff requires at least 2 files, got %d", len(refResult.FilesMatched)),
				Suggestion: "Use file_a and file_b params, or reference a step that matches 2+ files",
			}
		}
		fileA = refResult.FilesMatched[0]
		fileB = refResult.FilesMatched[1]
	} else {
		fileA, _ = step.Params["file_a"].(string)
		fileB, _ = step.Params["file_b"].(string)
	}

	fileA = NormalizePath(fileA)
	fileB = NormalizePath(fileB)

	// Read both files
	contentA, err := os.ReadFile(fileA)
	if err != nil {
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "diff",
			Param:   "file_a",
			Message: fmt.Sprintf("failed to read %s", fileA),
			Err:     err,
		}
	}

	contentB, err := os.ReadFile(fileB)
	if err != nil {
		return &PipelineStepError{
			StepID:  step.ID,
			Action:  "diff",
			Param:   "file_b",
			Message: fmt.Sprintf("failed to read %s", fileB),
			Err:     err,
		}
	}

	// Line-by-line unified diff
	linesA := strings.Split(string(contentA), "\n")
	linesB := strings.Split(string(contentB), "\n")

	var diffLines []string
	diffLines = append(diffLines, fmt.Sprintf("--- %s", fileA))
	diffLines = append(diffLines, fmt.Sprintf("+++ %s", fileB))

	maxLen := len(linesA)
	if len(linesB) > maxLen {
		maxLen = len(linesB)
	}

	changes := 0
	for i := 0; i < maxLen; i++ {
		var lineA, lineB string
		if i < len(linesA) {
			lineA = linesA[i]
		}
		if i < len(linesB) {
			lineB = linesB[i]
		}

		if lineA != lineB {
			changes++
			if i < len(linesA) {
				diffLines = append(diffLines, fmt.Sprintf("-%s", lineA))
			}
			if i < len(linesB) {
				diffLines = append(diffLines, fmt.Sprintf("+%s", lineB))
			}
		}
	}

	result.FilesMatched = []string{fileA, fileB}
	result.AggregatedContent = strings.Join(diffLines, "\n")
	result.Counts = map[string]int{"changes": changes}

	return nil
}

// executeMerge combines FilesMatched from multiple steps (union or intersection)
func (pe *PipelineExecutor) executeMerge(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult) error {
	mode := "union"
	if m, ok := step.Params["mode"].(string); ok {
		mode = m
	}

	refs := step.InputFromAll
	if len(refs) == 0 && step.InputFrom != "" {
		refs = []string{step.InputFrom}
	}

	if mode == "union" {
		seen := make(map[string]bool)
		var merged []string
		for _, ref := range refs {
			refResult, exists := pipelineCtx.GetStepResult(ref)
			if !exists {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "merge",
					Param:   "input_from_all",
					Message: fmt.Sprintf("referenced step '%s' not found", ref),
				}
			}
			for _, f := range refResult.FilesMatched {
				if !seen[f] {
					seen[f] = true
					merged = append(merged, f)
				}
			}
		}
		result.FilesMatched = merged
	} else if mode == "intersection" {
		// Count occurrences across all steps
		fileCounts := make(map[string]int)
		for _, ref := range refs {
			refResult, exists := pipelineCtx.GetStepResult(ref)
			if !exists {
				return &PipelineStepError{
					StepID:  step.ID,
					Action:  "merge",
					Param:   "input_from_all",
					Message: fmt.Sprintf("referenced step '%s' not found", ref),
				}
			}
			seen := make(map[string]bool) // deduplicate within one step
			for _, f := range refResult.FilesMatched {
				if !seen[f] {
					seen[f] = true
					fileCounts[f]++
				}
			}
		}
		// Only keep files present in ALL referenced steps
		var intersection []string
		for f, count := range fileCounts {
			if count == len(refs) {
				intersection = append(intersection, f)
			}
		}
		result.FilesMatched = intersection
	} else {
		return &PipelineStepError{
			StepID:     step.ID,
			Action:     "merge",
			Param:      "mode",
			Message:    fmt.Sprintf("unsupported merge mode '%s'", mode),
			Suggestion: "Use 'union' or 'intersection'",
		}
	}

	return nil
}

// PipelineSearchMatch represents a search result (internal to pipeline)
type PipelineSearchMatch struct {
	FilePath string
	Count    int
	Content  string
}
