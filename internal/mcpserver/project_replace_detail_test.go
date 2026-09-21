package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestFormatProjectReplace_CompactDefaultOmitsFiles(t *testing.T) {
	res := &core.ProjectReplaceResult{
		FilesChanged:  2,
		TotalReplaced: 3,
		PerFileResults: []core.ProjectReplaceFileResult{
			{Path: "b.go", Replaced: 1},
			{Path: "a.go", Replaced: 2},
		},
	}
	got := formatProjectReplace(true, `C:\p`, "foo", "bar", res, detailNormal)
	if strings.Contains(got, "a.go") || strings.Contains(got, "b.go") {
		t.Fatalf("compact normal must omit file list: %s", got)
	}
	if !strings.Contains(got, "2 files") || !strings.Contains(got, "3 replacements") {
		t.Fatalf("counters: %s", got)
	}
}

func TestFormatProjectReplace_CompactFullListsFilesSorted(t *testing.T) {
	res := &core.ProjectReplaceResult{
		FilesChanged:  2,
		TotalReplaced: 3,
		PerFileResults: []core.ProjectReplaceFileResult{
			{Path: "b.go", Replaced: 1},
			{Path: "a.go", Replaced: 2},
		},
	}
	got := formatProjectReplace(true, `C:\p`, "foo", "bar", res, detailFull)
	if !strings.Contains(got, "a.go: 2") || !strings.Contains(got, "b.go: 1") {
		t.Fatalf("expected sorted file list: %s", got)
	}
	if i, j := strings.Index(got, "a.go"), strings.Index(got, "b.go"); i < 0 || j < i {
		t.Fatalf("want a.go before b.go: %s", got)
	}
}

func TestFormatProjectReplace_VerboseNormalCaps20(t *testing.T) {
	files := make([]core.ProjectReplaceFileResult, 25)
	for i := 0; i < 25; i++ {
		files[i] = core.ProjectReplaceFileResult{Path: string(rune('a'+i%26)) + string(rune('0'+i/26)) + ".go", Replaced: 1}
	}
	res := &core.ProjectReplaceResult{FilesChanged: 25, TotalReplaced: 25, PerFileResults: files}
	got := formatProjectReplace(false, "/p", "x", "y", res, detailNormal)
	if !strings.Contains(got, `detail:"full"`) {
		t.Fatalf("cap hint: %s", got)
	}
	if strings.Count(got, " replacements\n") != 20 {
		t.Fatalf("want 20 listed, got:\n%s", got)
	}
}

func TestProjectReplace_HandlerCompactFullListsFiles(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().CompactMode = true
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("needle\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	res := callNamed(t, reg, context.Background(), "project_replace", map[string]interface{}{
		"path": dir, "find": "needle", "replace": "pin", "preview": true, "detail": "full",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "a.txt: 1") || !strings.Contains(text, "b.txt: 1") {
		t.Fatalf("compact detail=full should list files:\n%s", text)
	}
	if !strings.Contains(text, "PREVIEW") {
		t.Fatalf("preview status:\n%s", text)
	}
}
