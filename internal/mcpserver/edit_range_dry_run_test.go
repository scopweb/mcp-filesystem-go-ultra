package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReplaceRangeDryRun_CitesOriginalBounds(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		if i == 8 {
			fmt.Fprintf(&b, "L%03d-%sTAILMARK\n", i, strings.Repeat("x", 200))
			continue
		}
		fmt.Fprintf(&b, "L%03d-TOKEN\n", i)
	}
	original := b.String()
	path := filepath.Join(dir, "range.txt")
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	res := callEdit(t, reg, map[string]interface{}{
		"path":       path,
		"mode":       "replace_range",
		"start_line": float64(8),
		"end_line":   float64(30),
		"new_text":   "X\n",
		"dry_run":    true,
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	for _, want := range []string{"8: L008-", "30: L030-TOKEN", "31: L031-TOKEN"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing citation %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "19: L019-TOKEN") {
		t.Fatal("dry-run dumped the original range")
	}
	var cite8 string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "8: ") {
			cite8 = line
			break
		}
	}
	if cite8 == "" || utf8.RuneCountInString(cite8) > 120 || strings.Contains(cite8, "TAILMARK") {
		t.Fatalf("start citation not trimmed to 120: %q", cite8)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatal("replace_range dry_run wrote the file")
	}

	del := callEdit(t, reg, map[string]interface{}{
		"path":       path,
		"mode":       "delete_range",
		"start_line": float64(48),
		"end_line":   float64(50),
		"dry_run":    true,
	})
	if del.IsError {
		t.Fatal(resultText(t, del))
	}
	delText := resultText(t, del)
	for _, want := range []string{"48: L048-TOKEN", "50: L050-TOKEN", "end is last line"} {
		if !strings.Contains(delText, want) {
			t.Fatalf("delete_range missing %q in:\n%s", want, delText)
		}
	}
	after, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatal("delete_range dry_run wrote the file")
	}
}
