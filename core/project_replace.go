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
	// Blocked is true when the risk gate (HIGH/CRITICAL without force=true)
	// refused to write anything. The result is a pure preview: counts describe
	// what WOULD change, and the disk is guaranteed untouched.
	Blocked bool `json:"blocked,omitempty"`
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
//   - force: required to apply HIGH/CRITICAL-risk batches. Without it the
//     operation is a pure preview — nothing is written (issue: the force
//     flag was advertised in the schema and in the risk warning but never
//     enforced, so a CRITICAL replace applied silently and re-running with
//     force=true applied it a SECOND time)
func (e *UltraFastEngine) ProjectReplace(ctx context.Context, path, find, replace string, literal, caseSensitive bool, fileTypes string, includePaths, excludePaths []string, preview, createBackup, parallel bool, maxFiles int, force bool) (*ProjectReplaceResult, error) {
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
		return nil, e.AccessDeniedError("project_replace", path)
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

	// Calculate risk assessment before any writes AND collect the list of files
	// that actually contain matches. We do both in one read pass so the per-file
	// counts stay in sync with the backup set we will snapshot below.
	filesWithMatches := make([]string, 0, len(matchedFiles))
	var totalOccurrences int
	for _, f := range matchedFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		count := countMatches(string(content), literal, caseSensitive, literalText, regexPattern)
		if count > 0 {
			filesWithMatches = append(filesWithMatches, f)
			totalOccurrences += count
		}
	}

	riskLevel := "LOW"
	if totalOccurrences >= 1000 || len(filesWithMatches) >= 80 {
		riskLevel = "CRITICAL"
	} else if totalOccurrences >= 500 || len(filesWithMatches) >= 50 {
		riskLevel = "HIGH"
	} else if totalOccurrences >= 100 || len(filesWithMatches) >= 30 {
		riskLevel = "MEDIUM"
	}

	result := &ProjectReplaceResult{
		FilesChanged: len(filesWithMatches),
		DryRun:       preview,
		RiskLevel:    riskLevel,
	}

	if preview {
		// Just count
		result.TotalReplaced = totalOccurrences
		return result, nil
	}

	// Risk gate: HIGH/CRITICAL batches require force=true. Without it, NOTHING
	// is written and no backup is created — the result is a pure preview with
	// an explicit Blocked marker. Previously the writes went through anyway
	// and the warning said "Use force=true to proceed", which tricked callers
	// into re-running with force=true and applying the replacement a second
	// time (producing e.g. examples/examples/... duplications).
	if (riskLevel == "HIGH" || riskLevel == "CRITICAL") && !force {
		result.Blocked = true
		result.DryRun = true
		result.TotalReplaced = totalOccurrences
		result.RiskWarning = fmt.Sprintf("⚠️ %s risk: %d files, %d replacements — BLOCKED, no files were modified. Re-run with force=true to apply.", riskLevel, result.FilesChanged, totalOccurrences)
		return result, nil
	}

	// Snapshot the pre-replace bytes BEFORE any writes.
	// v4.5.29+: previously the backup was created *after* all writes completed,
	// which meant a successful "restore" only rolled back to the post-replace
	// state — the original content was already gone. With this guard the
	// backup captures disk state prior to the first atomic write, so a
	// subsequent `backup(action:"restore", backup_id:...)` reverts the entire
	// batch to its pre-replace state. If `create_backup:true` and the snapshot
	// fails, abort without touching any file (fail-closed).
	var backupID string
	if createBackup && e.backupManager != nil && len(filesWithMatches) > 0 {
		backupID, err = e.backupManager.CreateBatchBackup(filesWithMatches, "project_replace",
			fmt.Sprintf("ProjectReplace: %d files, %d replacements, risk=%s",
				len(filesWithMatches), totalOccurrences, riskLevel))
		if err != nil {
			return nil, fmt.Errorf("backup failed (no files modified): %w", err)
		}
		result.BackupID = backupID
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
	var firstProcessErr error

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

		// Count replacements using the shared helper so the per-file count here
		// cannot drift from the pre-write count that built filesWithMatches.
		replaced := countMatches(string(content), literal, caseSensitive, literalText, regexPattern)
		if replaced == 0 {
			return nil
		}

		// Write back atomically without re-entering the engine semaphore held by
		// ProjectReplace. Refresh every cache surface and the session OCC baseline
		// before another tool can verify or edit the file.
		fileMode := os.FileMode(0644)
		if info, statErr := os.Stat(f); statErr == nil {
			fileMode = info.Mode()
		}
		if err := atomicWriteFile(f, []byte(newContent), fileMode); err != nil {
			return err
		}
		e.invalidateMutatedPath(f)
		RecordWriteHash(NormalizePath(f), contentHashFNV(newContent))

		mu.Lock()
		results = append(results, fileResult{f, replaced, oldSize, int64(len(newContent))})
		mu.Unlock()

		return nil
	}

	if parallel && e.workerPool != nil {
		var wg sync.WaitGroup
		for _, f := range filesWithMatches {
			wg.Add(1)
			filePath := f
			if submitErr := e.workerPool.Submit(func() {
				defer wg.Done()
				if processErr := processFile(filePath); processErr != nil {
					mu.Lock()
					if firstProcessErr == nil {
						firstProcessErr = processErr
					}
					mu.Unlock()
				}
			}); submitErr != nil {
				wg.Done()
				mu.Lock()
				if firstProcessErr == nil {
					firstProcessErr = submitErr
				}
				mu.Unlock()
			}
		}
		wg.Wait()
	} else {
		for _, f := range filesWithMatches {
			if processErr := processFile(f); processErr != nil {
				firstProcessErr = processErr
				break
			}
		}
	}
	if firstProcessErr != nil {
		return nil, fmt.Errorf("project replace failed: %w", firstProcessErr)
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

	// Register the pre-write backup in the per-file undo chain so that a later
	// `backup(action:"undo_last", file_path:"...")` can step back through the
	// project_replace layer (mirrors how `edit_file` registers its chain entry).
	// Without this, undo_last would have no idea which backup to revert to for
	// files touched by project_replace. No-op when no backup was created.
	if backupID != "" {
		for _, r := range results {
			e.SetCurrentBackupID(r.path, backupID)
		}
	}

	if riskLevel == "HIGH" || riskLevel == "CRITICAL" {
		// Reaching here means force=true was passed (the risk gate above
		// returns early otherwise) — say so explicitly so the output cannot
		// be misread as "still needs force".
		result.RiskWarning = fmt.Sprintf("⚠️ %s risk applied (force=true): %d files, %d replacements were written.", riskLevel, result.FilesChanged, result.TotalReplaced)
	}

	return result, nil
}

// countMatches returns the number of matches that the configured find/replace
// pattern would produce for content. Centralised so the pre-write count loop
// (which builds the backup set) and the in-loop replacement count stay in
// lockstep — drifting them would risk backing up files that won't actually be
// changed or skipping files that will.
func countMatches(content string, literal, caseSensitive bool, literalText string, regexPattern *regexp.Regexp) int {
	if literal {
		if caseSensitive {
			return strings.Count(content, literalText)
		}
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(literalText))
		return len(re.FindAllString(content, -1))
	}
	return len(regexPattern.FindAllString(content, -1))
}
