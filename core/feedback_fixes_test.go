package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedLineCount_RepeatedEditsCountOnce(t *testing.T) {
	before := "alpha beta\nsecond\nthird\n"
	after := "ALPHA BETA\nSECOND\nthird\n"
	if got := changedLineCount(before, after); got != 2 {
		t.Fatalf("changed lines=%d, want 2", got)
	}
	if got := changedLineCount(before, before); got != 0 {
		t.Fatalf("unchanged=%d", got)
	}
}

func TestSearchContext_MergesOverlapAndKeepsNumbers(t *testing.T) {
	matches := []SearchMatch{
		{
			File: "a.razor", LineNumber: 435, Line: "hit-a",
			Context:            []string{"433", "434", "436", "437"},
			ContextLineNumbers: []int{433, 434, 436, 437},
			ContextSplit:       true,
		},
		{
			File: "a.razor", LineNumber: 437, Line: "hit-b",
			Context:            []string{"435", "436", "438"},
			ContextLineNumbers: []int{435, 436, 438},
			ContextSplit:       true,
		},
	}
	out := formatSearchGrouped(matches, true, 2)
	if strings.Count(out, "436 |") != 1 || strings.Count(out, "435 |") != 1 {
		t.Fatalf("overlapping lines repeated:\n%s", out)
	}
	if strings.Contains(out, "443 |") || !strings.Contains(out, ">435 |") || !strings.Contains(out, ">437 |") {
		t.Fatalf("line numbers:\n%s", out)
	}
}

func TestParseRipgrep_KeepsGapLineNumbers(t *testing.T) {
	raw := []byte(`{"type":"begin","data":{"path":{"text":"a.go"}}}
{"type":"context","data":{"path":{"text":"a.go"},"lines":{"text":"433\n"},"line_number":433}}
{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"435\n"},"line_number":435,"submatches":[{"start":0,"end":1}]}}
{"type":"context","data":{"path":{"text":"a.go"},"lines":{"text":"437\n"},"line_number":437}}
{"type":"end","data":{"path":{"text":"a.go"}}}
`)
	matches, err := parseRipgrepOutput(raw, true, 2)
	if err != nil || len(matches) != 1 {
		t.Fatalf("%v %#v", err, matches)
	}
	out := formatNumberedContext(matches[0], 2, true)
	if strings.Contains(out, "434 |") || !strings.Contains(out, "433 |") || !strings.Contains(out, ">435 |") || !strings.Contains(out, "437 |") {
		t.Fatalf("gap renumbered:\n%s", out)
	}
}

func TestPipelineRegex_FileStemOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AvisosOrdersModal.razor")
	if err := os.WriteFile(path, []byte("<div class=\"modal\">\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(dir)
	defer engine.Close()
	res, err := NewPipelineExecutor(engine).Execute(context.Background(), PipelineRequest{
		Name: "stem",
		Steps: []PipelineStep{{
			ID: "rx", Action: "regex_transform",
			Params: map[string]interface{}{
				"files": []interface{}{path},
				"patterns": []interface{}{map[string]interface{}{
					"pattern":     `(<div )`,
					"replacement": `${1}data-razor="{{file.stem}}" `,
				}},
			},
		}},
	})
	if err != nil || !res.Success {
		t.Fatalf("%v %+v", err, res)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "<div data-razor=\"AvisosOrdersModal\" class=\"modal\">\n"
	if string(got) != want {
		t.Fatalf("got %q", got)
	}
}
