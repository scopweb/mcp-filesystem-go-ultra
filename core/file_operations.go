package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RenameFile renames a file or directory
func (e *UltraFastEngine) RenameFile(ctx context.Context, oldPath, newPath string) error {
	// Normalize path (handles WSL ↔ Windows conversion)
	oldPath = NormalizePath(oldPath)
	newPath = NormalizePath(newPath)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "rename"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("rename", start)

	// Check if both paths are allowed (security + access control)
	if !e.IsPathAllowed(oldPath) {
		return fmt.Errorf("access denied: source path '%s' is not in allowed paths", oldPath)
	}
	if !e.IsPathAllowed(newPath) {
		return fmt.Errorf("access denied: destination path '%s' is not in allowed paths", newPath)
	}

	// Check if source exists
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return fmt.Errorf("source file does not exist: %s", oldPath)
	}

	// Check if destination already exists
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("destination file already exists: %s", newPath)
	}

	// Ensure destination directory exists
	destDir := filepath.Dir(newPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Perform the rename
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	// Invalidate cache entries for both paths
	e.cache.InvalidateFile(oldPath)
	e.cache.InvalidateFile(newPath)

	// Also invalidate parent directories
	e.cache.InvalidateDirectory(filepath.Dir(oldPath))
	e.cache.InvalidateDirectory(filepath.Dir(newPath))

	return nil
}

// SoftDeleteFile moves a file to a "filesdelete" folder for later deletion
func (e *UltraFastEngine) SoftDeleteFile(ctx context.Context, path string) error {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "softdelete"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("softdelete", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
	}
	// Prevent soft-deletion of allowed-path roots (would move entire tree to trash)
	if len(e.config.AllowedPaths) > 0 && e.IsAllowedPathRoot(path) {
		return fmt.Errorf("access denied: cannot delete allowed-path root '%s'", path)
	}

	// Check if source exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", path)
	}

	// Execute pre-delete hook
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreDelete,
		ToolName:   "delete_file",
		FilePath:   path,
		Operation:  "soft_delete",
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreDelete, hookCtx); err != nil {
		return fmt.Errorf("pre-delete hook denied operation: %w", err)
	}

	// Determine the root directory (where to create filesdelete folder)
	// If we have allowed paths, use the first one, otherwise use the parent of the file
	var rootDir string
	if len(e.config.AllowedPaths) > 0 {
		rootDir = e.config.AllowedPaths[0]
	} else {
		// Find a reasonable root directory - go up until we find a directory that looks like a project root
		rootDir = filepath.Dir(path)
		for {
			parent := filepath.Dir(rootDir)
			if parent == rootDir { // reached root
				break
			}
			// Look for common project indicators
			if hasProjectIndicators(rootDir) {
				break
			}
			rootDir = parent
		}
	}

	// Create the filesdelete directory
	deleteDir := filepath.Join(rootDir, "filesdelete")
	if err := os.MkdirAll(deleteDir, 0755); err != nil {
		return fmt.Errorf("failed to create filesdelete directory: %w", err)
	}

	// Create destination path maintaining relative structure
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute root directory: %w", err)
	}

	relPath, err := filepath.Rel(absRootDir, absPath)
	if err != nil {
		// If we can't get relative path, use just the filename with timestamp
		filename := filepath.Base(path)
		timestamp := time.Now().Format("20060102_150405")
		relPath = fmt.Sprintf("%s_%s", timestamp, filename)
	}

	destPath := filepath.Join(deleteDir, relPath)

	// Ensure destination directory exists
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory in filesdelete: %w", err)
	}

	// If destination already exists, add timestamp suffix
	if _, err := os.Stat(destPath); err == nil {
		timestamp := time.Now().Format("_20060102_150405")
		ext := filepath.Ext(destPath)
		nameWithoutExt := destPath[:len(destPath)-len(ext)]
		destPath = nameWithoutExt + timestamp + ext
	}

	// Move the file
	if err := os.Rename(path, destPath); err != nil {
		return fmt.Errorf("failed to move file to filesdelete: %w", err)
	}

	// Invalidate cache entries
	e.cache.InvalidateFile(path)
	e.cache.InvalidateDirectory(filepath.Dir(path))

	// Execute post-delete hook (best-effort)
	hookCtx.Event = HookPostDelete
	hookCtx.Metadata = map[string]interface{}{"dest_path": destPath}
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostDelete, hookCtx)

	return nil
}

