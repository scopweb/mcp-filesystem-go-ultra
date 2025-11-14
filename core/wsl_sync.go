package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WSLWindowsCopy copies a file from WSL to Windows or vice versa
func (e *UltraFastEngine) WSLWindowsCopy(ctx context.Context, srcPath, dstPath string, createDirs bool) error {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "wsl_copy"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("wsl_copy", start)

	// Normalize source path
	srcPath = NormalizePath(srcPath)

	// If destination is empty, auto-convert
	if dstPath == "" {
		var err error
		if IsWSLPath(srcPath) {
			dstPath, err = WSLToWindows(srcPath)
			if err != nil {
				return fmt.Errorf("failed to auto-convert WSL path to Windows: %v", err)
			}
		} else if IsWindowsPath(srcPath) {
			dstPath, err = WindowsToWSL(srcPath)
			if err != nil {
				return fmt.Errorf("failed to auto-convert Windows path to WSL: %v", err)
			}
		} else {
			return fmt.Errorf("could not determine path type for auto-conversion: %s", srcPath)
		}
	} else {
		dstPath = NormalizePath(dstPath)
	}

	// Check if source exists
	srcInfo, err := os.Stat(srcPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("source does not exist: %s", srcPath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat source: %v", err)
	}

	// If source is a directory, copy recursively
	if srcInfo.IsDir() {
		return e.copyDirectoryRecursive(srcPath, dstPath, createDirs)
	}

	// Copy single file
	return CopyFileWithConversion(srcPath, dstPath, createDirs)
}

// copyDirectoryRecursive copies a directory recursively
func (e *UltraFastEngine) copyDirectoryRecursive(srcDir, dstDir string, createDirs bool) error {
	// Create destination directory
	if createDirs {
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %v", err)
		}
	}

	// Walk through source directory
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dstDir, relPath)

		if d.IsDir() {
			// Create directory
			return os.MkdirAll(dstPath, 0755)
		}

		// Copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %v", path, err)
		}

		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %v", dstPath, err)
		}

		return nil
	})
}

// SyncWorkspace syncs files between WSL and Windows
func (e *UltraFastEngine) SyncWorkspace(ctx context.Context, direction string, filterPattern string, dryRun bool) (result map[string]interface{}, err error) {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "sync_workspace"); err != nil {
		return nil, err
	}

	start := time.Now()
	defer e.releaseOperation("sync_workspace", start)

	result = make(map[string]interface{})
	syncedFiles := []string{}
	errors := []string{}

	// Determine source and destination based on direction
	var srcDirs, dstDirs []string

	switch direction {
	case "wsl_to_windows":
		// Common WSL directories
		wslHome := GetWSLHome()
		srcDirs = []string{
			filepath.Join(wslHome, "claude"),
			filepath.Join(wslHome, "projects"),
			"/tmp/claude",
		}

		_, winHome := GetWindowsHome()
		if winHome != "" {
			dstDirs = []string{
				filepath.Join(winHome, "claude"),
				filepath.Join(winHome, "projects"),
				filepath.Join(winHome, "AppData", "Local", "Temp", "claude"),
			}
		}

	case "windows_to_wsl":
		wslHome := GetWSLHome()
		dstDirs = []string{
			filepath.Join(wslHome, "claude"),
			filepath.Join(wslHome, "projects"),
			"/tmp/claude",
		}

		_, winHome := GetWindowsHome()
		if winHome != "" {
			srcDirs = []string{
				filepath.Join(winHome, "claude"),
				filepath.Join(winHome, "projects"),
				filepath.Join(winHome, "AppData", "Local", "Temp", "claude"),
			}
		}

	case "bidirectional":
		return nil, fmt.Errorf("bidirectional sync not yet implemented")

	default:
		return nil, fmt.Errorf("invalid direction: %s (must be wsl_to_windows, windows_to_wsl, or bidirectional)", direction)
	}

	if len(srcDirs) == 0 || len(dstDirs) == 0 {
		return nil, fmt.Errorf("could not determine source or destination directories")
	}

	// Sync each directory pair
	for i, srcDir := range srcDirs {
		if i >= len(dstDirs) {
			break
		}
		dstDir := dstDirs[i]

		// Check if source exists
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}

		// Walk through source directory
		err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				errors = append(errors, fmt.Sprintf("error accessing %s: %v", path, err))
				return nil
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Apply filter pattern if specified
			if filterPattern != "" {
				matched, err := filepath.Match(filterPattern, filepath.Base(path))
				if err != nil {
					errors = append(errors, fmt.Sprintf("invalid filter pattern: %v", err))
					return nil
				}
				if !matched {
					return nil
				}
			}

			// Calculate destination path
			relPath, err := filepath.Rel(srcDir, path)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to get relative path for %s: %v", path, err))
				return nil
			}

			dstPath := filepath.Join(dstDir, relPath)

			// If dry run, just record what would be synced
			if dryRun {
				syncedFiles = append(syncedFiles, fmt.Sprintf("%s -> %s", path, dstPath))
				return nil
			}

			// Create destination directory
			dstFileDir := filepath.Dir(dstPath)
			if err := os.MkdirAll(dstFileDir, 0755); err != nil {
				errors = append(errors, fmt.Sprintf("failed to create directory %s: %v", dstFileDir, err))
				return nil
			}

			// Copy file
			data, err := os.ReadFile(path)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to read %s: %v", path, err))
				return nil
			}

			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				errors = append(errors, fmt.Sprintf("failed to write %s: %v", dstPath, err))
				return nil
			}

			syncedFiles = append(syncedFiles, dstPath)
			return nil
		})

		if err != nil {
			errors = append(errors, fmt.Sprintf("error walking directory %s: %v", srcDir, err))
		}
	}

	result["synced_files"] = syncedFiles
	result["synced_count"] = len(syncedFiles)
	result["errors"] = errors
	result["error_count"] = len(errors)
	result["direction"] = direction
	result["filter_pattern"] = filterPattern
	result["dry_run"] = dryRun

	return result, nil
}

