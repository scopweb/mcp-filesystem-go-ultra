package core

import "testing"

// TestComputeLineRangeDeletion_ByteExact is the core guarantee behind the batch
// "extract" action (point 4): removed + remaining must reconstruct the original
// exactly, so the bytes written to the destination equal the bytes removed from
// the source.
func TestComputeLineRangeDeletion_ByteExact(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		start, end  int
		wantRemoved string
		wantRemain  string
	}{
		{"middle", "a\nb\nc\nd\n", 2, 3, "b\nc\n", "a\nd\n"},
		{"first", "a\nb\nc\n", 1, 1, "a\n", "b\nc\n"},
		{"last_with_nl", "a\nb\nc\n", 3, 3, "c\n", "a\nb\n"},
		{"last_no_trailing_nl", "a\nb\nc", 3, 3, "c", "a\nb\n"},
		{"all", "a\nb\n", 1, 2, "a\nb\n", ""},
		{"clamp_end", "a\nb\n", 1, 99, "a\nb\n", ""},
		{"single_line_no_nl", "solo", 1, 1, "solo", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			removed, remaining, err := ComputeLineRangeDeletion(tc.content, tc.start, tc.end)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if removed != tc.wantRemoved {
				t.Errorf("removed = %q, want %q", removed, tc.wantRemoved)
			}
			if remaining != tc.wantRemain {
				t.Errorf("remaining = %q, want %q", remaining, tc.wantRemain)
			}
		})
	}
}

func TestComputeLineRangeDeletion_Errors(t *testing.T) {
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
			if _, _, err := ComputeLineRangeDeletion(tc.content, tc.start, tc.end); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestCountRemovedLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a\n", 1},
		{"a\nb\n", 2},
		{"a\nb", 2},
		{"solo", 1},
	}
	for _, tc := range cases {
		if got := countRemovedLines(tc.in); got != tc.want {
			t.Errorf("countRemovedLines(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
