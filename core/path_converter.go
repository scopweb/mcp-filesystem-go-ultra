package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WSLToWindows converts a WSL path to Windows path
// Example: /home/user/file.txt -> C:\Users\user\file.txt
// Example: /tmp/test.txt -> C:\Users\user\AppData\Local\Temp\test.txt
// Example: /mnt/c/Projects/file.go -> C:\Projects\file.go (already Windows)
func WSLToWindows(wslPath string) (string, error) {
	if wslPath == "" {
		return "", fmt.Errorf("empty path provided")
	}

	// If it's already a Windows path, return as-is
	if IsWindowsPath(wslPath) {
		// If it's a /mnt/c/ style path, convert it
		if strings.HasPrefix(wslPath, "/mnt/") && len(wslPath) > 6 {
			parts := strings.SplitN(wslPath[5:], "/", 2)
			if len(parts) >= 1 && len(parts[0]) == 1 {
				driveLetter := strings.ToUpper(parts[0])
				remainder := ""
				if len(parts) > 1 {
					remainder = parts[1]
					// Convert forward slashes to backslashes
					remainder = filepath.FromSlash(remainder)
				}
				return driveLetter + ":\\" + remainder, nil
			}
		}
		// Already in C:\ format
		return wslPath, nil
	}

	// Not a Windows path, need to convert WSL path
	if !strings.HasPrefix(wslPath, "/") {
		return "", fmt.Errorf("path must be absolute (start with /): %s", wslPath)
	}

	_, winHome := GetWindowsHome()
	if winHome == "" {
		return "", fmt.Errorf("could not determine Windows home directory")
	}

	// Handle common WSL directories
	switch {
	case strings.HasPrefix(wslPath, "/home/"):
		// /home/user/... -> C:\Users\user\...
		// Extract username and rest of path
		parts := strings.SplitN(wslPath[6:], "/", 2)
		if len(parts) >= 1 {
			username := parts[0]
			remainder := ""
			if len(parts) > 1 {
				remainder = filepath.FromSlash(parts[1])
			}
			return fmt.Sprintf("C:\\Users\\%s\\%s", username, remainder), nil
		}

	case strings.HasPrefix(wslPath, "/tmp/"):
		// /tmp/... -> C:\Users\user\AppData\Local\Temp\...
		remainder := filepath.FromSlash(wslPath[5:])
		return filepath.Join(winHome, "AppData", "Local", "Temp", remainder), nil

	case strings.HasPrefix(wslPath, "/usr/"):
		// /usr/... -> C:\Users\user\.wsl\usr\...
		remainder := filepath.FromSlash(wslPath[5:])
		return filepath.Join(winHome, ".wsl", "usr", remainder), nil

	case strings.HasPrefix(wslPath, "/etc/"):
		// /etc/... -> C:\Users\user\.wsl\etc\...
		remainder := filepath.FromSlash(wslPath[5:])
		return filepath.Join(winHome, ".wsl", "etc", remainder), nil

	case strings.HasPrefix(wslPath, "/var/"):
		// /var/... -> C:\Users\user\.wsl\var\...
		remainder := filepath.FromSlash(wslPath[5:])
		return filepath.Join(winHome, ".wsl", "var", remainder), nil

	case strings.HasPrefix(wslPath, "/opt/"):
		// /opt/... -> C:\Users\user\.wsl\opt\...
		remainder := filepath.FromSlash(wslPath[5:])
		return filepath.Join(winHome, ".wsl", "opt", remainder), nil

	default:
		// For other paths, use a .wsl prefix in Windows home
		remainder := filepath.FromSlash(wslPath[1:])
		return filepath.Join(winHome, ".wsl", remainder), nil
	}

	return "", fmt.Errorf("could not convert WSL path: %s", wslPath)
}

