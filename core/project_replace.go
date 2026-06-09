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

// ProjectReplaceResult is the result of a project_replace operation
type ProjectReplaceResult struct {
	FilesChanged   int                        `json:"files_changed"`
	TotalReplaced  int                        `json:"total_replacements"`
	BackupID       string                     `json:"backup_id,omitempty"`
	ChainID        string                     `json:"chain_id,omitempty"`
	PerFileResults []ProjectReplaceFileResult `json:"per_file,omitempty"`
	RiskLevel      string                     `json:"risk_level,omitempty"`
	RiskWarning    string                     `json:"risk_warning,omitempty"`
	DryRun         bool                       `json:"dry_run"`
}

// ProjectReplaceFileResult contains results for a single file
type ProjectReplaceFileResult struct {
	Path     string `json:"path"`
	Replaced int    `json:"replacements"`
	OldSize  int64  `json:"old_size"`
	NewSize  int64  `json:"new_size"`
}

// ProjectReplace performs a find/replace across an entire project tree.
// It's optimized for single-call project-wide replacements.
//
// Parameters:
//   - path: root directory to scan
//   - find: text or regex pattern to find
//   - replace: replacement text
//   - literal: if true, find is literal text; if false, find is regex
//   - caseSensitive: whether matching is case-sensitive
//   - fileTypes: comma-separated list of file extensions to include (e.g., ".php,.html")
//   - includePaths: optional list of glob patterns to include
//   - excludePaths: optional list of glob patterns to exclude
//   - preview: if true, only count replacements without writing
//   - createBackup: if true, create a single consolidated backup
//   - parallel: if true, process files in parallel
//   - maxFiles: maximum number of files to process (safety cap)
func (e *UltraFastEngine) ProjectReplace(ctx context.Context, path, find, replace string, literal, caseSensitive bool, fileTypes string, includePaths, excludePaths []string, preview, createBackup, parallel bool, maxFiles int) (*ProjectReplaceResult, error) {
	// Normalize path
	path = NormalizePath(path)

	// Acquire operation semaphore
	if err := e.acquireOperation(ctx, "project_replace"); err != nil {
		return nil, err
	}
	start := time.Now()
	defer e.releaseOperation("project_replace", start)

	// Check access control
	if !e.IsPathAllowed(path) {
		return nil, &PathError{Op: "project_replace", Path: path, Err: fmt.Errorf("access denied")}
	}

	// Parse file types
	var extensions []string
	if fileTypes != "" {
		for _, ext := range strings.Split(fileTypes, ",") {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				if !strings.HasPrefix(ext, ".") {
					ext = "." + ext
				}
				extensions = append(extensions, ext)
			}
		}
	}

	// Compile pattern
	var regexPattern *regexp.Regexp
	var literalText string
	if literal {
		literalText = find
		if !caseSensitive {
			regexPattern, _ = regexp.Compile("(?i)" + regexp.QuoteMeta(find))
		}
	} else {
		flags := "" // TODO: handle case sensitivity in regex
		if !caseSensitive {
			flags = "(?i)"
		}
		var err error
		regexPattern, err = regexp.Compile(flags + find)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	// Discover files
	var matchedFiles []string
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			// Check if this directory should be excluded
			rel, err := filepath.Rel(path, filePath)
			if err == nil {
				for _, excl := range excludePaths {
					// Handle ** globs (e.g., "jotajotape/**" matches all subdirs)
					if strings.HasSuffix(excl, "/**") || strings.HasSuffix(excl, "\\**") {
						prefix := excl[:len(excl)-3] // Remove /**
						if strings.HasPrefix(rel, prefix+"/") || rel == prefix {
							return filepath.SkipDir
						}
					} else if matched, _ := filepath.Match(excl, rel); matched {
						return filepath.SkipDir
					} else if strings.HasPrefix(rel, excl) {
						return filepath.SkipDir
					}
				}
			}
			return nil
		}

		// Check max files limit
		if maxFiles > 0 && len(matchedFiles) >= maxFiles {
			return nil
		}

		// Check file type filter
		if len(extensions) > 0 {
			ext := strings.ToLower(filepath.Ext(filePath))
			matchedExt := false
			for _, e := range extensions {
				if strings.ToLower(e) == ext {
					matchedExt = true
					break
				}
			}
			if !matchedExt {
				return nil
			}
		}

		// Check include paths
		if len(includePaths) > 0 {
			rel, err := filepath.Rel(path, filePath)
			if err != nil {
				return nil
			}
			matched := false
			for _, incl := range includePaths {
				if m, _ := filepath.Match(incl, rel); m {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		matchedFiles = append(matchedFiles, filePath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}

	if len(matchedFiles) == 0 {
		return &ProjectReplaceResult{
			FilesChanged:  0,
			TotalReplaced: 0,
			DryRun:        preview,
		}, nil
	}

	// Calculate risk assessment before any writes
	var totalOccurrences int
	for _, f := range matchedFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if literal {
			if caseSensitive {
				totalOccurrences += strings.Count(string(content), literalText)
			} else {
				// Case-insensitive count
				re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(literalText))
				totalOccurrences += len(re.FindAllString(string(content), -1))
			}
		} else {
			totalOccurrences += len(regexPattern.FindAllString(string(content), -1))
		}
	}

	riskLevel := "LOW"
	if totalOccurrences >= 1000 || len(matchedFiles) >= 80 {
		riskLevel = "CRITICAL"
	} else if totalOccurrences >= 500 || len(matchedFiles) >= 50 {
		riskLevel = "HIGH"
	} else if totalOccurrences >= 100 || len(matchedFiles) >= 30 {
		riskLevel = "MEDIUM"
	}

	result := &ProjectReplaceResult{
		FilesChanged: len(matchedFiles),
		DryRun:       preview,
		RiskLevel:    riskLevel,
	}

	if preview {
		// Just count
		result.TotalReplaced = totalOccurrences
		return result, nil
	}

	// Process files
	type fileResult struct {
		path     string
		replaced int
		oldSize  int64
		newSize  int64
	}

	var results []fileResult
	var mu sync.Mutex

	processFile := func(f string) error {
		content, err := os.ReadFile(f)
		if err != nil {
			return nil
		}
		oldSize := int64(len(content))
		var newContent string

		if literal {
			if caseSensitive {
				newContent = strings.ReplaceAll(string(content), literalText, replace)
			} else {
				re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(literalText))
				newContent = re.ReplaceAllString(string(content), replace)
			}
		} else {
			newContent = regexPattern.ReplaceAllString(string(content), replace)
		}

		// Count replacements
		replaced := strings.Count(string(content), literalText)
		if literal && !caseSensitive {
			re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(literalText))
			replaced = len(re.FindAllString(string(content), -1))
		} else if !literal {
			replaced = len(regexPattern.FindAllString(string(content), -1))
		}

		if replaced == 0 {
			return nil
		}

		// Write back
		if err := os.WriteFile(f, []byte(newContent), 0644); err != nil {
			return err
		}

		mu.Lock()
		results = append(results, fileResult{f, replaced, oldSize, int64(len(newContent))})
		mu.Unlock()

		return nil
	}

	if parallel && e.workerPool != nil {
		var wg sync.WaitGroup
		for _, f := range matchedFiles {
			wg.Add(1)
			e.workerPool.Submit(func() {
				processFile(f)
				wg.Done()
			})
		}
		wg.Wait()
	} else {
		for _, f := range matchedFiles {
			processFile(f)
		}
	}

	// Populate result
	result.TotalReplaced = 0
	result.FilesChanged = len(results) // Only files that actually had replacements
	result.PerFileResults = make([]ProjectReplaceFileResult, 0, len(results))
	for _, r := range results {
		result.TotalReplaced += r.replaced
		result.PerFileResults = append(result.PerFileResults, ProjectReplaceFileResult{
			Path:     r.path,
			Replaced: r.replaced,
			OldSize:  r.oldSize,
			NewSize:  r.newSize,
		})
	}

	// Create backup AFTER processing, only for files that actually had replacements
	var backupID string
	if createBackup && e.backupManager != nil && len(results) > 0 {
		changedFiles := make([]string, 0, len(results))
		for _, r := range results {
			changedFiles = append(changedFiles, r.path)
		}
		backupID, err = e.backupManager.CreateBatchBackup(changedFiles, "project_replace",
			fmt.Sprintf("ProjectReplace: %d files, %d replacements, risk=%s", len(results), result.TotalReplaced, riskLevel))
		if err != nil {
			return nil, fmt.Errorf("backup failed: %w", err)
		}
		result.BackupID = backupID
	}

	if riskLevel == "HIGH" || riskLevel == "CRITICAL" {
		result.RiskWarning = fmt.Sprintf("⚠️ %s risk: %d files, %d replacements. Use force=true to proceed with future operations.", riskLevel, result.FilesChanged, result.TotalReplaced)
	}

	return result, nil
}
