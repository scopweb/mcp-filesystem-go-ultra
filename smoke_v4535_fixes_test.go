package main

// Live regression coverage for v4.5.35 (proxy-log driven fixes):
//  1. edit_file accepts and ignores the benign "description" param.
//  2. multi_edit recovers edits_json with trailing commas.
//  3. renameWithRetry gets past a transient Windows destination lock.

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveFix_DescriptionParamIgnored(t *testing.T) {
	dir := t.TempDir()
	s, _ := newIncidentFixServer(t, dir)
	target := dir + string(os.PathSeparator) + "desc.txt"

	callServer(t, s, "write_file", map[string]any{
		"path": target, "content": "alpha\nbeta\n",
	})
	r := callServer(t, s, "edit_file", map[string]any{
		"path": target, "old_text": "beta", "new_text": "BETA",
		"description": "capitalize the second line",
	})
	if r.IsError {
		t.Fatalf("edit_file with description param failed: %s", textFromResult(t, r))
	}
	data, _ := os.ReadFile(target)
	if !strings.Contains(string(data), "BETA") {
		t.Fatalf("edit not applied, content: %q", data)
	}
}

func TestLiveFix_TrailingCommaEditsJSON(t *testing.T) {
	dir := t.TempDir()
	s, _ := newIncidentFixServer(t, dir)
	target := dir + string(os.PathSeparator) + "tc.txt"

	callServer(t, s, "write_file", map[string]any{
		"path": target, "content": "one\ntwo\nthree\n",
	})
	// Trailing comma inside the object AND after the last array element —
	// the exact shapes the proxy log showed as "Invalid edits JSON".
	editsJSON := `[{"old_text": "two", "new_text": "TWO",}, {"old_text": "three", "new_text": "THREE"},]`
	r := callServer(t, s, "multi_edit", map[string]any{
		"path": target, "edits_json": editsJSON,
	})
	if r.IsError {
		t.Fatalf("multi_edit with trailing commas failed: %s", textFromResult(t, r))
	}
	data, _ := os.ReadFile(target)
	content := string(data)
	if !strings.Contains(content, "TWO") || !strings.Contains(content, "THREE") {
		t.Fatalf("edits not applied, content: %q", content)
	}
}

func TestLiveFix_RenameRetryPastWindowsLock(t *testing.T) {
	dir := t.TempDir()
	s, _ := newIncidentFixServer(t, dir)
	target := dir + string(os.PathSeparator) + "locked.txt"

	callServer(t, s, "write_file", map[string]any{
		"path": target, "content": "before\n",
	})

	// Hold the destination open WITHOUT delete-sharing (Go's os.Open uses
	// FILE_SHARE_READ|FILE_SHARE_WRITE, no DELETE) — os.Rename on Windows
	// fails with ERROR_ACCESS_DENIED while this handle lives. Release after
	// 200ms: a single-shot rename would fail, renameWithRetry (budget ~750ms)
	// must succeed.
	fh, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		fh.Close()
	}()

	start := time.Now()
	r := callServer(t, s, "edit_file", map[string]any{
		"path": target, "old_text": "before", "new_text": "after",
	})
	elapsed := time.Since(start)
	if r.IsError {
		t.Fatalf("edit under transient lock failed: %s", textFromResult(t, r))
	}
	data, _ := os.ReadFile(target)
	if strings.TrimSpace(string(data)) != "after" {
		t.Fatalf("content after locked edit: %q", data)
	}
	t.Logf("edit succeeded under transient lock in %v (retry path exercised)", elapsed)
}
