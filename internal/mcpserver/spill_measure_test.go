package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func spillLine(label string, n int) string {
	mark := "ok"
	if n > claudeSpillChars {
		mark = "SPILL"
	}
	return fmt.Sprintf("%s %s chars=%d", mark, label, n)
}

func TestMeasureClaudeSpill(t *testing.T) {
	var lines []string

	typical := strings.Repeat("line of about eighty characters of ordinary source text padding.\n", 300)
	p := projectRead(typical, "typical.go", 0, 0, 0, "all", 0)
	lines = append(lines, spillLine("read_file default 300x80", len(p.Text)))

	wide := strings.Repeat(strings.Repeat("w", 200)+"\n", 300)
	wideProj := projectRead(wide, "wide.go", 0, 0, 0, "all", 0)
	lines = append(lines, spillLine("read_file default 300x200", len(wideProj.Text)))
	if !wideProj.Truncated || wideProj.ContinueAt <= 1 {
		t.Fatalf("wide read must continue with start_line, got %+v", wideProj)
	}

	fat := strings.Repeat(strings.Repeat("f", 2000)+"\n", 300)
	p = projectRead(fat, "fat.log", 0, 0, 0, "all", 0)
	lines = append(lines, spillLine("read_file default 300x2000", len(p.Text)))

	rangeBody := strings.Repeat(strings.Repeat("r", 400)+"\n", 400)
	p = projectRead(rangeBody, "range.go", 1, 400, 0, "all", 0)
	lines = append(lines, spillLine("read_file explicit range 400x400", len(p.Text)))

	head := strings.Repeat(strings.Repeat("h", 2000)+"\n", 500)
	p = projectRead(head, "head.log", 0, 0, 0, "head", 0)
	lines = append(lines, spillLine("read_file head omitted cap", len(p.Text)))

	dir := t.TempDir()
	for i := 0; i < 500; i++ {
		name := fmt.Sprintf("entry-%03d-%s.txt", i, strings.Repeat("n", 40))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := newHelpTestRegistry(t, dir)

	tree := callDetailTool(t, reg, "directory_tree", map[string]any{
		"path": dir, "max_depth": float64(1), "max_nodes": float64(500),
	})
	if tree.IsError {
		t.Fatal(resultText(t, tree))
	}
	lines = append(lines, spillLine("directory_tree default 500", len(resultText(t, tree))))

	list := callDetailTool(t, reg, "list_directory", map[string]any{"path": dir})
	if list.IsError {
		t.Fatal(resultText(t, list))
	}
	lines = append(lines, spillLine("list_directory compact 500", len(resultText(t, list))))

	jsonList := callDetailTool(t, reg, "list_directory", map[string]any{
		"path": dir, "output_format": "json",
	})
	if jsonList.IsError {
		t.Fatal(resultText(t, jsonList))
	}
	lines = append(lines, spillLine("list_directory json 500", len(resultText(t, jsonList))))

	searchFile := filepath.Join(dir, "search-target.txt")
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("NEEDLE ")
		b.WriteString(strings.Repeat("s", 600))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(searchFile, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	search := callDetailTool(t, reg, "search_files", map[string]any{
		"path": dir, "pattern": "NEEDLE", "include_content": true,
	})
	if search.IsError {
		t.Fatal(resultText(t, search))
	}
	lines = append(lines, spillLine("search_files page 40 long lines", len(resultText(t, search))))

	var many strings.Builder
	for i := 0; i < 200; i++ {
		many.WriteString("TOKEN ")
		many.WriteString(strings.Repeat("q", 600))
		many.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "search-many.txt"), []byte(many.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	searchMany := callDetailTool(t, reg, "search_files", map[string]any{
		"path": dir, "pattern": "TOKEN", "include_content": true, "max_results": float64(200),
	})
	if searchMany.IsError {
		t.Fatal(resultText(t, searchMany))
	}
	lines = append(lines, spillLine("search_files max_results 200", len(resultText(t, searchMany))))
	if sc, _ := searchMany.StructuredContent.(map[string]any); sc["continuation"] == nil {
		t.Fatalf("search spill must keep offset continuation: %#v", sc)
	}
	if sc, _ := jsonList.StructuredContent.(map[string]any); sc["continuation"] == nil {
		t.Fatalf("json listing spill must keep continuation: %#v", sc)
	}

	t.Log("\n" + strings.Join(lines, "\n"))
	for _, line := range lines {
		if strings.HasPrefix(line, "SPILL") {
			t.Errorf("%s", line)
		}
	}
}
