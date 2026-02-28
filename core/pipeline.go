package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PipelineExecutor executes multi-step file transformation pipelines
type PipelineExecutor struct {
	engine *UltraFastEngine
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

	// Step 6: Execute steps sequentially
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
	default:
		err = fmt.Errorf("unsupported action: %s", step.Action)
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
		return fmt.Errorf("search failed: %w", err)
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

// executeReadRanges reads file contents
func (pe *PipelineExecutor) executeReadRanges(ctx context.Context, step PipelineStep, pipelineCtx *PipelineContext, result *StepResult) error {
	// Get input files
	files, err := step.getInputFiles(pipelineCtx)
	if err != nil {
		return err
	}

	result.Content = make(map[string]string)
	result.FilesMatched = files

	for _, filePath := range files {
		// Normalize path
		normalizedPath := NormalizePath(filePath)

		// Check access
		if len(pe.engine.config.AllowedPaths) > 0 {
			if !pe.engine.isPathAllowed(normalizedPath) {
				return &PathError{Op: "read_ranges", Path: normalizedPath, Err: fmt.Errorf("access denied")}
			}
		}

		// Read file
		content, err := os.ReadFile(normalizedPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filePath, err)
		}

		result.Content[filePath] = string(content)
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
		return fmt.Errorf("operation blocked due to %s risk (%d files, %d occurrences). Use force=true to proceed",
			riskLevel, batchImpact.TotalFiles, batchImpact.TotalOccurrences)
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
			editResult, err := pe.engine.EditFile(normalizedPath, oldText, newText, true)
			if err != nil {
				return fmt.Errorf("edit failed for %s: %w", filePath, err)
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
		return fmt.Errorf("edits parameter is required")
	}

	var edits []MultiEditOperation
	switch v := editsParam.(type) {
	case []interface{}:
		for i, item := range v {
			editMap, ok := item.(map[string]interface{})
			if !ok {
				return fmt.Errorf("edits[%d] is not an object", i)
			}
			oldText, _ := editMap["old_text"].(string)
			newText, _ := editMap["new_text"].(string)
			if oldText == "" {
				return fmt.Errorf("edits[%d].old_text is required", i)
			}
			edits = append(edits, MultiEditOperation{OldText: oldText, NewText: newText})
		}
	default:
		return fmt.Errorf("edits parameter has invalid type: %T", v)
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
		return fmt.Errorf("operation blocked due to %s risk. Use force=true to proceed", riskLevel)
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
			editResult, err := pe.engine.MultiEdit(ctx, normalizedPath, edits)
			if err != nil {
				return fmt.Errorf("multi-edit failed for %s: %w", filePath, err)
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
		return fmt.Errorf("patterns parameter is required")
	}

	var patterns []TransformPattern
	switch v := patternsParam.(type) {
	case []interface{}:
		for i, item := range v {
			patternMap, ok := item.(map[string]interface{})
			if !ok {
				return fmt.Errorf("patterns[%d] is not an object", i)
			}
			pattern, _ := patternMap["pattern"].(string)
			replacement, _ := patternMap["replacement"].(string)
			if pattern == "" {
				return fmt.Errorf("patterns[%d].pattern is required", i)
			}
			patterns = append(patterns, TransformPattern{Pattern: pattern, Replacement: replacement})
		}
	default:
		return fmt.Errorf("patterns parameter has invalid type: %T", v)
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
		return fmt.Errorf("operation blocked due to %s risk. Use force=true to proceed", riskLevel)
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
			return fmt.Errorf("regex transform failed for %s: %w", filePath, err)
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
		return fmt.Errorf("destination parameter is required")
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
				return fmt.Errorf("copy failed for %s: %w", filePath, err)
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
		return fmt.Errorf("destination parameter is required")
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
				return fmt.Errorf("rename failed for %s: %w", filePath, err)
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
				return fmt.Errorf("delete failed for %s: %w", filePath, err)
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

// PipelineSearchMatch represents a search result (internal to pipeline)
type PipelineSearchMatch struct {
	FilePath string
	Count    int
	Content  string
}