// hasProjectIndicators checks if a directory has common project indicators
func hasProjectIndicators(dir string) bool {
	indicators := []string{
		".git", ".gitignore", "package.json", "go.mod", "Cargo.toml",
		"requirements.txt", "pom.xml", "build.gradle", ".project",
		"Makefile", "README.md", ".vscode", ".idea",
	}

	for _, indicator := range indicators {
		if _, err := os.Stat(filepath.Join(dir, indicator)); err == nil {
			return true
		}
	}
	return false
}

// CreateDirectory creates a new directory (and parents if needed)
func (e *UltraFastEngine) CreateDirectory(ctx context.Context, path string) error {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "createdir"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("createdir", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
	}

	// Check if directory already exists
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("directory already exists: %s", path)
		}
		return fmt.Errorf("a file with that name already exists: %s", path)
	}

	// Execute pre-create hook
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreCreate,
		ToolName:   "create_directory",
		FilePath:   path,
		Operation:  "create_directory",
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreCreate, hookCtx); err != nil {
		return fmt.Errorf("pre-create hook denied operation: %w", err)
	}

	// Create directory with all parent directories
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Invalidate parent directory cache
	e.cache.InvalidateDirectory(filepath.Dir(path))

	// Execute post-create hook (best-effort)
	hookCtx.Event = HookPostCreate
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostCreate, hookCtx)

	return nil
}

// DeleteFile permanently deletes a file or directory
func (e *UltraFastEngine) DeleteFile(ctx context.Context, path string) error {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "delete"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("delete", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
	}
	// Prevent deletion of allowed-path roots (would wipe entire tree via os.RemoveAll)
	if len(e.config.AllowedPaths) > 0 && e.IsAllowedPathRoot(path) {
		return fmt.Errorf("access denied: cannot delete allowed-path root '%s'", path)
	}

	// Check if file/directory exists
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("file or directory does not exist: %s", path)
	}
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Execute pre-delete hook
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreDelete,
		ToolName:   "delete_file",
		FilePath:   path,
		Operation:  "delete",
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata:   map[string]interface{}{"permanent": true},
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreDelete, hookCtx); err != nil {
		return fmt.Errorf("pre-delete hook denied operation: %w", err)
	}

	// Delete file or directory recursively
	if info.IsDir() {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}

	if err != nil {
		return fmt.Errorf("failed to delete: %w", err)
	}

	// Invalidate cache entries
	e.cache.InvalidateFile(path)
	e.cache.InvalidateDirectory(path)
	e.cache.InvalidateDirectory(filepath.Dir(path))

	// Execute post-delete hook (best-effort)
	hookCtx.Event = HookPostDelete
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostDelete, hookCtx)

	return nil
}

// MoveFile moves a file or directory to a new location
func (e *UltraFastEngine) MoveFile(ctx context.Context, sourcePath, destPath string) error {
	// Normalize path (handles WSL ↔ Windows conversion)
	sourcePath = NormalizePath(sourcePath)
	destPath = NormalizePath(destPath)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "move"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("move", start)

	// Check if both paths are allowed (security + access control)
	if !e.IsPathAllowed(sourcePath) {
		return fmt.Errorf("access denied: source path '%s' is not in allowed paths", sourcePath)
	}
	if !e.IsPathAllowed(destPath) {
		return fmt.Errorf("access denied: destination path '%s' is not in allowed paths", destPath)
	}
	// Prevent moving an allowed-path root (would remove the entire tree from its location)
	if len(e.config.AllowedPaths) > 0 && e.IsAllowedPathRoot(sourcePath) {
		return fmt.Errorf("access denied: cannot move allowed-path root '%s'", sourcePath)
	}

	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("source does not exist: %s", sourcePath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	// Check if destination already exists
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination already exists: %s", destPath)
	}

	// Ensure destination directory exists
	destDir := filepath.Dir(destPath)
	if !sourceInfo.IsDir() {
		// For files, create parent directory
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
	}

	// Execute pre-move hook
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreMove,
		ToolName:   "move_file",
		FilePath:   sourcePath,
		Operation:  "move",
		SourcePath: sourcePath,
		DestPath:   destPath,
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreMove, hookCtx); err != nil {
		return fmt.Errorf("pre-move hook denied operation: %w", err)
	}

	// Perform the move
	if err := os.Rename(sourcePath, destPath); err != nil {
		return fmt.Errorf("failed to move: %w", err)
	}

	// Invalidate cache entries
	e.cache.InvalidateFile(sourcePath)
	e.cache.InvalidateFile(destPath)
	e.cache.InvalidateDirectory(sourcePath)
	e.cache.InvalidateDirectory(destPath)
	e.cache.InvalidateDirectory(filepath.Dir(sourcePath))
	e.cache.InvalidateDirectory(filepath.Dir(destPath))

	// Execute post-move hook (best-effort)
	hookCtx.Event = HookPostMove
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostMove, hookCtx)

	return nil
}

