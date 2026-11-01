package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mcp/filesystem-ultra/core"
)

func TestLongLineSpillRecoversEveryByte(t *testing.T) {
	line := strings.Repeat("€", 40000) // 120000 bytes, 3 per rune
	if !utf8.ValidString(line) {
		t.Fatal("fixture")
	}
	var got strings.Builder
	start, off := 1, 0
	for page := 0; page < 6; page++ {
		p := projectReadCursor(line, "wide.txt", start, 0, 0, "all", 0, off, 0)
		if p.Err != "" {
			t.Fatal(p.Err)
		}
		if len(p.Text) > claudeSpillChars {
			t.Fatalf("page %d is %d bytes", page, len(p.Text))
		}
		if !utf8.ValidString(p.Text) {
			t.Fatalf("page %d is not valid UTF-8", page)
		}
		got.WriteString(stripProjectionFooter(p.Text))
		if !p.Truncated {
			if p.ResumeOffset != 0 {
				t.Fatalf("complete page still has byte cursor: %+v", p)
			}
			break
		}
		if p.ResumeOffset == 0 || p.ContinueAt != 1 {
			t.Fatalf("cut advanced past the line: %+v", p)
		}
		if p.ResumeOffset <= off {
			t.Fatalf("cursor did not advance: %d -> %d", off, p.ResumeOffset)
		}
		start, off = p.ContinueAt, p.ResumeOffset
		if page == 5 {
			t.Fatal("tail still unread")
		}
	}
	if got.String() != line {
		t.Fatalf("recovered %d bytes, want %d", got.Len(), len(line))
	}
}

func TestNewlineSpillStillAdvancesByLine(t *testing.T) {
	var body strings.Builder
	piece := strings.Repeat("a", 200)
	for i := 0; i < 400; i++ {
		body.WriteString(piece)
		body.WriteByte('\n')
	}
	p := projectRead(body.String(), "lines.txt", 0, 0, 0, "all", 0)
	if !p.Truncated || p.ResumeOffset != 0 || p.ContinueAt <= 1 {
		t.Fatalf("newline cut should advance by line: %+v", p)
	}
	if len(p.Text) > claudeSpillChars {
		t.Fatalf("spill %d", len(p.Text))
	}
}

func TestReadFileHandlerRecoversLongLine(t *testing.T) {
	for _, mode := range []string{core.ResponseModeCompat, core.ResponseModeStructured} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			reg := newHelpTestRegistry(t, dir)
			reg.engine.GetConfig().ResponseMode = mode
			path := filepath.Join(dir, "one.txt")
			line := strings.Repeat("€", 40000)
			if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
				t.Fatal(err)
			}
			var got strings.Builder
			args := map[string]any{"path": path}
			for page := 0; page < 6; page++ {
				res := callPatchTool(t, reg, "read_file", args)
				if res.IsError {
					t.Fatal(resultText(t, res))
				}
				sc := res.StructuredContent.(map[string]any)
				body, _ := sc["content"].(string)
				if len(resultText(t, res)) > claudeSpillChars && mode == core.ResponseModeCompat {
					t.Fatalf("text block %d", len(resultText(t, res)))
				}
				if !utf8.ValidString(body) {
					t.Fatal("structured content is not valid UTF-8")
				}
				got.WriteString(stripProjectionFooter(body))
				if sc["truncated"] != true {
					break
				}
				cont, _ := sc["continuation"].(map[string]any)
				if cont == nil || cont["byte_offset"] == nil {
					t.Fatalf("missing byte cursor: %#v", sc["continuation"])
				}
				args = map[string]any{"path": path, "start_line": asFloatArg(cont["start_line"]), "byte_offset": asFloatArg(cont["byte_offset"])}
				if page == 5 {
					t.Fatal("tail still unread")
				}
			}
			if got.String() != line {
				t.Fatalf("recovered %d bytes, want %d", got.Len(), len(line))
			}
		})
	}
}

func TestBatchReadStaysWithinTextBudget(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	line := strings.Repeat("x", 40000)
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(c, []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := callPatchTool(t, reg, "read_file", map[string]any{"paths": []any{a, b, c}})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if n := len(resultText(t, res)); n > claudeSpillChars {
		t.Fatalf("batch text %d", n)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["truncated"] != true {
		t.Fatalf("batch should continue: %#v", sc["continuation"])
	}
	raw, err := json.Marshal(sc["files"])
	if err != nil {
		t.Fatal(err)
	}
	var files []map[string]any
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files: %#v", files)
	}
	if files[0]["content"] == nil || files[0]["content"] == "" {
		t.Fatal("first file body missing")
	}
	for i, f := range files[1:] {
		cont, _ := f["continuation"].(map[string]any)
		if cont["kind"] != "batch_resume" || f["content"] != nil {
			t.Fatalf("file %d leaked after the budget cut: %#v", i+1, f)
		}
	}
}

func TestSearchContinuationHintIsNotAReasonCode(t *testing.T) {
	hint := core.SearchContinuationHint(128, 40, "max_results")
	if strings.Contains(hint, "max_results Continue") || strings.HasPrefix(hint, "max_results") {
		t.Fatalf("malformed hint: %s", hint)
	}
	if !strings.Contains(hint, "offset:128") {
		t.Fatalf("hint lost the cursor: %s", hint)
	}
}

func TestDiffFilesSpillHasOffset(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	var left, right strings.Builder
	for i := 0; i < 800; i++ {
		left.WriteString(strings.Repeat("a", 80))
		left.WriteByte('\n')
		right.WriteString(strings.Repeat("b", 80))
		right.WriteByte('\n')
	}
	if err := os.WriteFile(a, []byte(left.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(right.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res := callPatchTool(t, reg, "diff_files", map[string]any{"path_a": a, "path_b": b})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if n := len(resultText(t, res)); n > claudeSpillChars {
		t.Fatalf("diff text %d", n)
	}
	sc := res.StructuredContent.(map[string]any)
	cont, _ := sc["continuation"].(map[string]any)
	if cont == nil || cont["offset"] == nil {
		t.Fatalf("diff continuation: %#v", sc["continuation"])
	}
}

func TestListDirectoryOffsetIsSortedAndRecoverable(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for _, name := range []string{"c.txt", "a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first := callPatchTool(t, reg, "list_directory", map[string]any{"path": dir, "output_format": "json"})
	if first.IsError {
		t.Fatal(resultText(t, first))
	}
	sc := first.StructuredContent.(map[string]any)
	raw, err := json.Marshal(sc["entries"])
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || entries[0]["name"] != "a.txt" {
		t.Fatalf("first sorted name: %#v", entries)
	}
}

func asFloatArg(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		b, _ := json.Marshal(n)
		var f float64
		_ = json.Unmarshal(b, &f)
		return f
	}
}
