package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// BatchRenameRequest defines parameters for batch rename operations
type BatchRenameRequest struct {
	Path        string   `json:"path"`         // Base directory path
	Mode        string   `json:"mode"`         // Rename mode: find_replace, add_prefix, add_suffix, number_files, regex_rename, change_extension, to_lowercase, to_uppercase
	Find        string   `json:"find"`         // Text to find (for find_replace mode)
	Replace     string   `json:"replace"`      // Replacement text
	Prefix      string   `json:"prefix"`       // Prefix to add
	Suffix      string   `json:"suffix"`       // Suffix to add
	Pattern     string   `json:"pattern"`      // Regex pattern (for regex_rename mode)
	Extension   string   `json:"extension"`    // New extension (for change_extension mode)
	StartNumber int      `json:"start_number"` // Starting number (for number_files mode)
	Padding     int      `json:"padding"`      // Zero padding width (for number_files mode)
	Recursive   bool     `json:"recursive"`    // Process subdirectories
	FilePattern string   `json:"file_pattern"` // File filter pattern (e.g., "*.txt")
	Preview     bool     `json:"preview"`      // Preview mode (dry-run)
	CaseSensitive bool   `json:"case_sensitive"` // Case-sensitive matching
}

// BatchRenameResult holds the results of a batch rename operation
type BatchRenameResult struct {
	Success       bool                  `json:"success"`
	TotalFiles    int                   `json:"total_files"`
	RenamedCount  int                   `json:"renamed_count"`
	SkippedCount  int                   `json:"skipped_count"`
	ErrorCount    int                   `json:"error_count"`
	Preview       bool                  `json:"preview"`
	Operations    []RenameOperation     `json:"operations"`
	Conflicts     []string              `json:"conflicts"`
	Errors        []string              `json:"errors"`
	ExecutionTime string                `json:"execution_time"`
}

// RenameOperation represents a single rename operation
type RenameOperation struct {
	Index       int    `json:"index"`
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	OldName     string `json:"old_name"`
	NewName     string `json:"new_name"`
	Success     bool   `json:"success"`
	Skipped     bool   `json:"skipped"`
	Error       string `json:"error"`
	ConflictWith string `json:"conflict_with,omitempty"`
}

// BatchRenameFiles performs batch file renaming operations
func (e *UltraFastEngine) BatchRenameFiles(ctx context.Context, request BatchRenameRequest) (*BatchRenameResult, error) {
	start := time.Now()

	// Validate request
	if err := e.validateBatchRenameRequest(&request); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	// Validate path
	validPath, err := e.validatePath(request.Path)
	if err != nil {
		return nil, fmt.Errorf("path validation error: %w", err)
	}

	// Check if path exists
	info, err := os.Stat(validPath)
	if err != nil {
		return nil, fmt.Errorf("path does not exist: %w", err)
	}

	// Collect files to rename
	files, err := e.collectFilesForRename(validPath, info.IsDir(), request.Recursive, request.FilePattern)
	if err != nil {
		return nil, fmt.Errorf("error collecting files: %w", err)
	}

	if len(files) == 0 {
		return &BatchRenameResult{
			Success:      true,
			TotalFiles:   0,
			RenamedCount: 0,
			Preview:      request.Preview,
			Operations:   []RenameOperation{},
			ExecutionTime: time.Since(start).String(),
		}, nil
	}

	// Plan rename operations
	operations, conflicts, err := e.planRenameOperations(files, &request)
	if err != nil {
		return nil, fmt.Errorf("error planning operations: %w", err)
	}

	result := &BatchRenameResult{
		Success:      true,
		TotalFiles:   len(files),
		Preview:      request.Preview,
		Operations:   operations,
		Conflicts:    conflicts,
		Errors:       []string{},
	}

	// If preview mode, return without executing
	if request.Preview {
		result.ExecutionTime = time.Since(start).String()
		return result, nil
	}

	// Execute rename operations
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("cannot proceed: %d naming conflicts detected. Use preview mode to review", len(conflicts))
	}

	e.executeRenameOperations(operations, result)

	result.ExecutionTime = time.Since(start).String()
	return result, nil
}

