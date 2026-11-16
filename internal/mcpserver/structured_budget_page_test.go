package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFitDiffStructured_JSONStaysWithinBudget is the unit check of the
// diff fit: a diff whose JSON does not fit the budget is cut, the cursor
// survives, and the payload agrees with the text.
func TestFitDiffStructured_JSONStaysWithinBudget(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "+line %04d with content that needs escaping \" here\n", i)
	}
	msg := b.String()
	bounded, cont, err := boundDiffText(msg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"status": statusOK, "path_a": "a.txt", "path_b": "b.txt",
		"identical": false, "added": 3000, "removed": 0, "message": bounded,
	}
	if cont != nil {
		payload["truncated"] = true
		payload["continuation"] = cont
		payload["status"] = statusTruncated
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= claudeSpillChars {
		t.Fatalf("fixture JSON %d did not exceed the budget; the test no longer covers the spill", len(raw))
	}
	fitted, text := fitDiffStructured(payload, msg, 0, 0, claudeSpillChars)
	raw, err = json.Marshal(fitted)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > claudeSpillChars {
		t.Fatalf("structured JSON %d exceeds %d", len(raw), claudeSpillChars)
	}
	if fitted["message"] != text {
		t.Fatal("the fitted message and the text disagree")
	}
	fittedCont, _ := fitted["continuation"].(map[string]any)
	if fittedCont == nil {
		t.Fatalf("the cut diff lost the continuation: %#v", fitted)
	}
	if fitted["truncated"] != true {
		t.Fatal("the cut diff is not marked truncated")
	}
	// The cursor points into the diff: the rest is still reachable.
	offset := int(asFloatArg(fittedCont["offset"]))
	if offset <= 1 {
		t.Fatalf("continuation offset %d does not point into the diff", offset)
	}
	rest, restCont, err := boundDiffText(msg, offset, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rest == text {
		t.Fatal("the continuation returns the same page")
	}
	if restCont == nil {
		t.Fatal("the second page lost its own continuation")
	}
}

// TestDiffFiles_StructuredStaysWithinBudget drives the real handler with a
// diff whose JSON does not fit the response budget. The text block was
// already capped; the JSON beside it repeated the whole diff. The cut keeps
// the cursor, so the rest of the diff is still reachable.
func TestDiffFiles_StructuredStaysWithinBudget(t *testing.T) {
	dir := t.TempDir()
	var a, b strings.Builder
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&a, "line %04d alpha beta gamma delta epsilon\n", i)
		fmt.Fprintf(&b, "line %04d omega beta gamma delta epsilon\n", i)
	}
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pathA, []byte(a.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "diff_files", map[string]any{"path_a": pathA, "path_b": pathB})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T", res.StructuredContent)
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > claudeSpillChars {
		t.Fatalf("structured JSON %d exceeds %d", len(raw), claudeSpillChars)
	}
	msg, _ := sc["message"].(string)
	if msg != resultText(t, res) {
		t.Fatal("the structured message and the text content block disagree")
	}
	if sc["truncated"] != true {
		t.Fatal("the cut diff is not marked truncated")
	}
	cont, _ := sc["continuation"].(map[string]any)
	if cont == nil {
		t.Fatalf("the cut diff lost the continuation: %#v", sc)
	}
	offset := asFloatArg(cont["offset"])
	if offset <= 1 {
		t.Fatalf("continuation offset %v does not point into the diff", offset)
	}
	// The cursor still reaches the rest of the diff.
	next := callPatchTool(t, reg, "diff_files", map[string]any{
		"path_a": pathA, "path_b": pathB, "offset": offset,
	})
	if next.IsError {
		t.Fatal(resultText(t, next))
	}
	if resultText(t, next) == resultText(t, res) {
		t.Fatal("the continuation returned the same page")
	}
	// The cut payload must satisfy the declared output schema.
	assertPayloadConforms(t, parseSchema(t, diffFilesOutputSchema), sc)
}

// TestListDirectory_StructuredStaysWithinBudget drives the real handler with
// a directory whose page does not fit the JSON budget. The listing is cut to
// a smaller page, so the text block and structuredContent.entries describe
// the SAME page and the cursor points at the next one.
func TestListDirectory_StructuredStaysWithinBudget(t *testing.T) {
	dir := t.TempDir()
	const n = 600
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%04d-with-a-long-name-to-fill-the-response-budget.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "list_directory", map[string]any{"path": dir})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T", res.StructuredContent)
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > claudeSpillChars {
		t.Fatalf("structured JSON %d exceeds %d", len(raw), claudeSpillChars)
	}
	msg, _ := sc["message"].(string)
	if msg != resultText(t, res) {
		t.Fatal("the structured message and the text content block disagree")
	}
	names := listingNames(t, res)
	if len(names) == 0 {
		t.Fatal("the page lost every entry")
	}
	if len(names) >= n {
		t.Fatalf("the page kept all %d entries; the test no longer covers the cut", len(names))
	}
	offset := continuationOffset(t, res)
	if int(offset) != len(names) {
		t.Fatalf("continuation offset %v does not match the %d entries of the page", offset, len(names))
	}
	// The next page continues where this one stopped.
	next := callPatchTool(t, reg, "list_directory", map[string]any{
		"path": dir, "offset": offset,
	})
	if next.IsError {
		t.Fatal(resultText(t, next))
	}
	nextNames := listingNames(t, next)
	if len(nextNames) == 0 {
		t.Fatal("the next page is empty")
	}
	if nextNames[0] == names[0] {
		t.Fatalf("the next page repeated the first entry %q", nextNames[0])
	}
	// The cut payload must satisfy the declared output schema.
	assertPayloadConforms(t, parseSchema(t, listDirectoryOutputSchema), sc)
}
