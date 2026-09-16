package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeNumberedFile(t *testing.T, dir, name string, n int, trailingNL bool) string {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d", i)
		if i < n || trailingNL {
			b.WriteByte('\n')
		}
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func numberedContent(n int, trailingNL bool) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d", i)
		if i < n || trailingNL {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func bodyBeforeTruncationFooter(text string) string {
	if idx := strings.Index(text, "\n[Truncated:"); idx >= 0 {
		return text[:idx]
	}
	if idx := strings.Index(text, "\n\n[Lines "); idx >= 0 {
		return text[:idx]
	}
	return text
}

func hasNumberedLine(text string, n int) bool {
	return regexp.MustCompile(fmt.Sprintf(`(?m)^line %d$`, n)).MatchString(text)
}

func collectNumberedLines(text string) []int {
	re := regexp.MustCompile(`(?m)^line (\d+)$`)
	matches := re.FindAllStringSubmatch(text, -1)
	out := make([]int, 0, len(matches))
	for _, m := range matches {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		out = append(out, n)
	}
	return out
}

func assertConsecutive(t *testing.T, got []int, from, to int) {
	t.Helper()
	want := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		want = append(want, i)
	}
	if len(got) != len(want) {
		t.Fatalf("line numbers %v, want consecutive %d-%d", got, from, to)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line numbers %v, want consecutive %d-%d", got, from, to)
		}
	}
}

func TestProjectRead_MaxLinesPrefixModes(t *testing.T) {
	content := numberedContent(10, true)
	for _, mode := range []string{"", "all"} {
		p := projectRead(content, "f.go", 0, 0, 4, mode, 0)
		if !p.Truncated {
			t.Fatalf("mode=%q: expected truncated", mode)
		}
		if p.StartLine != 1 || p.EndLine != 4 || p.TotalLines != 10 || p.ContinueAt != 5 {
			t.Fatalf("mode=%q meta start=%d end=%d total=%d continue=%d", mode, p.StartLine, p.EndLine, p.TotalLines, p.ContinueAt)
		}
		body := bodyBeforeTruncationFooter(p.Text)
		assertConsecutive(t, collectNumberedLines(body), 1, 4)
		if hasNumberedLine(body, 5) || hasNumberedLine(body, 10) {
			t.Fatalf("mode=%q must not skip to the tail: %q", mode, body)
		}
		if strings.Contains(p.Text, "lines omitted") || strings.Contains(p.Text, "head +") || strings.Contains(p.Text, "mode=all") {
			t.Fatalf("mode=%q contradictory/old footer: %q", mode, p.Text)
		}
		if !strings.Contains(p.Text, "To continue: start_line=5, max_lines=4") {
			t.Fatalf("mode=%q missing continuation footer: %q", mode, p.Text)
		}
	}
}

func TestProjectRead_MaxLinesEvenOddAndOne(t *testing.T) {
	content := numberedContent(11, true)
	cases := []struct {
		maxLines int
		end      int
	}{
		{6, 6},
		{5, 5},
		{1, 1},
	}
	for _, tc := range cases {
		p := projectRead(content, "f.go", 0, 0, tc.maxLines, "all", 0)
		body := bodyBeforeTruncationFooter(p.Text)
		got := collectNumberedLines(body)
		assertConsecutive(t, got, 1, tc.end)
		if p.EndLine != tc.end || p.ContinueAt != tc.end+1 {
			t.Fatalf("max_lines=%d end=%d continue=%d", tc.maxLines, p.EndLine, p.ContinueAt)
		}
		if hasNumberedLine(body, 11) {
			t.Fatalf("max_lines=%d must not include the last line", tc.maxLines)
		}
	}
}

func TestProjectRead_MaxLinesFileFits(t *testing.T) {
	withNL := numberedContent(4, true)
	noNL := numberedContent(4, false)
	for _, content := range []string{withNL, noNL} {
		p := projectRead(content, "f.go", 0, 0, 4, "all", 0)
		if p.Truncated {
			t.Fatalf("equal length must not truncate: %q", p.Text)
		}
		if p.Text != content {
			t.Fatalf("must return original bytes, trailingNL=%v", strings.HasSuffix(content, "\n"))
		}
		p2 := projectRead(content, "f.go", 0, 0, 10, "all", 0)
		if p2.Truncated || p2.Text != content {
			t.Fatalf("smaller file must be returned whole")
		}
	}
}

func TestProjectRead_MaxLinesTrailingNewlineTruncated(t *testing.T) {
	withNL := numberedContent(8, true)
	noNL := numberedContent(8, false)
	for _, content := range []string{withNL, noNL} {
		p := projectRead(content, "f.go", 0, 0, 3, "all", 0)
		body := bodyBeforeTruncationFooter(p.Text)
		assertConsecutive(t, collectNumberedLines(body), 1, 3)
		if p.TotalLines != 8 || p.EndLine != 3 {
			t.Fatalf("total=%d end=%d", p.TotalLines, p.EndLine)
		}
	}
}

