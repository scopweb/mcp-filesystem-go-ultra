package main

// Regression tests for issue #24 — multi_edit missing OCC stale-read
// protection (Improvement B3 parity with edit_file).
//
// Before this fix, the `expected_hash` parameter accepted by `edit_file`
// was NOT declared on the `multi_edit` tool. A consumer editing a file
// that may be concurrently modified (external editor, another agent
// calling edit_file) could opt into stale-read protection for a single
// edit but not for a batch — even though a single hash check before
// the atomic loop would cover the whole batch.

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// setupMultiEditHashEngine creates a test engine for the multi_edit
// expected_hash regression tests (issue #24).
func setupMultiEditHashEngine(t *testing.T) (*core.UltraFastEngine, string) {
	t.Helper()
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}
	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	return engine, tempDir
}

// fnvHashFile returns the FNV-1a 8-hex-char hash of the file's content,
// matching the algorithm used by read_file (tools_core.go:267-269) and
// edit_file (tools_core.go:760-762). Helper used by the regression tests
// to compute the hash a consumer would have captured from a prior read.
func fnvHashFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	h := fnv.New32a()
	h.Write(content)
	return fmt.Sprintf("%08x", h.Sum32())
}

// TestIssue24_MultiEditExpectedHash_Accepted verifies that multi_edit
// with a valid `expected_hash` (matching the file's current FNV-1a)
// proceeds and applies the edits. This is the basic OCC acceptance path,
// mirroring edit_file's behavior (TestExpectedHash_Accepted).
func TestIssue24_MultiEditExpectedHash_Accepted(t *testing.T) {
	engine, tempDir := setupMultiEditHashEngine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "occ_accepted.txt")
	if err := os.WriteFile(filePath, []byte("foo bar\nbaz qux\n"), 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	// Compute the hash a consumer would have captured from a prior read.
	hash := fnvHashFile(t, filePath)

	edits := []core.MultiEditOperation{
		{OldText: "foo bar", NewText: "FOO BAR"},
		{OldText: "baz qux", NewText: "BAZ QUX"},
	}

	result, err := engine.MultiEdit(ctx, filePath, edits, false, false, false, hash)
	if err != nil {
		t.Fatalf("MultiEdit with valid expected_hash should succeed, got: %v", err)
	}
	if result.SuccessfulEdits != 2 {
		t.Errorf("expected 2 successful edits, got %d (errors=%v)",
			result.SuccessfulEdits, result.Errors)
	}

	// Verify the file was actually modified
	after, _ := os.ReadFile(filePath)
	if !strings.Contains(string(after), "FOO BAR") || !strings.Contains(string(after), "BAZ QUX") {
		t.Errorf("file was not modified: %s", after)
	}
}

// TestIssue24_MultiEditExpectedHash_Rejected_StaleEdit verifies that
// multi_edit with a stale `expected_hash` (file changed since the
// consumer's last read) is rejected with the EXACT same error string
// edit_file uses, and that the file is NOT modified — the whole batch
// rolls back atomically. This is the OCC stale-edit protection.
func TestIssue24_MultiEditExpectedHash_Rejected_StaleEdit(t *testing.T) {
	engine, tempDir := setupMultiEditHashEngine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "occ_stale.txt")
	original := []byte("alpha\nbeta\ngamma\n")
	if err := os.WriteFile(filePath, original, 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	// Capture the hash as a consumer would from a read_file call.
	staleHash := fnvHashFile(t, filePath)

	// Simulate a concurrent write: the file changes between the read
	// (which produced staleHash) and the multi_edit call.
	if err := os.WriteFile(filePath, []byte("DIFFERENT CONTENT\n"), 0644); err != nil {
		t.Fatalf("concurrent write: %v", err)
	}

	edits := []core.MultiEditOperation{
		{OldText: "alpha", NewText: "ALPHA"},
		{OldText: "beta", NewText: "BETA"},
	}

	// The OCC check rejects the whole batch with "stale edit:" — no edits apply.
	_, err := engine.MultiEdit(ctx, filePath, edits, false, false, false, staleHash)
	if err == nil {
		t.Fatal("MultiEdit with stale expected_hash should return error, got nil")
	}

	// Error string must mention the OCC signature so consumers can
	// pattern-match (parity with edit_file's response at
	// tools_core.go:767-768). The consumer-facing string is the one
	// edit_file returns to the MCP client.
	if !strings.Contains(err.Error(), "stale edit") {
		t.Errorf("error should mention 'stale edit', got: %v", err)
	}
	if !strings.Contains(err.Error(), staleHash) {
		t.Errorf("error should mention the wrong expected hash %q, got: %v", staleHash, err)
	}
	if !strings.Contains(err.Error(), "Re-read the file with read_file") {
		t.Errorf("error should include the read_file retry hint (parity with edit_file), got: %v", err)
	}

	// File must remain at the post-concurrent-write state — no edits applied.
	after, _ := os.ReadFile(filePath)
	if string(after) != "DIFFERENT CONTENT\n" {
		t.Errorf("file was modified despite stale hash. Expected %q, got %q",
			"DIFFERENT CONTENT\n", string(after))
	}
	if strings.Contains(string(after), "ALPHA") || strings.Contains(string(after), "BETA") {
		t.Errorf("stale-hash batch leaked partial edits into the file: %s", after)
	}
}

