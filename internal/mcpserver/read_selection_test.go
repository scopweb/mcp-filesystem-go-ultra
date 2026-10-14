package mcpserver

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestReadFile_CompletedRangeIsNotTruncated(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := writeNumberedFile(t, dir, "range.go", 100, true)
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": path, "start_line": float64(82), "end_line": float64(85),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["status"] != statusOK || m["truncated"] != false || m["continuation"] != nil {
		t.Fatalf("completed range must not look incomplete: %#v", m)
	}
	if asIntMeta(m["start_line"]) != 82 || asIntMeta(m["end_line"]) != 85 || asIntMeta(m["total_lines"]) != 100 {
		t.Fatalf("range meta: %#v", m)
	}
	text := resultText(t, res)
	if strings.Contains(text, "read more") || strings.Contains(text, "To continue:") || strings.Contains(text, "[Truncated:") {
		t.Fatalf("completed range suggested continuation:\n%s", text)
	}
	body := bodyBeforeTruncationFooter(text)
	assertConsecutive(t, collectNumberedLines(body), 82, 85)
	full := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{"path": path, "max_lines": float64(1)})
	fm, _ := full.StructuredContent.(map[string]any)
	if fm["content_hash"] != m["content_hash"] {
		t.Fatalf("hash drifted: range %v full %v", m["content_hash"], fm["content_hash"])
	}
}

func TestReadFile_EOFAndOutOfRangeAreNotLimitCuts(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := writeNumberedFile(t, dir, "eof.go", 10, true)

	eof := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": path, "start_line": float64(8), "end_line": float64(40),
	})
	em, _ := eof.StructuredContent.(map[string]any)
	if em["truncated"] != false || em["continuation"] != nil || asIntMeta(em["end_line"]) != 10 {
		t.Fatalf("clamped EOF range: %#v", em)
	}
	if strings.Contains(resultText(t, eof), "read more") {
		t.Fatalf("EOF suggested more:\n%s", resultText(t, eof))
	}

	past := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": path, "start_line": float64(40), "end_line": float64(42),
	})
	pm, _ := past.StructuredContent.(map[string]any)
	if pm["truncated"] != false || pm["continuation"] != nil || pm["status"] != statusOK {
		t.Fatalf("past EOF is not a limit cut: %#v", pm)
	}

	emptyPath := writeNumberedFile(t, dir, "empty.go", 0, false)
	// writeNumberedFile with n=0 writes an empty file.
	empty := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": emptyPath, "start_line": float64(1), "end_line": float64(3),
	})
	if empty.IsError {
		t.Fatal(resultText(t, empty))
	}
	om, _ := empty.StructuredContent.(map[string]any)
	if om["truncated"] != false || om["continuation"] != nil {
		t.Fatalf("empty file range: %#v", om)
	}
}

func TestReadFile_AutoCapAndLineWidthStayTruncated(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	total := autoTruncateLargeFileLines + 5
	path := writeNumberedFile(t, dir, "big.go", total, true)
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{"path": path})
	m, _ := res.StructuredContent.(map[string]any)
	if m["truncated"] != true || m["status"] != statusTruncated {
		t.Fatalf("auto-cap: %#v", m)
	}
	cont, _ := m["continuation"].(map[string]any)
	if asIntMeta(cont["start_line"]) != autoTruncateLargeFileLines+1 || cont["kind"] != cutAutoCap {
		t.Fatalf("auto-cap continuation: %#v", cont)
	}

	wide := filepathJoin(t, dir, "wide.go")
	if err := os.WriteFile(wide, []byte(strings.Repeat("q", 40)+"\nshort\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cut := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": wide, "max_line_length": float64(8),
	})
	cm, _ := cut.StructuredContent.(map[string]any)
	if cm["truncated"] != true {
		t.Fatalf("line-width must truncate: %#v", cm)
	}
	ccont, _ := cm["continuation"].(map[string]any)
	if ccont["kind"] != cutLineWidth || ccont["start_line"] != nil {
		t.Fatalf("line-width continuation must not invent a next range: %#v", ccont)
	}
	full := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": wide, "max_line_length": float64(0),
	})
	fm, _ := full.StructuredContent.(map[string]any)
	if cm["content_hash"] != fm["content_hash"] || asString(cm["content_hash"]) == "" {
		t.Fatalf("width cut changed snapshot hash: cut %v full %v", cm["content_hash"], fm["content_hash"])
	}
}

func TestReadFile_OmittedHeadIsServerCap(t *testing.T) {
	content := numberedContent(defaultHeadTailMaxLines+3, true)
	p := projectRead(content, "f.go", 0, 0, 0, "head", 0)
	if !p.Truncated || p.Cut != cutServerMaxLines || p.ContinueAt != defaultHeadTailMaxLines+1 {
		t.Fatalf("omitted head cap: %+v", p)
	}
	if !strings.Contains(p.Text, "[Truncated:") {
		t.Fatalf("server cap must be identifiable: %q", p.Text)
	}
}

func filepathJoin(t *testing.T, dir, name string) string {
	t.Helper()
	return dir + string(os.PathSeparator) + name
}
