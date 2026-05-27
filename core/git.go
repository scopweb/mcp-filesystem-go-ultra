package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// FindGitRoot walks up the directory tree from path until a .git directory is found.
// Returns the repository root path.
func FindGitRoot(path string) (string, error) {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
		path = cwd
	}

	// If path is a file, start from its directory
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}

	dir := path
	for {
		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return dir, nil
		}

		// Windows: .git might be a file (gitdir: /path/to/actual/.git)
		// This happens with bare repos and git worktrees on Windows
		if runtime.GOOS == "windows" {
			if data, err := os.ReadFile(gitDir); err == nil {
				content := strings.TrimSpace(string(data))
				if strings.HasPrefix(content, "gitdir: ") {
					// Extract actual .git dir path
					actualGitDir := strings.TrimPrefix(content, "gitdir: ")
					// The content points to the actual .git directory
					// We need to return the parent of that .git directory
					actualGitDir = strings.TrimSuffix(actualGitDir, "/.git")
					actualGitDir = strings.TrimSuffix(actualGitDir, "\\.git")
					return actualGitDir, nil
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a git repository: %s", path)
		}
		dir = parent
	}
}

// IsGitRepo returns true if path is inside a git repository
func IsGitRepo(path string) bool {
	_, err := FindGitRoot(path)
	return err == nil
}