func TestProjectRead_HeadTailAndOmittedMaxLinesPreserved(t *testing.T) {
	content := numberedContent(10, true)
	head := projectRead(content, "f.go", 0, 0, 3, "head", 0)
	assertConsecutive(t, collectNumberedLines(bodyBeforeTruncationFooter(head.Text)), 1, 3)
	if head.ContinueAt != 4 {
		t.Fatalf("head continue=%d", head.ContinueAt)
	}

	tail := projectRead(content, "f.go", 0, 0, 3, "tail", 0)
	assertConsecutive(t, collectNumberedLines(bodyBeforeTruncationFooter(tail.Text)), 8, 10)
	if tail.StartLine != 8 || tail.ContinueAt != 0 {
		t.Fatalf("tail start=%d continue=%d", tail.StartLine, tail.ContinueAt)
	}

	full := projectRead(content, "f.go", 0, 0, 0, "all", 0)
	if full.Truncated || full.Text != content {
		t.Fatalf("omitted max_lines on small file must return original")
	}
}

func TestProjectRead_OmittedMaxLinesStillAutoCaps(t *testing.T) {
	total := autoTruncateLargeFileLines + 20
	content := numberedContent(total, true)
	p := projectRead(content, "big.go", 0, 0, 0, "all", 0)
	if !p.Truncated || p.EndLine != autoTruncateLargeFileLines || p.ContinueAt != autoTruncateLargeFileLines+1 {
		t.Fatalf("auto-cap meta: truncated=%v end=%d continue=%d", p.Truncated, p.EndLine, p.ContinueAt)
	}
	body := bodyBeforeTruncationFooter(p.Text)
	if hasNumberedLine(body, autoTruncateLargeFileLines+1) {
		t.Fatal("auto-cap must not include lines past the threshold")
	}
	if !hasNumberedLine(body, 1) || !hasNumberedLine(body, autoTruncateLargeFileLines) {
		t.Fatal("auto-cap must keep a consecutive prefix")
	}
}

func TestReadFileHandler_MaxLinesPrefixOmittedAndAll(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := writeNumberedFile(t, dir, "seq.go", 12, true)

	for _, args := range []map[string]interface{}{
		{"path": path, "max_lines": float64(5)},
		{"path": path, "max_lines": float64(5), "mode": "all"},
	} {
		res := callReadFile(t, reg, args)
		if res.IsError {
			t.Fatalf("read_file error: %v", res.Content)
		}
		text := resultText(t, res)
		body := bodyBeforeTruncationFooter(text)
		assertConsecutive(t, collectNumberedLines(body), 1, 5)
		if hasNumberedLine(body, 6) || hasNumberedLine(body, 12) {
			t.Fatalf("must be a consecutive prefix, got:\n%s", body)
		}
		m, _ := res.StructuredContent.(map[string]any)
		if m["truncated"] != true {
			t.Fatalf("truncated=%v", m["truncated"])
		}
		if asIntMeta(m["start_line"]) != 1 || asIntMeta(m["end_line"]) != 5 || asIntMeta(m["total_lines"]) != 12 {
			t.Fatalf("structured range: %#v", m)
		}
		cont, _ := m["continuation"].(map[string]any)
		if asIntMeta(cont["start_line"]) != 6 || asIntMeta(cont["max_lines"]) != 5 {
			t.Fatalf("continuation: %#v", cont)
		}
		if strings.Contains(fmt.Sprint(cont["hint"]), "mode=all") {
			t.Fatalf("hint must not recommend mode=all: %#v", cont)
		}
		if !strings.Contains(text, "To continue: start_line=6, max_lines=5") {
			t.Fatalf("text footer mismatch:\n%s", text)
		}
		if !strings.Contains(asString(m["content"]), body) {
			t.Fatal("structured content must include the same prefix")
		}
	}
}

