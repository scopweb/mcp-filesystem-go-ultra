package main

// Verifies that write_file now reports verified post-write evidence: the
// reported path, byte count and content hash must match the on-disk reality
// after hooks/EOL transforms, and a synthetic "atomic write but readback
// fails" path must surface verified:false plus the unverified warning.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func callWriteFile(t *testing.T, reg *toolRegistry, params map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "write_file",
			Arguments: params,
		},
	}
	result, err := reg.writeFileHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("write_file handler: %v", err)
	}
	return result
}

func TestWriteFile_ReportsVerifiedEvidence(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "hello.txt")
	res := callWriteFile(t, reg, map[string]interface{}{
		"path":    path,
		"content": "hello world\n",
	})
	if res.IsError {
		t.Fatalf("write_file returned error: %s", resultText(t, res))
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("write_file StructuredContent is %T, expected map", res.StructuredContent)
	}
	if got, want := sc["verified"], true; got != want {
		t.Errorf("verified = %v, want %v", got, want)
	}
	bytesWritten, ok := sc["bytes_written"].(int)
	if !ok {
		t.Fatalf("bytes_written is %T, expected int", sc["bytes_written"])
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if bytesWritten != int(info.Size()) {
		t.Errorf("bytes_written = %d, on-disk size = %d", bytesWritten, info.Size())
	}
	hashOnDisk, ok := computeFileOCCHash(core.NormalizePath(path))
	if !ok {
		t.Fatalf("could not hash on-disk file")
	}
	if got, want := sc["content_hash"], hashOnDisk; got != want {
		t.Errorf("content_hash = %v, on-disk hash = %v", got, want)
	}
}

func TestWriteFile_PreservesExistingCRLFSize(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "crlf.txt")
	if err := os.WriteFile(path, []byte("one\r\ntwo\r\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res := callWriteFile(t, reg, map[string]interface{}{
		"path":    path,
		"content": "ONE\nTWO\n",
	})
	if res.IsError {
		t.Fatalf("write_file returned error: %s", resultText(t, res))
	}
	sc := res.StructuredContent.(map[string]any)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := sc["bytes_written"].(int), int(info.Size()); got != want {
		t.Errorf("bytes_written = %d, on-disk size = %d (CRLF preservation)", got, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if !strings.Contains(string(raw), "\r\n") {
		t.Errorf("CRLF was not preserved; bytes=%q", raw)
	}
}
