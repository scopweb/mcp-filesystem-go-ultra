package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"testing"
)

// TestComputeFileOCCHash_MatchesRawBytesFNV verifies that the OCC hash surfaced
// for partial reads (point 3: range/head/tail/base64) equals the FNV-1a of the
// file's raw bytes — the exact value edit_file / multi_edit validate via
// expected_hash. If these diverged, an expected_hash obtained from a range read
// would always be (incorrectly) rejected as stale.
func TestComputeFileOCCHash_MatchesRawBytesFNV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	content := []byte("line1\nline2\nline3\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	got, ok := computeFileOCCHash(path)
	if !ok {
		t.Fatal("computeFileOCCHash returned ok=false for an existing file")
	}

	h := fnv.New32a()
	h.Write(content)
	want := fmt.Sprintf("%08x", h.Sum32())

	if got != want {
		t.Errorf("computeFileOCCHash = %s, want %s (raw-bytes FNV used by edit_file)", got, want)
	}
}

func TestComputeFileOCCHash_MissingFile(t *testing.T) {
	if _, ok := computeFileOCCHash(filepath.Join(t.TempDir(), "nope.txt")); ok {
		t.Error("expected ok=false for a missing file")
	}
}
