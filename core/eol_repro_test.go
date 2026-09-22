package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These fixtures deliberately keep their EOL bytes in escaped strings.
func TestEOLReproSingleLine(t *testing.T) {
	for _, tc := range []struct{ name, original string }{
		{"CRLF_BOM", "\ufeffhead\r\n<div old>\r\nkeep\r\nend\r\n"},
		{"LF", "head\n<div old>\nkeep\nend\n"},
		{"mixed_BOM", "\ufeffhead\r\n<div old>\r\nkeep\nend\r\n"},
	} {
		for _, multi := range []bool{false, true} {
			name := "edit_file"
			if multi { name = "multi_edit" }
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				e := testEngine(t, dir)
				path := filepath.Join(dir, "Pda.cshtml")
				if err := os.WriteFile(path, []byte(tc.original), 0600); err != nil { t.Fatal(err) }
				want := strings.Replace(tc.original, "<div old>", "<div new>", 1)
				var predicted string
				for _, dry := range []bool{true, false} {
					var got string
					if multi {
						r, err := e.MultiEdit(context.Background(), path, []MultiEditOperation{{OldText: "old", NewText: "new"}}, false, dry, false, "")
						if err != nil { t.Fatal(err) }
						got = r.FinalContent
						if r.LinesAdded != 1 || r.LinesRemoved != 1 { t.Errorf("dry=%v reported diff +%d -%d; want +1 -1", dry, r.LinesAdded, r.LinesRemoved) }
					} else {
						r, err := e.EditFile(context.Background(), path, "old", "new", false, dry, false)
						if err != nil { t.Fatal(err) }
						got = r.ModifiedContent
					}
					disk, err := os.ReadFile(path)
					if err != nil { t.Fatal(err) }
					t.Logf("dry=%v result=%q hash=%s disk=%q diff=%s", dry, got, contentHashFNV(got), disk, DiffStats(tc.original, got))
					if got != want { t.Errorf("dry=%v result bytes differ: got %q want %q", dry, got, want) }
					if contentHashFNV(got) != contentHashFNV(want) { t.Errorf("dry=%v hash=%s want %s", dry, contentHashFNV(got), contentHashFNV(want)) }
					if stats := DiffStats(tc.original, got); stats != "+1 -1" { t.Errorf("dry=%v diff=%s", dry, stats) }
					if dry {
						predicted = got
						if string(disk) != tc.original { t.Error("dry run changed disk") }
					} else {
						if string(disk) != want { t.Errorf("disk=%q want %q", disk, want) }
						if got != predicted { t.Error("dry-run prediction differs from applied result") }
					}
				}
			})
		}
	}
}

func TestEOLReproStrictMultiLine(t *testing.T) {
	for _, old := range []string{"alpha\nbeta", "alpha\r\nbeta"} {
		t.Run(strings.ReplaceAll(old, "\n", "LF"), func(t *testing.T) {
			dir := t.TempDir()
			e := testEngine(t, dir)
			path := filepath.Join(dir, "mixed.txt")
			original := "\ufeffhead\r\nalpha\nbeta\r\ntail\r\n"
			if err := os.WriteFile(path, []byte(original), 0600); err != nil { t.Fatal(err) }
			ctx := WithEditPolicy(context.Background(), EditPolicy{Strict: true})
			r, err := e.MultiEdit(ctx, path, []MultiEditOperation{{OldText: old, NewText: "ALPHA\nBETA"}}, false, true, false, "")
			if err != nil { t.Fatal(err) }
			want := "\ufeffhead\r\nALPHA\r\nBETA\r\ntail\r\n"
			if r.FinalContent != want { t.Errorf("got %q want %q", r.FinalContent, want) }
		})
	}
}

func TestEOLReproDiffOnly(t *testing.T) {
	old := "\ufeffhead\r\nkeep\r\nend\r\n"
	lf := strings.ReplaceAll(old, "\r\n", "\n")
	if got := UnifiedDiff(old, lf, "f.txt"); got != "" { t.Errorf("EOL-only diff must be empty: %q", got) }
	if got := DiffStats(old, lf); got != "+0 -0" { t.Errorf("EOL-only stats=%s", got) }
}

func TestEOLReproPatchMixed(t *testing.T) {
	old := "\ufeffhead\r\nold\r\nkeep\nend\r\n"
	got, err := ApplyUnifiedPatch(old, "--- a/f.txt\n+++ b/f.txt\n@@ -2,1 +2,1 @@\n-old\n+new\n")
	if err != nil { t.Fatal(err) }
	want := strings.Replace(old, "old", "new", 1)
	if got != want { t.Errorf("got %q want %q", got, want) }
}
