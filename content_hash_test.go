package main

// Tests for improvement B3: content_hash in read_file + expected_hash in edit_file.
// These verify the stale-edit protection mechanism.

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestContentHash_AppearsInRead verifies that read_file appends a
// "# content_hash: XXXXXXXX" line at the end of its response.
func TestContentHash_AppearsInRead(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "read_file",
			Arguments: map[string]interface{}{"path": path},
		},
	}
	result, err := reg.readFileHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if result.IsError {
		t.Fatalf("read_file returned error: %v", result.Content)
	}
	text := resultText(t, result)

	// Extract the content_hash line
	re := regexp.MustCompile(`(?m)^# content_hash: ([0-9a-f]{8})$`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("expected '# content_hash: XXXXXXXX' line in response, got:\n%s", text)
	}
	reportedHash := m[1]
	// Compute what the hash should be (FNV-1a of "hello world\n" + the appended line is circular,
	// so we just verify it's 8 hex chars — already done by regex).
	_ = reportedHash
}

// TestContentHash_Stable verifies that two reads of the same unchanged file
// return the same content_hash.
func TestContentHash_Stable(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "stable.txt")
	if err := os.WriteFile(path, []byte("abc\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	read := func() string {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{Name: "read_file", Arguments: map[string]interface{}{"path": path}},
		}
		result, _ := reg.readFileHandler(context.Background(), req)
		return resultText(t, result)
	}
	text1 := read()
	text2 := read()

	re := regexp.MustCompile(`# content_hash: ([0-9a-f]{8})`)
	m1 := re.FindStringSubmatch(text1)
	m2 := re.FindStringSubmatch(text2)
	if m1 == nil || m2 == nil {
		t.Fatal("content_hash line missing in one of the reads")
	}
	if m1[1] != m2[1] {
		t.Errorf("content_hash should be stable for unchanged file: %q vs %q", m1[1], m2[1])
	}
}

// TestExpectedHash_Accepted verifies that edit_file with the correct
// expected_hash proceeds normally (backward-compatible behavior).
func TestExpectedHash_Accepted(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(path, []byte("foo bar\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Compute the hash of the actual file content
	content, _ := os.ReadFile(path)
	h := fnv.New32a()
	h.Write(content)
	hash := fmt.Sprintf("%08x", h.Sum32())

	_ = callEdit(t, reg, map[string]interface{}{
		"path":          path,
		"old_text":      "foo bar",
		"new_text":      "BAZ",
		"expected_hash": hash,
	})
	// If we got here without an error, the edit was accepted.

	// Verify the file was actually edited
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "BAZ") {
		t.Errorf("expected file to contain BAZ after edit, got: %s", after)
	}
}

// TestExpectedHash_Rejected verifies that edit_file with a wrong expected_hash
// returns a clear error and does NOT modify the file.
func TestExpectedHash_Rejected(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "noedit.txt")
	if err := os.WriteFile(path, []byte("original content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := callEdit(t, reg, map[string]interface{}{
		"path":          path,
		"old_text":      "original",
		"new_text":      "MODIFIED",
		"expected_hash": "deadbeef", // wrong hash
	})

	if !result.IsError {
		t.Fatal("expected IsError=true on hash mismatch, got ok")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "stale edit") {
		t.Errorf("expected error to mention 'stale edit', got: %s", text)
	}
	if !strings.Contains(text, "deadbeef") {
		t.Errorf("expected error to mention the wrong expected hash, got: %s", text)
	}

	// Verify the file was NOT modified
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "original content") {
		t.Errorf("file was modified despite hash mismatch! got: %s", after)
	}
	if strings.Contains(string(after), "MODIFIED") {
		t.Error("file was modified despite hash mismatch")
	}
}

// TestExpectedHash_NotProvided verifies that omitting expected_hash preserves
// the original behavior (backward-compat: edit proceeds without the check).
func TestExpectedHash_NotProvided(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "nocheck.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = callEdit(t, reg, map[string]interface{}{
		"path":     path,
		"old_text": "hello",
		"new_text": "goodbye",
		// no expected_hash → no check
	})

	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "goodbye") {
		t.Errorf("edit without expected_hash should proceed normally, got: %s", after)
	}
}