// validateBatchRenameRequest validates the batch rename request
func (e *UltraFastEngine) validateBatchRenameRequest(req *BatchRenameRequest) error {
	if req.Path == "" {
		return fmt.Errorf("path is required")
	}

	validModes := []string{
		"find_replace", "add_prefix", "add_suffix", "number_files",
		"regex_rename", "change_extension", "to_lowercase", "to_uppercase",
	}

	modeValid := false
	for _, mode := range validModes {
		if req.Mode == mode {
			modeValid = true
			break
		}
	}

	if !modeValid {
		return fmt.Errorf("invalid mode '%s'. Valid modes: %s", req.Mode, strings.Join(validModes, ", "))
	}

	// Mode-specific validation
	switch req.Mode {
	case "find_replace":
		if req.Find == "" {
			return fmt.Errorf("'find' parameter is required for find_replace mode")
		}
	case "add_prefix":
		if req.Prefix == "" {
			return fmt.Errorf("'prefix' parameter is required for add_prefix mode")
		}
	case "add_suffix":
		if req.Suffix == "" {
			return fmt.Errorf("'suffix' parameter is required for add_suffix mode")
		}
	case "number_files":
		if req.Padding < 0 {
			req.Padding = 3 // Default padding
		}
	case "regex_rename":
		if req.Pattern == "" {
			return fmt.Errorf("'pattern' parameter is required for regex_rename mode")
		}
		// Test regex pattern
		if _, err := regexp.Compile(req.Pattern); err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	case "change_extension":
		if req.Extension == "" {
			return fmt.Errorf("'extension' parameter is required for change_extension mode")
		}
		// Ensure extension starts with dot
		if !strings.HasPrefix(req.Extension, ".") {
			req.Extension = "." + req.Extension
		}
	}

	return nil
}

// collectFilesForRename collects all files to be renamed
func (e *UltraFastEngine) collectFilesForRename(path string, isDir bool, recursive bool, filePattern string) ([]string, error) {
	var files []string

	// Compile file pattern if provided
	var patternRegex *regexp.Regexp
	if filePattern != "" {
		// Convert glob pattern to regex
		regexPattern := "^" + strings.ReplaceAll(strings.ReplaceAll(filePattern, ".", "\\."), "*", ".*") + "$"
		var err error
		patternRegex, err = regexp.Compile(regexPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid file pattern: %w", err)
		}
	}

	if !isDir {
		// Single file
		if patternRegex == nil || patternRegex.MatchString(filepath.Base(path)) {
			files = append(files, path)
		}
		return files, nil
	}

	// Directory
	walkFn := func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on error
		}

		// Skip if not recursive and not in base directory
		if !recursive && filepath.Dir(currentPath) != path {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check file pattern
		if patternRegex != nil && !patternRegex.MatchString(info.Name()) {
			return nil
		}

		// Validate path
		if _, err := e.validatePath(currentPath); err != nil {
			return nil
		}

		files = append(files, currentPath)
		return nil
	}

	if err := filepath.Walk(path, walkFn); err != nil {
		return nil, err
	}

	return files, nil
}

