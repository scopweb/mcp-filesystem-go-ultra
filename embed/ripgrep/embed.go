// Package ripgrep provides embedded ripgrep binaries for high-performance search.
// Use build tag `embed_rg` to include binaries in the build.
//
//go:build embed_rg
// +build embed_rg

package ripgrep

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//go:embed *.exe
var binaries embed.FS

var (
	extractedPath string
	extractOnce  sync.Once
	extractErr   error
)

// GetExtractedPath returns the path to the extracted ripgrep binary.
// It extracts the binary from the embedded filesystem on first call.
func GetExtractedPath() (string, error) {
	extractOnce.Do(func() {
		extractedPath, extractErr = extractBinary()
	})
	return extractedPath, extractErr
}

func extractBinary() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	var filename string
	switch {
	case goos == "windows" && goarch == "amd64":
		filename = "rg-windows-amd64.exe"
	case goos == "linux" && goarch == "amd64":
		filename = "rg-linux-amd64"
	case goos == "linux" && goarch == "arm64":
		filename = "rg-linux-arm64"
	case goos == "darwin" && goarch == "amd64":
		filename = "rg-darwin-amd64"
	case goos == "darwin" && goarch == "arm64":
		filename = "rg-darwin-arm64"
	default:
		return "", fmt.Errorf("no embedded ripgrep for %s/%s", goos, goarch)
	}

	// Read embedded binary
	data, err := binaries.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to read embedded binary %s: %w", filename, err)
	}

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "filesystem-ultra-rg-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Extract binary
	destPath := filepath.Join(tempDir, filename)
	if goos == "windows" {
		destPath = filepath.Join(tempDir, "rg.exe")
	}

	out, err := os.Create(destPath)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, bytes.NewReader(data)); err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to write binary: %w", err)
	}

	// Make executable on Unix
	if goos != "windows" {
		os.Chmod(destPath, 0755)
	}

	return destPath, nil
}

// Cleanup removes the extracted binary from temp directory.
// Call this on shutdown if needed.
func Cleanup() {
	if extractedPath != "" {
		dir := filepath.Dir(extractedPath)
		os.RemoveAll(dir)
	}
}

// Version of embedded ripgrep
const Version = "15.1.0"

// IsEmbedded returns true when embed_rg build tag is set.
func IsEmbedded() bool {
	return true
}
