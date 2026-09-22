package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEOLReproOtherWriters(t *testing.T) {
	for _, route := range []string{"batch_edit", "batch_search_and_replace", "project_replace"} {
		for _, multiline := range []bool{false, true} {
			name := route + "/single"
			if multiline { name = route + "/multiline" }
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				e := testEngine(t, dir)
				path := filepath.Join(dir, "f.txt")
				original := "\ufeffhead\r\nold\r\nkeep\nend\r\n"
				newText := "new"
				want := "\ufeffhead\r\nnew\r\nkeep\nend\r\n"
				if multiline {
					newText = "new\nextra"
					want = "\ufeffhead\r\nnew\r\nextra\r\nkeep\nend\r\n"
				}
				if err := os.WriteFile(path, []byte(original), 0600); err != nil { t.Fatal(err) }
				if route == "project_replace" {
					_, err := e.ProjectReplace(context.Background(), dir, "old", newText, true, true, ".txt", nil, nil, false, false, false, 10, true)
					if err != nil { t.Fatal(err) }
				} else {
					m := NewBatchOperationManager(t.TempDir(), 10)
					m.SetEngine(e)
					op := "edit"
					if route == "batch_search_and_replace" { op = "search_and_replace" }
					r := m.ExecuteBatch(BatchRequest{Operations: []FileOperation{{Type: op, Path: path, OldText: "old", NewText: newText}}, Atomic: true, Force: true})
					if !r.Success { t.Fatalf("batch failed: %+v", r) }
				}
				got, err := os.ReadFile(path)
				if err != nil { t.Fatal(err) }
				t.Logf("disk=%q", got)
				if string(got) != want { t.Errorf("got %q want %q", got, want) }
			})
		}
	}
}
