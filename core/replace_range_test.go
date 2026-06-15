package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeLineRangeReplacement(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		start, end  int
		newText     string
		wantRemoved string
		wantResult  string
	}{
		{"middle_multiline", "a\nb\nc\nd\n", 2, 3, "X\nY\n", "b\nc\n", "a\nX\nY\nd\n"},
		{"newtext_no_nl_midfile", "a\nb\nc\n", 2, 2, "X", "b\n", "a\nX\nc\n"},
		{"last_line_file_ends_nl", "a\nb\n", 2, 2, "X", "b\n", "a\nX\n"},
		{"last_line_no_trailing_nl", "a\nb", 2, 2, "X", "b", "a\nX"},
		{"clamp_whole_file", "a\nb\n", 1, 99, "Z\n", "a\nb\n", "Z\n"},
		{"empty_newtext_is_delete", "a\nb\nc\n", 2, 2, "", "b\n", "a\nc\n"},
		{"first_line", "a\nb\nc\n", 1, 1, "HEAD\n", "a\n", "HEAD\nb\nc\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			removed, result, err := ComputeLineRangeReplacement(tc.content, tc.start, tc.end, tc.newText)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if removed != tc.wantRemoved {
				t.Errorf("removed = %q, want %q", removed, tc.wantRemoved)
			}
			if result != tc.wantResult {
				t.Errorf("result = %q, want %q", result, tc.wantResult)
			}
		})
	}
}

func TestComputeLineRangeReplacement_Errors(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		start, end int
	}{
		{"empty", "", 1, 1},
		{"start_zero", "a\nb\n", 0, 1},
		{"end_before_start", "a\nb\n", 2, 1},
		{"start_beyond", "a\nb\n", 5, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ComputeLineRangeReplacement(tc.content, tc.start, tc.end, "X\n"); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

// TestReplaceLineRange_EndToEnd checks the engine method writes the spliced
// content and stamps a NewHash matching the file on disk.
func TestReplaceLineRange_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)

	res, err := engine.ReplaceLineRange(context.Background(), path, 2, 3, "X\nY\n")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "a\nX\nY\nd\n" {
		t.Errorf("file = %q, want %q", string(raw), "a\nX\nY\nd\n")
	}
	if want := contentHashFNV(string(raw)); res.NewHash != want {
		t.Errorf("NewHash = %s, want %s (hash of file on disk)", res.NewHash, want)
	}
}
