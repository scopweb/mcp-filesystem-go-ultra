package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