// planRenameOperations plans all rename operations and detects conflicts
func (e *UltraFastEngine) planRenameOperations(files []string, req *BatchRenameRequest) ([]RenameOperation, []string, error) {
	operations := make([]RenameOperation, len(files))
	newNames := make(map[string]string) // newPath -> oldPath mapping for conflict detection
	var conflicts []string

	for i, oldPath := range files {
		oldName := filepath.Base(oldPath)
		dir := filepath.Dir(oldPath)

		newName, err := e.generateNewName(oldName, i, req)
		if err != nil {
			operations[i] = RenameOperation{
				Index:   i,
				OldPath: oldPath,
				OldName: oldName,
				Error:   err.Error(),
				Skipped: true,
			}
			continue
		}

		// Skip if name unchanged
		if newName == oldName {
			operations[i] = RenameOperation{
				Index:   i,
				OldPath: oldPath,
				OldName: oldName,
				NewName: newName,
				Skipped: true,
			}
			continue
		}

		newPath := filepath.Join(dir, newName)

		// Check for conflicts
		if existingOldPath, exists := newNames[newPath]; exists {
			conflict := fmt.Sprintf("Conflict: '%s' and '%s' both rename to '%s'", oldName, filepath.Base(existingOldPath), newName)
			conflicts = append(conflicts, conflict)
			operations[i] = RenameOperation{
				Index:        i,
				OldPath:      oldPath,
				NewPath:      newPath,
				OldName:      oldName,
				NewName:      newName,
				ConflictWith: filepath.Base(existingOldPath),
				Skipped:      true,
			}
			continue
		}

		// Check if target already exists
		if _, err := os.Stat(newPath); err == nil {
			conflict := fmt.Sprintf("Target already exists: '%s'", newPath)
			conflicts = append(conflicts, conflict)
			operations[i] = RenameOperation{
				Index:   i,
				OldPath: oldPath,
				NewPath: newPath,
				OldName: oldName,
				NewName: newName,
				Error:   "target exists",
				Skipped: true,
			}
			continue
		}

		newNames[newPath] = oldPath
		operations[i] = RenameOperation{
			Index:   i,
			OldPath: oldPath,
			NewPath: newPath,
			OldName: oldName,
			NewName: newName,
			Success: false,
			Skipped: false,
		}
	}

	return operations, conflicts, nil
}

// generateNewName generates a new file name based on the mode
func (e *UltraFastEngine) generateNewName(oldName string, index int, req *BatchRenameRequest) (string, error) {
	ext := filepath.Ext(oldName)
	baseName := strings.TrimSuffix(oldName, ext)

	switch req.Mode {
	case "find_replace":
		if req.CaseSensitive {
			return strings.ReplaceAll(oldName, req.Find, req.Replace), nil
		}
		// Case-insensitive replace
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(req.Find))
		return re.ReplaceAllString(oldName, req.Replace), nil

	case "add_prefix":
		return req.Prefix + oldName, nil

	case "add_suffix":
		return baseName + req.Suffix + ext, nil

	case "number_files":
		num := req.StartNumber + index
		paddedNum := fmt.Sprintf("%0*d", req.Padding, num)
		return paddedNum + "_" + oldName, nil

	case "regex_rename":
		re, err := regexp.Compile(req.Pattern)
		if err != nil {
			return "", err
		}
		if req.Replace == "" {
			// If no replacement specified, just match
			if re.MatchString(oldName) {
				return oldName, nil
			}
			return oldName, fmt.Errorf("pattern does not match")
		}
		return re.ReplaceAllString(oldName, req.Replace), nil

	case "change_extension":
		return baseName + req.Extension, nil

	case "to_lowercase":
		return strings.ToLower(oldName), nil

	case "to_uppercase":
		return strings.ToUpper(oldName), nil

	default:
		return "", fmt.Errorf("unknown mode: %s", req.Mode)
	}
}

// executeRenameOperations executes the rename operations with parallel processing
func (e *UltraFastEngine) executeRenameOperations(operations []RenameOperation, result *BatchRenameResult) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Process operations in parallel using worker pool
	for i := range operations {
		if operations[i].Skipped {
			mu.Lock()
			result.SkippedCount++
			mu.Unlock()
			continue
		}

		wg.Add(1)
		op := &operations[i]

		e.workerPool.Submit(func() {
			defer wg.Done()

			err := os.Rename(op.OldPath, op.NewPath)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				op.Success = false
				op.Error = err.Error()
				result.ErrorCount++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", op.OldName, err))
			} else {
				op.Success = true
				result.RenamedCount++

				// Invalidate cache for old and new paths
				e.cache.InvalidateFile(op.OldPath)
				e.cache.InvalidateFile(op.NewPath)
			}
		})
	}

	wg.Wait()

	result.Success = result.ErrorCount == 0
}

