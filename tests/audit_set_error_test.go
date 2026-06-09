package main

import (
	"context"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// TestSetError_SetsFieldAndForcesErrorStatus verifies that SetError correctly
// annotates the AuditEntry in context with a custom error message and
// downgrades the status to "error" if it was "ok".
func TestSetError_SetsFieldAndForcesErrorStatus(t *testing.T) {
	entry := &core.AuditEntry{Tool: "edit_file", Status: "ok"}
	ctx := context.WithValue(context.Background(), core.AuditEntryKey{}, entry)

	core.SetError(ctx, "stale edit: file content changed since read")

	if entry.Error != "stale edit: file content changed since read" {
		t.Errorf("expected Error set, got %q", entry.Error)
	}
	if entry.Status != "error" {
		t.Errorf("expected Status=error after SetError, got %q", entry.Status)
	}
}

// TestSetError_EmptyMessageIsNoOp verifies that calling SetError with ""
// does not modify the entry — guards against accidental empty overwrites.
func TestSetError_EmptyMessageIsNoOp(t *testing.T) {
	entry := &core.AuditEntry{Tool: "read_file", Status: "ok", Error: "previous"}
	ctx := context.WithValue(context.Background(), core.AuditEntryKey{}, entry)

	core.SetError(ctx, "")

	if entry.Error != "previous" {
		t.Errorf("empty SetError should preserve previous error, got %q", entry.Error)
	}
	if entry.Status != "ok" {
		t.Errorf("empty SetError should preserve status, got %q", entry.Status)
	}
}

// TestSetError_NoOpWithoutEntry verifies that SetError is a no-op when no
// AuditEntry is in the context (e.g., direct engine calls outside auditWrap).
func TestSetError_NoOpWithoutEntry(t *testing.T) {
	ctx := context.Background()
	// Must not panic
	core.SetError(ctx, "this should be silently dropped")
}