// TestIssue24_MultiEditExpectedHash_NotProvided verifies backward
// compatibility: omitting expected_hash preserves the original
// behavior — no OCC check is performed, the edits apply. This is
// the same contract edit_file offers (TestExpectedHash_NotProvided).
func TestIssue24_MultiEditExpectedHash_NotProvided(t *testing.T) {
	engine, tempDir := setupMultiEditHashEngine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "occ_omitted.txt")
	if err := os.WriteFile(filePath, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	edits := []core.MultiEditOperation{
		{OldText: "hello", NewText: "goodbye"},
	}

	// Empty expectedHash ("") disables the OCC check. Edit must apply.
	result, err := engine.MultiEdit(ctx, filePath, edits, false, false, false, "")
	if err != nil {
		t.Fatalf("MultiEdit without expected_hash should succeed, got: %v", err)
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("expected 1 successful edit, got %d", result.SuccessfulEdits)
	}

	after, _ := os.ReadFile(filePath)
	if !strings.Contains(string(after), "goodbye") {
		t.Errorf("file was not modified: %s", after)
	}
}

// TestIssue24_MultiEditExpectedHash_AtomicRollback verifies that the
// OCC check happens BEFORE the edit loop and the backup creation, so
// a stale hash never creates an unnecessary backup and never applies
// any edits — even though the file's current content DOES contain the
// anchor text. The OCC check is the gate; once it passes (or is
// skipped), the normal atomic-rollback semantics take over for the
// per-edit matching.
func TestIssue24_MultiEditExpectedHash_AtomicRollback(t *testing.T) {
	engine, tempDir := setupMultiEditHashEngine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "occ_atomic.txt")
	original := []byte("anchor1\nanchor2\nNONEXISTENT_TOKEN\n")
	if err := os.WriteFile(filePath, original, 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	// Use a deliberately wrong (but non-empty) hash to trigger the OCC
	// rejection — the anchors and the nonexistent token are irrelevant
	// to the OCC check; the check happens BEFORE the per-edit matching.
	wrongHash := "deadbeef"

	edits := []core.MultiEditOperation{
		{OldText: "anchor1", NewText: "ANCHOR1"},
		{OldText: "anchor2", NewText: "ANCHOR2"},
		{OldText: "NONEXISTENT_TOKEN", NewText: "REPLACED"},
	}

	_, err := engine.MultiEdit(ctx, filePath, edits, false, false, false, wrongHash)
	if err == nil {
		t.Fatal("MultiEdit with wrong expected_hash should return error, got nil")
	}
	if !strings.Contains(err.Error(), "stale edit") {
		t.Errorf("error should mention 'stale edit', got: %v", err)
	}

	// File must remain byte-for-byte identical to the original.
	after, _ := os.ReadFile(filePath)
	if string(after) != string(original) {
		t.Errorf("file was modified despite stale hash. Expected %q, got %q",
			string(original), string(after))
	}
}