// FormatBatchRenameResult formats the batch rename result for display
func FormatBatchRenameResult(result *BatchRenameResult, compactMode bool) string {
	var sb strings.Builder

	if result.Preview {
		sb.WriteString("📋 Batch Rename Preview (Dry-Run)\n")
	} else {
		if result.Success {
			sb.WriteString("✅ Batch Rename Completed Successfully\n")
		} else {
			sb.WriteString("⚠️ Batch Rename Completed with Errors\n")
		}
	}
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Summary
	if compactMode {
		sb.WriteString(fmt.Sprintf("Files: %d total, ", result.TotalFiles))
		if result.Preview {
			planCount := 0
			for _, op := range result.Operations {
				if !op.Skipped {
					planCount++
				}
			}
			sb.WriteString(fmt.Sprintf("%d to rename", planCount))
		} else {
			sb.WriteString(fmt.Sprintf("%d renamed, %d skipped", result.RenamedCount, result.SkippedCount))
			if result.ErrorCount > 0 {
				sb.WriteString(fmt.Sprintf(", %d errors", result.ErrorCount))
			}
		}
		sb.WriteString(fmt.Sprintf(" | %s\n", result.ExecutionTime))
	} else {
		sb.WriteString("📊 Summary:\n")
		sb.WriteString(fmt.Sprintf("  Total files processed: %d\n", result.TotalFiles))
		if result.Preview {
			planCount := 0
			for _, op := range result.Operations {
				if !op.Skipped {
					planCount++
				}
			}
			sb.WriteString(fmt.Sprintf("  Files to be renamed: %d\n", planCount))
			sb.WriteString(fmt.Sprintf("  Files to skip: %d\n", result.SkippedCount))
		} else {
			sb.WriteString(fmt.Sprintf("  Successfully renamed: %d\n", result.RenamedCount))
			sb.WriteString(fmt.Sprintf("  Skipped: %d\n", result.SkippedCount))
			sb.WriteString(fmt.Sprintf("  Errors: %d\n", result.ErrorCount))
		}
		sb.WriteString(fmt.Sprintf("  Execution time: %s\n", result.ExecutionTime))
	}

	// Conflicts
	if len(result.Conflicts) > 0 {
		sb.WriteString("\n⚠️ Naming Conflicts Detected:\n")
		for _, conflict := range result.Conflicts {
			sb.WriteString(fmt.Sprintf("  • %s\n", conflict))
		}
	}

	// Operations details
	if !compactMode || result.Preview {
		sb.WriteString("\n📋 Operations:\n")
		maxShow := 50
		if compactMode {
			maxShow = 20
		}

		shown := 0
		for _, op := range result.Operations {
			if op.Skipped && !result.Preview {
				continue // Skip showing skipped items in execution mode
			}
			if shown >= maxShow {
				remaining := 0
				for _, o := range result.Operations[shown:] {
					if !o.Skipped || result.Preview {
						remaining++
					}
				}
				sb.WriteString(fmt.Sprintf("  ... and %d more operations\n", remaining))
				break
			}

			status := "✓"
			if op.Skipped {
				status = "⊘"
			} else if !op.Success && !result.Preview {
				status = "✗"
			} else if result.Preview {
				status = "→"
			}

			if compactMode {
				sb.WriteString(fmt.Sprintf("  %s %s → %s", status, op.OldName, op.NewName))
				if op.Error != "" {
					sb.WriteString(fmt.Sprintf(" [%s]", op.Error))
				}
				sb.WriteString("\n")
			} else {
				sb.WriteString(fmt.Sprintf("  %s [%d] %s\n", status, op.Index+1, op.OldName))
				if !op.Skipped || result.Preview {
					sb.WriteString(fmt.Sprintf("      → %s\n", op.NewName))
				}
				if op.Error != "" {
					sb.WriteString(fmt.Sprintf("      Error: %s\n", op.Error))
				}
				if op.ConflictWith != "" {
					sb.WriteString(fmt.Sprintf("      Conflict with: %s\n", op.ConflictWith))
				}
			}
			shown++
		}
	}

	// Errors summary
	if len(result.Errors) > 0 && !compactMode {
		sb.WriteString("\n❌ Errors:\n")
		for i, err := range result.Errors {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("  ... and %d more errors\n", len(result.Errors)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  • %s\n", err))
		}
	}

	if result.Preview {
		sb.WriteString("\n💡 This was a preview. Set preview=false to execute the rename operations.\n")
	}

	return sb.String()
}
