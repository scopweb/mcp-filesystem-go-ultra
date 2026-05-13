// Package ripgrep provides embedded ripgrep binaries for high-performance search.
// Use build tag `embed_rg` to include binaries in the build.
//
//go:build embed_rg
// +build embed_rg

package ripgrep

import (
	"fmt"
	"runtime"
)

const (
	// Version of embedded ripgrep
	Version = "15.1.0"
)

// EmbeddedBin returns the path to the embedded ripgrep binary for the current platform.
// Returns empty string if no binary is available for this platform.
func EmbeddedBin() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch {
	case goos == "windows" && goarch == "amd64":
		return "<cwd>/embed/ripgrep/rg-windows-amd64.exe"
	case goos == "linux" && goarch == "amd64":
		return "<cwd>/embed/ripgrep/rg-linux-amd64"
	case goos == "linux" && goarch == "arm64":
		return "<cwd>/embed/ripgrep/rg-linux-arm64"
	case goos == "darwin" && goarch == "amd64":
		return "<cwd>/embed/ripgrep/rg-darwin-amd64"
	case goos == "darwin" && goarch == "arm64":
		return "<cwd>/embed/ripgrep/rg-darwin-arm64"
	default:
		return ""
	}
}

// IsEmbedded returns true if ripgrep binaries are embedded in this build.
func IsEmbedded() bool {
	return EmbeddedBin() != ""
}

// PlatformInfo returns a string describing the embedded binary platform.
func PlatformInfo() string {
	bin := EmbeddedBin()
	if bin == "" {
		return fmt.Sprintf("no embedded ripgrep for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("embedded ripgrep %s for %s/%s", Version, runtime.GOOS, runtime.GOARCH)
}