// GetWSLWindowsStatus returns the current WSL/Windows integration status
func (e *UltraFastEngine) GetWSLWindowsStatus(ctx context.Context) (map[string]interface{}, error) {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "wsl_status"); err != nil {
		return nil, err
	}

	start := time.Now()
	defer e.releaseOperation("wsl_status", start)

	status := make(map[string]interface{})

	// Detect environment
	isWSL, winUser := DetectEnvironment()
	status["is_wsl"] = isWSL
	status["windows_user"] = winUser

	if isWSL {
		status["environment"] = "WSL"
	} else if os.PathSeparator == '\\' {
		status["environment"] = "Windows"
	} else {
		status["environment"] = "Linux"
	}

	// Get home directories
	wslHome := GetWSLHome()
	wslWinHome, winHome := GetWindowsHome()

	status["wsl_home"] = wslHome
	status["windows_home_wsl_style"] = wslWinHome
	status["windows_home_windows_style"] = winHome

	// Check if common directories exist
	commonDirs := make(map[string]bool)
	checkDirs := []string{
		filepath.Join(wslHome, "claude"),
		filepath.Join(wslHome, "projects"),
		"/tmp/claude",
	}

	if wslWinHome != "" {
		checkDirs = append(checkDirs,
			filepath.Join(wslWinHome, "claude"),
			filepath.Join(wslWinHome, "Documents"),
		)
	}

	for _, dir := range checkDirs {
		if _, err := os.Stat(dir); err == nil {
			commonDirs[dir] = true
		} else {
			commonDirs[dir] = false
		}
	}

	status["directory_status"] = commonDirs

	// Get system info
	status["os"] = os.PathSeparator
	status["path_separator"] = string(os.PathSeparator)

	// Check for WSL interop
	if isWSL {
		// Check if we can execute Windows commands
		_, err := os.Stat("/mnt/c/Windows/System32/cmd.exe")
		status["windows_interop_available"] = err == nil
	}

	return status, nil
}

// AutoCopyToWindows attempts to automatically copy a file to Windows after writing (if enabled)
func (e *UltraFastEngine) AutoCopyToWindows(srcPath string) error {
	// Only proceed if we're in WSL and the source is a WSL path
	isWSL, _ := DetectEnvironment()
	if !isWSL || !IsWSLPath(srcPath) {
		return nil
	}

	// Convert to Windows path
	winPath, err := WSLToWindows(srcPath)
	if err != nil {
		// Silent fail - this is a best-effort operation
		return nil
	}

	// Copy the file
	return CopyFileWithConversion(srcPath, winPath, true)
}

// MatchesPattern checks if a filename matches a pattern (supports wildcards)
func MatchesPattern(filename, pattern string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}

	// Handle multiple patterns separated by comma
	patterns := strings.Split(pattern, ",")
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		matched, err := filepath.Match(p, filepath.Base(filename))
		if err == nil && matched {
			return true
		}
	}

	return false
}
