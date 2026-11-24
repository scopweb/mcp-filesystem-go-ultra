package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The rotation must keep the number of files the operator asked for: the
// weekly measurement only sees what the log still holds.
func TestProxyLoggerKeepsRotatedFiles(t *testing.T) {
	dir := t.TempDir()
	logger, err := newProxyLogger(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	for i := 0; i < 8; i++ {
		logger.Log(ProxyLogEntry{Tool: "read_file"})
		// Force a rotation on every write.
		logger.written = 10 * 1024 * 1024
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "proxy-*.jsonl"))
	if len(matches) > 5 {
		t.Fatalf("kept %d rotated files, want at most 5", len(matches))
	}
}

// A logger built without an explicit keep must not be destructive.
func TestProxyLoggerDefaultKeep(t *testing.T) {
	dir := t.TempDir()
	logger, err := newProxyLogger(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	if logger.keep != 10 {
		t.Fatalf("default keep = %d, want 10", logger.keep)
	}
	// The live log must exist and be writable.
	if _, err := os.Stat(logger.logPath); err != nil {
		t.Fatalf("live log missing: %v", err)
	}
}
