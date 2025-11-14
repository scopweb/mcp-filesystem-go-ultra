package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DetectEnvironment detects if running in WSL and returns Windows username if applicable
func DetectEnvironment() (isWSL bool, windowsUser string) {
	// Check if running on Linux (WSL runs on Linux kernel)
	if runtime.GOOS != "linux" {
		return false, ""
	}

	// Check for WSL-specific files/env variables
	// 1. Check /proc/version for "Microsoft" or "WSL"
	if data, err := os.ReadFile("/proc/version"); err == nil {
		version := strings.ToLower(string(data))
		if strings.Contains(version, "microsoft") || strings.Contains(version, "wsl") {
			// Try to get Windows username
			winUser := getWindowsUsername()
			return true, winUser
		}
	}

	// 2. Check WSL_DISTRO_NAME environment variable (WSL 2)
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		winUser := getWindowsUsername()
		return true, winUser
	}

	// 3. Check for /mnt/c existence (common WSL mount point)
	if _, err := os.Stat("/mnt/c"); err == nil {
		winUser := getWindowsUsername()
		return true, winUser
	}

	return false, ""
}

// getWindowsUsername attempts to retrieve the Windows username from WSL
func getWindowsUsername() string {
	// Try to get from Windows environment via cmd.exe
	if cmd := exec.Command("cmd.exe", "/c", "echo", "%USERNAME%"); cmd != nil {
		if output, err := cmd.Output(); err == nil {
			username := strings.TrimSpace(string(output))
			// Remove %USERNAME% in case the variable doesn't expand
			if username != "" && username != "%USERNAME%" {
				return username
			}
		}
	}

	// Try to get from WSL environment variable
	if wslUser := os.Getenv("WSLUSER"); wslUser != "" {
		return wslUser
	}

	// Try to extract from /mnt/c/Users directory
	if entries, err := os.ReadDir("/mnt/c/Users"); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			// Skip system directories
			if name != "Public" && name != "Default" && name != "All Users" &&
			   name != "Default User" && !strings.HasPrefix(name, ".") {
				// Return first valid-looking username
				if entry.IsDir() {
					return name
				}
			}
		}
	}

	return ""
}

// IsWSLPath checks if the given path is a WSL-style path
func IsWSLPath(path string) bool {
	if path == "" {
		return false
	}

	// WSL paths typically start with / (absolute Unix paths)
	// but not /mnt/<drive> which are Windows paths accessed from WSL
	if strings.HasPrefix(path, "/mnt/") && len(path) > 6 {
		parts := strings.SplitN(path[5:], "/", 2)
		if len(parts) >= 1 && len(parts[0]) == 1 {
			// This is a Windows path accessed from WSL (/mnt/c/...)
			return false
		}
	}

	// True WSL paths: /home/..., /tmp/..., /usr/..., etc.
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "/mnt/")
}

// IsWindowsPath checks if the given path is a Windows-style path
func IsWindowsPath(path string) bool {
	if len(path) < 3 {
		return false
	}

	// Windows absolute paths: C:\... or C:/...
	if path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}

	// Windows UNC paths: \\server\share
	if strings.HasPrefix(path, "\\\\") {
		return true
	}

	// WSL-style Windows mount: /mnt/c/...
	if strings.HasPrefix(path, "/mnt/") && len(path) > 6 {
		parts := strings.SplitN(path[5:], "/", 2)
		if len(parts) >= 1 && len(parts[0]) == 1 {
			return true
		}
	}

	return false
}

// GetWSLHome returns the WSL home directory
func GetWSLHome() string {
	// First try HOME environment variable
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(home, "/") {
		return home
	}

	// Try to get from /etc/passwd
	if username := os.Getenv("USER"); username != "" {
		possibleHome := "/home/" + username
		if _, err := os.Stat(possibleHome); err == nil {
			return possibleHome
		}
	}

	// Default fallback
	return "/home/user"
}

// GetWindowsHome returns the Windows home directory
// Returns both WSL-style (/mnt/c/Users/...) and Windows-style (C:\Users\...) paths
func GetWindowsHome() (wslStyle string, windowsStyle string) {
	isWSL, winUser := DetectEnvironment()

	if !isWSL {
		// Not in WSL, try to get Windows home if running on Windows
		if runtime.GOOS == "windows" {
			if home := os.Getenv("USERPROFILE"); home != "" {
				return "", home
			}
			if homeDrive := os.Getenv("HOMEDRIVE"); homeDrive != "" {
				if homePath := os.Getenv("HOMEPATH"); homePath != "" {
					windowsStyle = filepath.Join(homeDrive, homePath)
					return "", windowsStyle
				}
			}
		}
		return "", ""
	}

	// In WSL - construct Windows home paths
	if winUser == "" {
		winUser = "user" // fallback
	}

	wslStyle = "/mnt/c/Users/" + winUser
	windowsStyle = "C:\\Users\\" + winUser

	// Verify the WSL-style path exists
	if _, err := os.Stat(wslStyle); err != nil {
		// Try other common drive letters
		for _, drive := range []string{"d", "e"} {
			altPath := "/mnt/" + drive + "/Users/" + winUser
			if _, err := os.Stat(altPath); err == nil {
				driveLetter := strings.ToUpper(drive)
				wslStyle = altPath
				windowsStyle = driveLetter + ":\\Users\\" + winUser
				break
			}
		}
	}

	return wslStyle, windowsStyle
}

// GetWindowsUserDocuments returns the Windows Documents folder path
func GetWindowsUserDocuments() (wslStyle string, windowsStyle string) {
	wslHome, winHome := GetWindowsHome()
	if wslHome != "" {
		wslStyle = filepath.Join(wslHome, "Documents")
	}
	if winHome != "" {
		windowsStyle = filepath.Join(winHome, "Documents")
	}
	return wslStyle, windowsStyle
}