func TestReadFileHandler_MaxLinesContinuationNoGaps(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := writeNumberedFile(t, dir, "cont.go", 10, true)

	first := callReadFile(t, reg, map[string]interface{}{
		"path": path, "max_lines": float64(4),
	})
	m, _ := first.StructuredContent.(map[string]any)
	cont, _ := m["continuation"].(map[string]any)
	nextStart := asIntMeta(cont["start_line"])
	chunk := asIntMeta(cont["max_lines"])
	if nextStart != 5 || chunk != 4 {
		t.Fatalf("continuation want start=5 max_lines=4 got %#v", cont)
	}

	second := callReadFile(t, reg, map[string]interface{}{
		"path":       path,
		"start_line": float64(nextStart),
		"max_lines":  float64(chunk),
	})
	if second.IsError {
		t.Fatalf("continuation read error: %v", second.Content)
	}
	secondBody := bodyBeforeTruncationFooter(resultText(t, second))
	assertConsecutive(t, collectNumberedLines(secondBody), 5, 8)
	firstBody := bodyBeforeTruncationFooter(resultText(t, first))
	for _, n := range collectNumberedLines(firstBody) {
		if hasNumberedLine(secondBody, n) {
			t.Fatalf("duplicated line %d across chunks", n)
		}
	}

	sm, _ := second.StructuredContent.(map[string]any)
	scont, _ := sm["continuation"].(map[string]any)
	third := callReadFile(t, reg, map[string]interface{}{
		"path":       path,
		"start_line": float64(asIntMeta(scont["start_line"])),
		"max_lines":  float64(4),
	})
	thirdBody := bodyBeforeTruncationFooter(resultText(t, third))
	assertConsecutive(t, collectNumberedLines(thirdBody), 9, 10)
	tm, _ := third.StructuredContent.(map[string]any)
	if tm["continuation"] != nil {
		t.Fatalf("final chunk must not continue: %#v", tm)
	}
}

func TestReadFileHandler_MaxLinesOneAndFits(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := writeNumberedFile(t, dir, "one.go", 7, false)

	one := callReadFile(t, reg, map[string]interface{}{
		"path": path, "max_lines": float64(1),
	})
	body := bodyBeforeTruncationFooter(resultText(t, one))
	assertConsecutive(t, collectNumberedLines(body), 1, 1)
	m, _ := one.StructuredContent.(map[string]any)
	if asIntMeta(m["end_line"]) != 1 || asIntMeta(m["total_lines"]) != 7 {
		t.Fatalf("max_lines=1 meta: %#v", m)
	}

	fits := callReadFile(t, reg, map[string]interface{}{
		"path": path, "max_lines": float64(7),
	})
	fm, _ := fits.StructuredContent.(map[string]any)
	if fm["truncated"] != false || fm["continuation"] != nil {
		t.Fatalf("exact fit must not truncate: %#v", fm)
	}
	if !strings.Contains(resultText(t, fits), "line 7") {
		t.Fatal("exact fit must include the last line")
	}
}

func TestReadFileHandler_HeadTailAndZeroMaxLinesPreserved(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := writeNumberedFile(t, dir, "keep.go", 9, true)

	head := callReadFile(t, reg, map[string]interface{}{
		"path": path, "mode": "head", "max_lines": float64(2), "max_line_length": float64(0),
	})
	assertConsecutive(t, collectNumberedLines(bodyBeforeTruncationFooter(resultText(t, head))), 1, 2)

	tail := callReadFile(t, reg, map[string]interface{}{
		"path": path, "mode": "tail", "max_lines": float64(2), "max_line_length": float64(0),
	})
	assertConsecutive(t, collectNumberedLines(bodyBeforeTruncationFooter(resultText(t, tail))), 8, 9)

	omitted := callReadFile(t, reg, map[string]interface{}{"path": path})
	zero := callReadFile(t, reg, map[string]interface{}{"path": path, "max_lines": float64(0)})
	if omitted.IsError || zero.IsError {
		t.Fatal("omitted/0 max_lines must succeed")
	}
	om, _ := omitted.StructuredContent.(map[string]any)
	zm, _ := zero.StructuredContent.(map[string]any)
	if om["truncated"] != false || zm["truncated"] != false {
		t.Fatalf("small file omitted/0 must not truncate: omitted=%#v zero=%#v", om, zm)
	}
	if !hasNumberedLine(resultText(t, omitted), 9) || !hasNumberedLine(resultText(t, zero), 9) {
		t.Fatal("omitted/0 must return the full small file")
	}
}

func TestReadFileHandler_OmittedMaxLinesAutoCapNotUnlimited(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	total := autoTruncateLargeFileLines + 15
	path := writeNumberedFile(t, dir, "big.go", total, true)

	res := callReadFile(t, reg, map[string]interface{}{"path": path})
	if res.IsError {
		t.Fatalf("read_file error: %v", res.Content)
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["truncated"] != true || asIntMeta(m["end_line"]) != autoTruncateLargeFileLines {
		t.Fatalf("auto-cap structured: %#v", m)
	}
	body := bodyBeforeTruncationFooter(resultText(t, res))
	if hasNumberedLine(body, autoTruncateLargeFileLines+1) {
		t.Fatal("omitted max_lines must not become an unlimited read")
	}
}
