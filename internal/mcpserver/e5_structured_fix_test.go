package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

)

func TestE5_SearchFilenameHitNotEmpty(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "sample",
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["status"] == statusEmpty || asIntMeta(m["match_count"]) == 0 {
		t.Fatalf("filename hit reported empty: %#v text=%s", m, resultText(t, res))
	}
	if len(e5Maps(m["matches"])) == 0 {
		t.Fatalf("structured matches empty: %#v", m)
	}
}

func TestE5_SearchLiteralNoMatchesFoundStillHits(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "empty-message.txt")
	if err := os.WriteFile(path, []byte("No matches found\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"", "json"} {
		args := map[string]interface{}{
			"path": path, "pattern": "No matches found", "include_content": true,
		}
		if format != "" {
			args["output_format"] = format
		}
		res := callNamed(t, reg, context.Background(), "search_files", args)
		if res.IsError {
			t.Fatal(resultText(t, res))
		}
		m, _ := res.StructuredContent.(map[string]any)
		if m["status"] == statusEmpty || asIntMeta(m["match_count"]) == 0 {
			t.Fatalf("format=%q sentinel pattern empty: %#v text=%s", format, m, resultText(t, res))
		}
	}
}

func TestE5_SearchMaxResultsTruncated(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "two.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\nalpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": path, "pattern": "alpha", "include_content": true, "max_results": float64(1),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["truncated"] != true {
		t.Fatalf("expected truncated: %#v", m)
	}
	if asIntMeta(m["match_count"]) < 2 {
		t.Fatalf("match_count should be total before cap: %#v", m)
	}
	if len(e5Maps(m["matches"])) != 1 {
		t.Fatalf("returned matches should be capped: %#v", m)
	}
}

func TestE5_ReadLiteralTruncationMarkerNotTruncated(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "marker.txt")
	body := "[Truncated: this is literal file content]\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{"path": path})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["truncated"] != false {
		t.Fatalf("literal marker must not set truncated: %#v", m)
	}
	if m["continuation"] != nil {
		t.Fatalf("no continuation: %#v", m)
	}
	if m["content_hash"] == nil {
		t.Fatal("missing hash")
	}
}

func TestE5_ReadRangePastEOFActualEndLine(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\nalpha\nomega\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": path, "start_line": float64(2), "max_lines": float64(100),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if asIntMeta(m["start_line"]) != 2 || asIntMeta(m["end_line"]) != 4 {
		t.Fatalf("actual range want 2-4 got %#v", m)
	}
	if asIntMeta(m["total_lines"]) != 4 {
		t.Fatalf("total_lines=%v", m["total_lines"])
	}
	if m["continuation"] != nil {
		t.Fatalf("no more lines: %#v", m)
	}
}

func TestE5_ReadHeadContinuationNextLine(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\nalpha\nomega\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": path, "mode": "head", "max_lines": float64(1),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["truncated"] != true {
		t.Fatalf("head must be truncated: %#v", m)
	}
	cont, _ := m["continuation"].(map[string]any)
	if asIntMeta(cont["start_line"]) != 2 {
		t.Fatalf("continuation start_line want 2: %#v", m)
	}
}

func TestE5_ReadRangeHashMatchesSnapshot(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "h.txt")
	body := "abc\ndef\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	full := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{"path": path})
	rng := callNamed(t, reg, context.Background(), "read_file", map[string]interface{}{
		"path": path, "start_line": float64(2), "end_line": float64(2),
	})
	fm, _ := full.StructuredContent.(map[string]any)
	rm, _ := rng.StructuredContent.(map[string]any)
	if fm["content_hash"] != rm["content_hash"] {
		t.Fatalf("full hash %v range hash %v", fm["content_hash"], rm["content_hash"])
	}
	if asString(fm["content_hash"]) == "" {
		t.Fatal("missing hash")
	}
}

func asIntMeta(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
