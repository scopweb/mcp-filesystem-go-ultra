package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_BatchHonorsRange(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "vue.js")
	var b strings.Builder
	for i := 1; i <= 800; i++ {
		b.WriteString("L")
		b.WriteString(strings.Repeat("x", 8))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	res := callReadFile(t, reg, map[string]interface{}{
		"paths":      []string{path},
		"start_line": float64(633),
		"end_line":   float64(705),
	})
	if res.IsError {
		t.Fatalf("%v", res.Content)
	}
	text := resultText(t, res)
	if strings.Count(text, "\n") > 120 {
		t.Fatalf("batch read ignored the range (%d lines)", strings.Count(text, "\n"))
	}
	if !strings.Contains(text, "[Lines 633-705 of 800") {
		t.Fatalf("missing range footer:\n%s", text[max(0, len(text)-240):])
	}
}
