package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
)

func testEngine(t *testing.T, dir string) *UltraFastEngine {
	t.Helper()
	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewUltraFastEngine(&Config{Cache: c, AllowedPaths: []string{dir}, ParallelOps: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func TestMultiEdit_AmbiguousAfterValid_DiagnosedAndUnchanged(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	path := filepath.Join(dir, "f.txt")
	original := "foo\nbar\nfoo\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := e.MultiEdit(context.Background(), path, []MultiEditOperation{
		{OldText: "bar", NewText: "baz"},
		{OldText: "foo", NewText: "qux"},
	}, false, false, false, "")
	if err == nil {
		t.Fatal("expected atomic rollback")
	}
	msg := err.Error()
	if strings.Contains(msg, "Failed: .") || strings.HasSuffix(strings.TrimSpace(msg), "Failed:") {
		t.Fatalf("empty failed list: %s", msg)
	}
	if !strings.Contains(msg, "ambiguous") && !strings.Contains(msg, "[ambiguous]") {
		t.Fatalf("expected ambiguous diagnosis: %s", msg)
	}
	if !strings.Contains(msg, "edit 2") {
		t.Fatalf("expected edit index: %s", msg)
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Fatalf("file changed: %q", got)
	}
}

func TestMultiEdit_ValidThenMissing_Diagnosed(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	path := filepath.Join(dir, "f.txt")
	original := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := e.MultiEdit(context.Background(), path, []MultiEditOperation{
		{OldText: "beta", NewText: "BETA"},
		{OldText: "missing", NewText: "x"},
	}, false, false, false, "")
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "edit 2") {
		t.Fatalf("missing edit 2: %s", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Fatalf("file changed: %q", got)
	}
}

func TestMultiEdit_AllFailed_Diagnosed(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("only\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := e.MultiEdit(context.Background(), path, []MultiEditOperation{
		{OldText: "nope", NewText: "a"},
		{OldText: "also", NewText: "b"},
	}, false, false, false, "")
	if err == nil {
		t.Fatal("expected failure")
	}
	if res != nil && res.FailedEdits != 2 && !strings.Contains(err.Error(), "none of the") {
		if !strings.Contains(err.Error(), "edit 1") && !strings.Contains(err.Error(), "no edits were successful") {
			t.Fatalf("undiagnosed: %s res=%+v", err, res)
		}
	}
}

func TestFormatFailedEditDetails_IncludesAmbiguous(t *testing.T) {
	got := formatFailedEditDetails([]EditDetail{
		{Index: 0, Status: EditStatusApplied},
		{Index: 1, Status: EditStatusAmbiguous, Error: "old_text matched 3 times"},
		{Index: 2, Status: EditStatusFailed, Error: "no match"},
	})
	joined := strings.Join(got, "; ")
	if !strings.Contains(joined, "edit 2 [ambiguous]") || !strings.Contains(joined, "edit 3 [failed]") {
		t.Fatalf("got %q", joined)
	}
	if strings.Contains(joined, "edit 1") {
		t.Fatalf("applied should not appear: %q", joined)
	}
}