// WindowsToWSL converts a Windows path to WSL path
// Example: C:\Users\user\file.txt -> /home/user/file.txt
// Example: C:\Projects\test.go -> /mnt/c/Projects/test.go
// Example: /home/user/file.txt -> /home/user/file.txt (already WSL)
func WindowsToWSL(winPath string) (string, error) {
	if winPath == "" {
		return "", fmt.Errorf("empty path provided")
	}

	// If it's already a WSL path, return as-is
	if IsWSLPath(winPath) {
		return winPath, nil
	}

	// Handle /mnt/ style paths (already in WSL format but for Windows drives)
	if strings.HasPrefix(winPath, "/mnt/") {
		return winPath, nil
	}

	// Handle Windows absolute paths: C:\... or C:/...
	if len(winPath) >= 3 && winPath[1] == ':' && (winPath[2] == '\\' || winPath[2] == '/') {
		driveLetter := strings.ToLower(string(winPath[0]))
		remainder := winPath[3:]
		// Convert backslashes to forward slashes
		remainder = filepath.ToSlash(remainder)

		// Check if this is a Users directory path
		if strings.HasPrefix(strings.ToLower(remainder), "users/") {
			parts := strings.SplitN(remainder[6:], "/", 2)
			if len(parts) >= 1 {
				username := parts[0]
				rest := ""
				if len(parts) > 1 {
					rest = "/" + parts[1]
				}

				// Map common Windows user directories to WSL equivalents
				rest = strings.ToLower(rest)
				switch {
				case strings.HasPrefix(rest, "/appdata/local/temp"):
					// C:\Users\user\AppData\Local\Temp\... -> /tmp/...
					suffix := rest[19:]
					return "/tmp" + suffix, nil

				case strings.Contains(rest, "/.wsl/"):
					// C:\Users\user\.wsl\usr\... -> /usr/...
					idx := strings.Index(rest, "/.wsl/")
					suffix := rest[idx+5:]
					return suffix, nil

				default:
					// C:\Users\user\... -> /home/user/...
					return "/home/" + username + rest, nil
				}
			}
		}

		// For non-Users paths, use /mnt/ style
		return "/mnt/" + driveLetter + "/" + remainder, nil
	}

	// Handle UNC paths: \\server\share -> /mnt/server/share
	if strings.HasPrefix(winPath, "\\\\") {
		remainder := filepath.ToSlash(winPath[2:])
		return "/mnt/" + remainder, nil
	}

	return "", fmt.Errorf("could not convert Windows path: %s", winPath)
}

// ConvertPath automatically detects the path type and converts it
// Returns the converted path and a boolean indicating if it was WSL->Windows (true) or Windows->WSL (false)
func ConvertPath(path string) (converted string, wasWSL bool, err error) {
	if IsWSLPath(path) {
		converted, err := WSLToWindows(path)
		return converted, true, err
	}

	if IsWindowsPath(path) {
		converted, err := WindowsToWSL(path)
		return converted, false, err
	}

	return "", false, fmt.Errorf("could not determine path type: %s", path)
}

// NormalizePathAdvanced is an enhanced version that returns both normalized path and metadata
func NormalizePathAdvanced(path string) (normalized string, isWSL bool, isWindows bool, err error) {
	if path == "" {
		return "", false, false, fmt.Errorf("empty path provided")
	}

	isWSL = IsWSLPath(path)
	isWindows = IsWindowsPath(path)

	// Use the existing NormalizePath function for basic normalization
	normalized = NormalizePath(path)

	return normalized, isWSL, isWindows, nil
}

// EnsurePathAccessible ensures a path is accessible in the current environment
// If running on Windows and given a WSL path, converts to Windows
// If running on WSL and given a Windows path, converts to WSL
func EnsurePathAccessible(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path provided")
	}

	// Detect current environment
	isWSLEnv, _ := DetectEnvironment()
	isWindowsEnv := !isWSLEnv && os.PathSeparator == '\\'

	pathIsWSL := IsWSLPath(path)
	pathIsWindows := IsWindowsPath(path)

	// If running on Windows and path is WSL-style
	if isWindowsEnv && pathIsWSL {
		return WSLToWindows(path)
	}

	// If running on WSL and path is Windows-style
	if isWSLEnv && pathIsWindows {
		return WindowsToWSL(path)
	}

	// Path is already in the correct format
	return filepath.Clean(path), nil
}

// CopyFileWithConversion copies a file from source to destination, handling path conversion
func CopyFileWithConversion(srcPath, dstPath string, createDirs bool) error {
	// Normalize paths
	srcPath = NormalizePath(srcPath)
	dstPath = NormalizePath(dstPath)

	// Check if source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("source file does not exist: %s", srcPath)
	}

	// Create destination directory if requested
	if createDirs {
		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %v", err)
		}
	}

	// Read source file
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source file: %v", err)
	}

	// Write to destination
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write destination file: %v", err)
	}

	return nil
}
