package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestFitSearchStructured_JSONStaysWithinBudget(t *testing.T) {
	line := strings.Repeat(`say "hello" `, 80)
	matches := make([]core.SearchMatch, 80)
	var text strings.Builder
	for i := range matches {
		matches[i] = core.SearchMatch{File: "C:\\long\\path\\file.go", LineNumber: i + 1, Line: line}
		text.WriteString(line)
		text.WriteByte('\n')
	}
	out := core.SearchOutcome{
		Text: text.String(), Matches: matches, MatchCount: len(matches),
		Offset: 0, ExactTotal: true,
	}
	out = shrinkSearchToSpill(out, claudeSpillChars)
	if len(out.Text) > claudeSpillChars {
		t.Fatalf("text %d exceeds budget before structured fit", len(out.Text))
	}
	textBefore := out.Text
	sc := searchStructuredFromOutcome(out, map[string]any{
		"path": "C:\\long", "pattern": "hello", "include_content": true,
		"offset": 0, "max_results": 80, "searched": "content",
	}, len(out.Text) < len(text.String()))
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= claudeSpillChars {
		t.Fatal("fixture did not exceed the structured budget; the test no longer covers the spill")
	}
	sc = fitSearchStructured(sc, claudeSpillChars)
	raw, err = json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > claudeSpillChars {
		t.Fatalf("structured JSON %d exceeds %d", len(raw), claudeSpillChars)
	}
	if out.Text != textBefore {
		t.Fatal("fitting structuredContent rewrote the already-capped text")
	}
	if sc["continuation"] == nil {
		t.Fatal("cut structured search lost the continuation hint")
	}
	if sc["status"] != statusTruncated {
		t.Fatalf("status %v", sc["status"])
	}
}

func TestFitReadStructured_DropsDuplicateBody(t *testing.T) {
	body := strings.Repeat("line\n", 4000)
	files := []map[string]any{
		{"path": "a.txt", "content": body, "content_hash": "abc"},
		{"path": "b.txt", "content": body, "content_hash": "def"},
	}
	combined := "=== a.txt ===\n" + body + "\n=== b.txt ===\n" + body
	sc := readStructured("", combined, "", false, 0, 0, files)
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= claudeSpillChars {
		t.Fatalf("fixture JSON %d did not exceed the budget", len(raw))
	}
	sc = fitReadStructured(sc, claudeSpillChars)
	raw, err = json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > claudeSpillChars {
		t.Fatalf("structured JSON %d exceeds %d", len(raw), claudeSpillChars)
	}
	if _, dup := sc["content"]; dup && len(sc["content"].(string)) > 1000 {
		t.Fatal("combined body still duplicated in structuredContent.content")
	}
	// The cut payload must still satisfy the declared output schema. A client
	// that validates it rejects the whole result otherwise (content is required).
	assertPayloadConforms(t, parseSchema(t, readFileOutputSchema), sc)
	kept := 0
	for _, f := range files {
		if _, ok := f["content"]; ok {
			kept++
		}
	}
	if kept == 0 {
		t.Fatal("every file body was dropped; structured mode would have nothing to read")
	}
	if sc["continuation"] == nil && sc["truncated"] == true {
		t.Fatal("truncated batch lost the continuation hint")
	}
}