// CopyFile copies a file or directory to a new location
func (e *UltraFastEngine) CopyFile(ctx context.Context, sourcePath, destPath string) error {
	// Normalize path (handles WSL ↔ Windows conversion)
	sourcePath = NormalizePath(sourcePath)
	destPath = NormalizePath(destPath)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "copy"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("copy", start)

	// Check if both paths are allowed (security + access control)
	if !e.IsPathAllowed(sourcePath) {
		return fmt.Errorf("access denied: source path '%s' is not in allowed paths", sourcePath)
	}
	if !e.IsPathAllowed(destPath) {
		return fmt.Errorf("access denied: destination path '%s' is not in allowed paths", destPath)
	}

	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("source does not exist: %s", sourcePath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	// Check if destination already exists
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination already exists: %s", destPath)
	}

	// Execute pre-copy hook
	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      HookPreCopy,
		ToolName:   "copy_file",
		FilePath:   sourcePath,
		Operation:  "copy",
		SourcePath: sourcePath,
		DestPath:   destPath,
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
		Metadata:   map[string]interface{}{"is_dir": sourceInfo.IsDir()},
	}
	if _, err := e.hookManager.ExecuteHooks(ctx, HookPreCopy, hookCtx); err != nil {
		return fmt.Errorf("pre-copy hook denied operation: %w", err)
	}

	// Copy based on type
	var copyErr error
	if sourceInfo.IsDir() {
		copyErr = e.copyDirectory(sourcePath, destPath)
	} else {
		copyErr = e.copyFile(sourcePath, destPath)
	}
	if copyErr != nil {
		return copyErr
	}

	// Execute post-copy hook (best-effort)
	hookCtx.Event = HookPostCopy
	_, _ = e.hookManager.ExecuteHooks(ctx, HookPostCopy, hookCtx)

	return nil
}

// copyFile copies a single file using io.Copy for memory efficiency
func (e *UltraFastEngine) copyFile(src, dst string) error {
	// Get source file permissions
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Open source file for reading
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Ensure destination directory exists
	destDir := filepath.Dir(dst)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create destination file with same permissions
	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Use io.CopyBuffer with pooled buffer for efficient, memory-constant copy
	// This leverages OS optimizations like sendfile on Linux/WSL and reduces GC pressure
	bufPtr := e.bufferPool.Get().(*[]byte)
	defer e.bufferPool.Put(bufPtr)

	if _, err := io.CopyBuffer(dstFile, srcFile, *bufPtr); err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	// Invalidate cache for destination
	e.cache.InvalidateFile(dst)
	e.cache.InvalidateDirectory(filepath.Dir(dst))

	return nil
}

// copyDirectory recursively copies a directory
func (e *UltraFastEngine) copyDirectory(src, dst string) error {
	// Get source directory info
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source directory: %w", err)
	}

	// Create destination directory
	if err := os.MkdirAll(dst, sourceInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Read directory contents
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	// Copy each entry, skipping symlinks to prevent sandbox escape and infinite loops
	for _, entry := range entries {
		sourcePath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dst, entry.Name())

		// Skip symlinks to prevent following links outside allowed paths
		// and to avoid infinite loops from circular symlinks
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := e.copyDirectory(sourcePath, destPath); err != nil {
				return err
			}
		} else {
			// Copy file
			if err := e.copyFile(sourcePath, destPath); err != nil {
				return err
			}
		}
	}

	// Invalidate cache for destination
	e.cache.InvalidateDirectory(dst)

	return nil
}

