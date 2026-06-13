package main

// Tests for B1 (read_file returns content_hash as a structured field) and the
// B3 expected_hash OCC mechanism in edit_file.
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

// TestContentHash_AppearsInRead verifies that read_file returns the
// content_hash as a structured response field (Bug B1 fix: it is no longer
// appended as a `# content_hash: XXXXXXXX` line to the file body, which
// was visually indistinguishable from legitimate Markdown content and
// caused consumers to anchor edits on it).
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

	// Bug B1 regression: the file body returned to the client must NOT
	// contain a `# content_hash:` line. The hash lives in StructuredContent.
	text := resultText(t, result)
	if strings.Contains(text, "# content_hash:") {
		t.Errorf("file body still contains '# content_hash:' trailer (B1 regression). The trailer must live in StructuredContent, not in the body text. Body:\n%s", text)
	}

	// The hash should be in StructuredContent under the "content_hash" key.
	if result.StructuredContent == nil {
		t.Fatal("read_file did not set StructuredContent (B1 fix expects the hash in a structured field)")
	}
	m, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T, expected map[string]any", result.StructuredContent)
	}
	hashRaw, present := m["content_hash"]
	if !present {
		t.Fatalf("StructuredContent is missing 'content_hash' key. Got: %#v", m)
	}
	hash, ok := hashRaw.(string)
	if !ok {
		t.Fatalf("content_hash is %T, expected string", hashRaw)
	}
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	if !re.MatchString(hash) {
		t.Errorf("content_hash %q is not 8 lowercase hex chars", hash)
	}
}

// TestContentHash_Stable verifies that two reads of the same unchanged file
// return the same content_hash (now read from StructuredContent).
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
		m, ok := result.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("StructuredContent is %T, expected map[string]any", result.StructuredContent)
		}
		hash, _ := m["content_hash"].(string)
		return hash
	}
	hash1 := read()
	hash2 := read()

	if hash1 == "" || hash2 == "" {
		t.Fatal("content_hash missing from StructuredContent in one of the reads")
	}
	if hash1 != hash2 {
		t.Errorf("content_hash should be stable for unchanged file: %q vs %q", hash1, hash2)
	}
}

// TestContentHash_RoundTripsWithExpectedHash verifies the OCC mechanism
// still works end-to-end after the B1 fix: read_file returns a hash in
// StructuredContent, the consumer extracts it, and edit_file accepts it
// as `expected_hash` to confirm the file has not been concurrently
// modified. This is the documented usage pattern; the regression test
// guards against any future change that breaks the round-trip.
func TestContentHash_RoundTripsWithExpectedHash(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "round_trip.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 1. read_file
	readReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "read_file",
			Arguments: map[string]interface{}{"path": path},
		},
	}
	readResult, err := reg.readFileHandler(context.Background(), readReq)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	m, ok := readResult.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("read_file StructuredContent is %T, expected map[string]any", readResult.StructuredContent)
	}
	hash, _ := m["content_hash"].(string)
	if hash == "" {
		t.Fatal("content_hash missing from StructuredContent")
	}

	// 2. edit_file with the extracted hash → must be accepted
	editResult := callEdit(t, reg, map[string]interface{}{
		"path":          path,
		"old_text":      "alpha",
		"new_text":      "ALPHA",
		"expected_hash": hash,
	})
	if editResult.IsError {
		t.Fatalf("edit_file with valid expected_hash should succeed, got error: %s", resultText(t, editResult))
	}

	// 3. Verify the file was modified
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "ALPHA") {
		t.Errorf("edit was rejected or did not apply: %s", after)
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
