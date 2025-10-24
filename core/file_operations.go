package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RenameFile renames a file or directory
func (e *UltraFastEngine) RenameFile(ctx context.Context, oldPath, newPath string) error {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "rename"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("rename", start)

	// Check if both paths are allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(oldPath) {
			return fmt.Errorf("access denied: source path '%s' is not in allowed paths", oldPath)
		}
		if !e.isPathAllowed(newPath) {
			return fmt.Errorf("access denied: destination path '%s' is not in allowed paths", newPath)
		}
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
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Perform the rename
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename file: %v", err)
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
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "softdelete"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("softdelete", start)

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
		}
	}

	// Check if source exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", path)
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
		return fmt.Errorf("failed to create filesdelete directory: %v", err)
	}

	// Create destination path maintaining relative structure
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %v", err)
	}

	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute root directory: %v", err)
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
		return fmt.Errorf("failed to create destination directory in filesdelete: %v", err)
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
		return fmt.Errorf("failed to move file to filesdelete: %v", err)
	}

	// Invalidate cache entries
	e.cache.InvalidateFile(path)
	e.cache.InvalidateDirectory(filepath.Dir(path))

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
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "createdir"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("createdir", start)

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
		}
	}

	// Check if directory already exists
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("directory already exists: %s", path)
		}
		return fmt.Errorf("a file with that name already exists: %s", path)
	}

	// Create directory with all parent directories
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Invalidate parent directory cache
	e.cache.InvalidateDirectory(filepath.Dir(path))

	return nil
}

// DeleteFile permanently deletes a file or directory
func (e *UltraFastEngine) DeleteFile(ctx context.Context, path string) error {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "delete"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("delete", start)

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
		}
	}

	// Check if file/directory exists
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("file or directory does not exist: %s", path)
	}
	if err != nil {
		return fmt.Errorf("failed to stat file: %v", err)
	}

	// Delete file or directory recursively
	if info.IsDir() {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}

	if err != nil {
		return fmt.Errorf("failed to delete: %v", err)
	}

	// Invalidate cache entries
	e.cache.InvalidateFile(path)
	e.cache.InvalidateDirectory(path)
	e.cache.InvalidateDirectory(filepath.Dir(path))

	return nil
}

// MoveFile moves a file or directory to a new location
func (e *UltraFastEngine) MoveFile(ctx context.Context, sourcePath, destPath string) error {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "move"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("move", start)

	// Check if both paths are allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(sourcePath) {
			return fmt.Errorf("access denied: source path '%s' is not in allowed paths", sourcePath)
		}
		if !e.isPathAllowed(destPath) {
			return fmt.Errorf("access denied: destination path '%s' is not in allowed paths", destPath)
		}
	}

	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("source does not exist: %s", sourcePath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat source: %v", err)
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
			return fmt.Errorf("failed to create destination directory: %v", err)
		}
	}

	// Perform the move
	if err := os.Rename(sourcePath, destPath); err != nil {
		return fmt.Errorf("failed to move: %v", err)
	}

	// Invalidate cache entries
	e.cache.InvalidateFile(sourcePath)
	e.cache.InvalidateFile(destPath)
	e.cache.InvalidateDirectory(sourcePath)
	e.cache.InvalidateDirectory(destPath)
	e.cache.InvalidateDirectory(filepath.Dir(sourcePath))
	e.cache.InvalidateDirectory(filepath.Dir(destPath))

	return nil
}

// CopyFile copies a file or directory to a new location
func (e *UltraFastEngine) CopyFile(ctx context.Context, sourcePath, destPath string) error {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "copy"); err != nil {
		return err
	}

	start := time.Now()
	defer e.releaseOperation("copy", start)

	// Check if both paths are allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(sourcePath) {
			return fmt.Errorf("access denied: source path '%s' is not in allowed paths", sourcePath)
		}
		if !e.isPathAllowed(destPath) {
			return fmt.Errorf("access denied: destination path '%s' is not in allowed paths", destPath)
		}
	}

	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("source does not exist: %s", sourcePath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat source: %v", err)
	}

	// Check if destination already exists
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination already exists: %s", destPath)
	}

	// Copy based on type
	if sourceInfo.IsDir() {
		return e.copyDirectory(sourcePath, destPath)
	}
	return e.copyFile(sourcePath, destPath)
}

// copyFile copies a single file
func (e *UltraFastEngine) copyFile(src, dst string) error {
	// Read source file
	sourceData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source file: %v", err)
	}

	// Get source file permissions
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %v", err)
	}

	// Ensure destination directory exists
	destDir := filepath.Dir(dst)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Write to destination with same permissions
	if err := os.WriteFile(dst, sourceData, sourceInfo.Mode()); err != nil {
		return fmt.Errorf("failed to write destination file: %v", err)
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
		return fmt.Errorf("failed to stat source directory: %v", err)
	}

	// Create destination directory
	if err := os.MkdirAll(dst, sourceInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Read directory contents
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read directory: %v", err)
	}

	// Copy each entry
	for _, entry := range entries {
		sourcePath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dst, entry.Name())

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

// GetFileInfo returns detailed information about a file or directory
func (e *UltraFastEngine) GetFileInfo(ctx context.Context, path string) (string, error) {
	// Acquire semaphore
	if err := e.acquireOperation(ctx, "fileinfo"); err != nil {
		return "", err
	}

	start := time.Now()
	defer e.releaseOperation("fileinfo", start)

	// Check if path is allowed
	if len(e.config.AllowedPaths) > 0 {
		if !e.isPathAllowed(path) {
			return "", fmt.Errorf("access denied: path '%s' is not in allowed paths", path)
		}
	}

	// Get file info
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file or directory does not exist: %s", path)
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %v", err)
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