// ReadFileRange reads a specific range of lines from a file
func (e *UltraFastEngine) ReadFileRange(ctx context.Context, path string, startLine, endLine int) (string, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "read_range"); err != nil {
		return "", err
	}

	start := time.Now()
	defer e.releaseOperation("read_range", start)

	// Validate path (security + access control)
	if !e.IsPathAllowed(path) {
		return "", fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
	}

	// Check if file exists
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", path)
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file: %s", path)
	}

	// Validate line numbers
	if startLine < 1 {
		return "", fmt.Errorf("start_line must be >= 1, got %d", startLine)
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_line (%d) must be >= start_line (%d)", endLine, startLine)
	}

	// Open file for efficient line-by-line reading
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var result strings.Builder
	scanner := bufio.NewScanner(file)

	// Use a larger buffer for better performance with long lines
	const maxCapacity = 1024 * 1024 // 1MB buffer for very long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	lineNum := 0
	foundLines := 0

	// Read file line by line.
	//
	// IMPORTANT: we keep iterating past endLine WITHOUT collecting content,
	// so the footer can report the REAL total line count of the file.
	//
	// Earlier versions broke out of this loop as soon as lineNum > endLine,
	// which left totalLines = endLine + 1 and produced a footer like
	//   "[Lines 15-50 of 51 total lines in foo.go]"
	// for a file that actually had 685 lines. Both humans and AI agents
	// reading that footer concluded the file had been truncated to 51 lines
	// — a silent reporting bug that mimicked catastrophic data loss.
	//
	// The cost of finishing the scan without buffering is negligible
	// (just newline detection per line), and it eliminates the lying footer.
	for scanner.Scan() {
		lineNum++

		// Before the requested range: skip cheaply.
		if lineNum < startLine {
			continue
		}

		// Inside the requested range: collect the line.
		if lineNum <= endLine {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(scanner.Text())
			foundLines++
		}
		// After the range: keep iterating to get the real totalLines, but
		// do not call scanner.Text() (avoids buffer copies for unused lines).
	}
	totalLines := lineNum

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}

	// If endLine was beyond the actual file length, clamp for display.
	actualEndLine := endLine
	if actualEndLine > totalLines {
		actualEndLine = totalLines
	}

	// Footer: report the REAL total. When the response does not include
	// the tail of the file, append a concrete hint for how to read more,
	// matching the format used by autoTruncateLargeFile so consumers see
	// a single consistent shape across read paths.
	footer := fmt.Sprintf("\n\n[Lines %d-%d of %d total lines in %s",
		startLine, actualEndLine, totalLines, filepath.Base(path))
	if actualEndLine < totalLines {
		rangeSize := endLine - startLine
		if rangeSize < 0 {
			rangeSize = 0
		}
		nextStart := actualEndLine + 1
		nextEnd := nextStart + rangeSize
		if nextEnd > totalLines {
			nextEnd = totalLines
		}
		footer += fmt.Sprintf(" \u2014 use start_line/end_line to read more, e.g. start_line=%d end_line=%d",
			nextStart, nextEnd)
	}
	footer += "]"
	result.WriteString(footer)

	return result.String(), nil
}

// GetFileInfo returns detailed information about a file or directory
func (e *UltraFastEngine) GetFileInfo(ctx context.Context, path string) (string, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "fileinfo"); err != nil {
		return "", err
	}

	start := time.Now()
	defer e.releaseOperation("fileinfo", start)

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return "", fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
	}

	// Get file info
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file or directory does not exist: %s", path)
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	// Build detailed info string
	var result strings.Builder

	if e.config.CompactMode {
		// Compact mode: minimal info
		fileType := "file"
		if info.IsDir() {
			fileType = "dir"
		}
		result.WriteString(fmt.Sprintf("%s: %s | %s | %s\n",
			fileType,
			info.Name(),
			formatSize(info.Size()),
			info.ModTime().Format("2006-01-02 15:04:05")))
	} else {
		// Verbose mode: detailed info
		result.WriteString(fmt.Sprintf("📄 File Information\n"))
		result.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"))
		result.WriteString(fmt.Sprintf("📁 Name: %s\n", info.Name()))
		result.WriteString(fmt.Sprintf("📍 Full Path: %s\n", path))

		if info.IsDir() {
			result.WriteString(fmt.Sprintf("📂 Type: Directory\n"))

			// Count items in directory if it's a directory
			entries, err := os.ReadDir(path)
			if err == nil {
				fileCount := 0
				dirCount := 0
				for _, entry := range entries {
					if entry.IsDir() {
						dirCount++
					} else {
						fileCount++
					}
				}
				result.WriteString(fmt.Sprintf("📊 Contents: %d files, %d directories\n", fileCount, dirCount))
			}
		} else {
			result.WriteString(fmt.Sprintf("📄 Type: File\n"))
			result.WriteString(fmt.Sprintf("💾 Size: %s (%d bytes)\n", formatSize(info.Size()), info.Size()))
		}

		result.WriteString(fmt.Sprintf("🔐 Permissions: %s\n", info.Mode().String()))
		result.WriteString(fmt.Sprintf("🕐 Modified: %s\n", info.ModTime().Format("2006-01-02 15:04:05")))

		// Get absolute path
		absPath, err := filepath.Abs(path)
		if err == nil && absPath != path {
			result.WriteString(fmt.Sprintf("🔗 Absolute Path: %s\n", absPath))
		}

		result.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"))
	}

	return result.String(), nil
}
