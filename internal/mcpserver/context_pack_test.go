package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func TestContextPack_FocusRanksAndOmitsSecrets(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\nfunc Alpha() {}\n")
	write("b.go", "package b\nfunc Beta() {}\n")
	write(".env", "PASSWORD=secret\n")
	res, err := reg.handlers["context_pack"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "context_pack",
		Arguments: map[string]interface{}{
			"path": dir, "query": "Beta", "focus_paths": []interface{}{"a.go", ".env"}, "include_git": false,
		},
	}})
	if err != nil || res.IsError {
		t.Fatalf("%v %v", err, res)
	}
	text := textBlock(t, res)
	if !strings.Contains(text, "Alpha") || !strings.Contains(text, "Beta") {
		t.Fatalf("missing symbols:\n%s", text)
	}
	if strings.Contains(text, "PASSWORD") || strings.Contains(text, ".env") {
		t.Fatalf("secret leaked:\n%s", text)
	}
	payload, _ := res.StructuredContent.(map[string]any)
	files, _ := payload["files"].([]map[string]any)
	if len(files) < 2 {
		t.Fatalf("files=%#v", payload["files"])
	}
	first, _ := files[0]["path"].(string)
	if !strings.HasSuffix(first, "a.go") {
		t.Fatalf("focus path should rank first, got %s", first)
	}
	if payload["git_omitted"] != "disabled" {
		t.Fatalf("git_omitted=%v", payload["git_omitted"])
	}
}

func TestContextPack_BudgetTruncates(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for i := 0; i < 8; i++ {
		name := filepath.Join(dir, string(rune('a'+i))+".go")
		body := "package p\nfunc Exported" + strings.Repeat("X", 40) + "() {}\n"
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := reg.handlers["context_pack"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "context_pack",
		Arguments: map[string]interface{}{
			"path": dir, "max_files": float64(8), "budget_chars": float64(500), "include_git": false,
			"focus_paths": []interface{}{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go"},
		},
	}})
	if err != nil || res.IsError {
		t.Fatalf("%v %v", err, res)
	}
	text := textBlock(t, res)
	payload, _ := res.StructuredContent.(map[string]any)
	if len(text) > 600 || payload["truncated"] != true || !strings.Contains(text, "budget_chars") {
		t.Fatalf("truncated=%v len=%d\n%s", payload["truncated"], len(text), text)
	}
}

// packCall runs context_pack and returns the text and the structured payload.
func packCall(t *testing.T, reg *toolRegistry, args map[string]interface{}) (string, map[string]any) {
	t.Helper()
	res, err := reg.handlers["context_pack"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "context_pack", Arguments: args,
	}})
	if err != nil || res.IsError {
		t.Fatalf("context_pack failed: %v %v", err, res)
	}
	payload, _ := res.StructuredContent.(map[string]any)
	if payload == nil {
		t.Fatal("context_pack returned no structured payload")
	}
	return textBlock(t, res), payload
}

// packCoverageOf returns the coverage map of a payload.
func packCoverageOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	cov, ok := payload["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("payload has no coverage map: %#v", payload)
	}
	return cov
}

// packFilesOf returns the files array of a payload.
func packFilesOf(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	files, ok := payload["files"].([]map[string]any)
	if !ok {
		t.Fatalf("payload has no files array: %#v", payload)
	}
	return files
}

// packWriteFile writes a file inside dir.
func packWriteFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// packFNV is the OCC hash contract of the server: FNV-1a, 8 lowercase hex.
func packFNV(raw []byte) string {
	h := fnv.New32a()
	h.Write(raw)
	return fmt.Sprintf("%08x", h.Sum32())
}

// TestContextPack_BudgetNeverExceeded checks the hard ceiling: the text
// fallback never exceeds budget_chars, whatever the caller asks for.
func TestContextPack_BudgetNeverExceeded(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	var body strings.Builder
	body.WriteString("package p\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "func Sym%d() { /* %s */ }\n", i, strings.Repeat("x", 50))
	}
	packWriteFile(t, dir, "big.go", body.String())
	for i := 0; i < 4; i++ {
		packWriteFile(t, dir, fmt.Sprintf("other%d.go", i), body.String())
	}

	for _, budget := range []int{500, 700, 1500, 4000, 12000, 32000} {
		text, payload := packCall(t, reg, map[string]interface{}{
			"path": dir, "query": "Sym", "budget_chars": float64(budget), "include_git": false,
		})
		if got := packRuneLen(text); got > budget {
			t.Fatalf("budget %d: text has %d characters\n%s", budget, got, text)
		}
		if got, _ := payload["budget_chars"].(int); got != budget {
			t.Fatalf("budget %d: payload reports %v", budget, payload["budget_chars"])
		}
		if got, _ := payload["text_chars"].(int); got != packRuneLen(text) {
			t.Fatalf("budget %d: text_chars %v != %d", budget, payload["text_chars"], packRuneLen(text))
		}
	}
}

// TestContextPack_SmallBudgetDegradesProgressively checks that a small
// budget removes bodies before it removes locators, and that every removal
// is counted.
func TestContextPack_SmallBudgetDegradesProgressively(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	var body strings.Builder
	body.WriteString("package p\n\nfunc Alpha() {\n")
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&body, "\tprintln(%d)\n", i)
	}
	body.WriteString("}\n")
	packWriteFile(t, dir, "a.go", body.String())

	rich, richPayload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Alpha", "budget_chars": float64(12000), "include_git": false,
	})
	if !strings.Contains(rich, "println") {
		t.Fatalf("generous budget should carry the body:\n%s", rich)
	}
	richFiles := packFilesOf(t, richPayload)
	if richFiles[0]["detail"] != packDetailSnippet {
		t.Fatalf("detail=%v", richFiles[0]["detail"])
	}

	poor, poorPayload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Alpha", "budget_chars": float64(500), "include_git": false,
	})
	if strings.Contains(poor, "println") {
		t.Fatalf("small budget must not carry the body:\n%s", poor)
	}
	poorFiles := packFilesOf(t, poorPayload)
	if len(poorFiles) == 0 {
		t.Fatalf("small budget dropped every locator:\n%s", poor)
	}
	if poorFiles[0]["detail"] == packDetailSnippet {
		t.Fatalf("detail should have degraded, got %v", poorFiles[0]["detail"])
	}
	if _, ok := poorFiles[0]["snippet"]; ok {
		t.Fatal("degraded entry must not keep a snippet body")
	}
	// The locator survives, so the caller can continue with read_file.
	if poorFiles[0]["start_line"] == nil || poorFiles[0]["symbol"] != "Alpha" {
		t.Fatalf("the locator did not survive: %#v", poorFiles[0])
	}
	cov := packCoverageOf(t, poorPayload)
	if poorPayload["truncated"] != true {
		t.Fatalf("truncated=%v", poorPayload["truncated"])
	}
	if cov["fragments_reduced"] == nil {
		t.Fatalf("the reduction was not counted: %#v", cov)
	}
}

// TestContextPack_UnicodeIsNeverSplit checks that a budget cut cannot break
// a UTF-8 sequence and that the budget counts characters, not bytes.
func TestContextPack_UnicodeIsNeverSplit(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	body := "package p\n\n// áéíóú ñ 中文 日本語 Ελληνικά\nfunc Acentuado() {\n\tprintln(\"áéíóú 中文 日本語\")\n}\n"
	packWriteFile(t, dir, "unicode.go", body)

	for _, budget := range []int{500, 800, 1200, 2000} {
		text, _ := packCall(t, reg, map[string]interface{}{
			"path": dir, "query": "Acentuado", "budget_chars": float64(budget), "include_git": false,
		})
		if !utf8.ValidString(text) {
			t.Fatalf("budget %d: invalid UTF-8 in the text", budget)
		}
		if strings.ContainsRune(text, utf8.RuneError) {
			t.Fatalf("budget %d: replacement rune in the text:\n%s", budget, text)
		}
		if got := packRuneLen(text); got > budget {
			t.Fatalf("budget %d: %d characters", budget, got)
		}
	}
}

// TestContextPack_TextAndStructuredAgree checks that the two views carry the
// same content: no body in one that is missing from the other.
func TestContextPack_TextAndStructuredAgree(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "a.go", "package p\n\nfunc Alpha() {\n\tprintln(\"alpha\")\n}\n")
	packWriteFile(t, dir, "b.go", "package q\n\nfunc Beta() {\n\tprintln(\"beta\")\n}\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Alpha", "include_git": false,
	})
	files := packFilesOf(t, payload)

	structuredLines := map[string]bool{}
	for _, f := range files {
		snippet, _ := f["snippet"].(string)
		for _, line := range strings.Split(snippet, "\n") {
			structuredLines[line] = true
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "  > ") {
			continue
		}
		body := strings.TrimPrefix(line, "  > ")
		if !structuredLines[body] {
			t.Fatalf("text carries a body the structured payload does not:\n%q", body)
		}
	}
	// Every file in the payload must be named in the text.
	for _, f := range files {
		p, _ := f["path"].(string)
		if !strings.Contains(text, p) {
			t.Fatalf("structured file %q missing from the text", p)
		}
	}
}

// TestContextPack_FindsSymbolBeyondFirstSix checks that a relevant symbol
// past the head of the file is still selected.
func TestContextPack_FindsSymbolBeyondFirstSix(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	var body strings.Builder
	body.WriteString("package p\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&body, "func Sym%d() {}\n", i)
	}
	body.WriteString("func TargetSymbol() {}\n")
	packWriteFile(t, dir, "many.go", body.String())

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "TargetSymbol", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) == 0 {
		t.Fatalf("no files selected:\n%s", text)
	}
	first := files[0]
	if first["symbol"] != "TargetSymbol" {
		t.Fatalf("symbol=%v", first["symbol"])
	}
	if first["range_status"] != packRangeExact {
		t.Fatalf("range_status=%v", first["range_status"])
	}
	if first["range_source"] != packSourceGoAST {
		t.Fatalf("range_source=%v", first["range_source"])
	}
	if first["line"] != 14 {
		t.Fatalf("line=%v want 14", first["line"])
	}
	if !strings.Contains(text, "TargetSymbol") {
		t.Fatalf("symbol missing from the text:\n%s", text)
	}
}

// TestContextPack_DefinitionBeatsMention checks that a file defining the
// queried identifier outranks a file that only mentions it.
func TestContextPack_DefinitionBeatsMention(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "definition.go", "package p\n\nfunc TargetSymbol() {}\n")
	packWriteFile(t, dir, "mention.go", "package q\n\n// see TargetSymbol\nfunc Other() {}\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "TargetSymbol", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) < 2 {
		t.Fatalf("expected two files:\n%s", text)
	}
	first, _ := files[0]["path"].(string)
	if !strings.HasSuffix(first, "definition.go") {
		t.Fatalf("definition must rank first, got %s", first)
	}
	if files[0]["symbol"] != "TargetSymbol" {
		t.Fatalf("symbol=%v", files[0]["symbol"])
	}
	// The mention is a window, not a definition.
	if files[1]["range_source"] != packSourceWindow {
		t.Fatalf("mention range_source=%v", files[1]["range_source"])
	}
	if files[1]["range_status"] != packRangeApproximate {
		t.Fatalf("mention range_status=%v", files[1]["range_status"])
	}
}

// TestContextPack_NoSymbolsFallsBackToWindow checks the textual fallback when
// no analyzer can describe the file.
func TestContextPack_NoSymbolsFallsBackToWindow(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "notes.txt", "line one\nTargetSymbol appears here\nline three\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "TargetSymbol", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) == 0 {
		t.Fatalf("no files selected:\n%s", text)
	}
	if files[0]["range_source"] != packSourceWindow {
		t.Fatalf("range_source=%v", files[0]["range_source"])
	}
	if files[0]["range_status"] != packRangeApproximate {
		t.Fatalf("range_status=%v", files[0]["range_status"])
	}
	if _, ok := files[0]["end_line"]; !ok {
		t.Fatal("a window still has an end line")
	}
	if !strings.Contains(text, "TargetSymbol appears here") {
		t.Fatalf("window body missing:\n%s", text)
	}
}

// TestContextPack_OverlappingRangesMerged checks that two matches that
// produce overlapping windows do not produce two overlapping fragments.
func TestContextPack_OverlappingRangesMerged(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "overlap.txt", "line1\nNeedle here\nline3\nNeedle again\nline5\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Needle", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d:\n%s", len(files), text)
	}
	cov := packCoverageOf(t, payload)
	merged, _ := cov["ranges_merged"].(int)
	if merged == 0 {
		t.Fatalf("expected the overlapping ranges to be merged: %#v", cov)
	}
	// One fragment, not two: the body must not be repeated.
	if _, ok := files[0]["fragments"]; ok {
		t.Fatalf("a folded range must not leave a second fragment: %#v", files[0])
	}
	// The range covers both matches.
	start, _ := files[0]["start_line"].(int)
	end, _ := files[0]["end_line"].(int)
	if start > 2 || end < 4 {
		t.Fatalf("merged range=%d-%d does not cover both matches", start, end)
	}
	// A window is never presented as an exact definition.
	if files[0]["range_status"] == packRangeExact {
		t.Fatalf("a window must not claim to be exact: %#v", files[0])
	}
}

// TestContextPack_DuplicateRangesAreFolded checks that two matches inside
// one symbol yield a single fragment and that the fold is counted.
func TestContextPack_DuplicateRangesAreFolded(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "overlap.go", "package p\n\nfunc Shared() {\n\tTarget := 1\n\tprintln(Target)\n}\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Target", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d:\n%s", len(files), text)
	}
	cov := packCoverageOf(t, payload)
	merged, _ := cov["ranges_merged"].(int)
	if merged == 0 {
		t.Fatalf("expected the duplicate range to be folded: %#v", cov)
	}
	// A duplicate of one symbol keeps the analyzer's exact range.
	if files[0]["range_status"] != packRangeExact {
		t.Fatalf("range_status=%v", files[0]["range_status"])
	}
}

// TestContextPack_SearchTruncationIsPropagated checks that a capped candidate
// search is reported instead of being read as complete coverage.
func TestContextPack_SearchTruncationIsPropagated(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	var body strings.Builder
	body.WriteString("package p\n")
	for i := 0; i < 260; i++ {
		body.WriteString("// Needle line\n")
	}
	packWriteFile(t, dir, "many.go", body.String())

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Needle", "include_git": false,
	})
	cov := packCoverageOf(t, payload)
	if cov["search_truncated"] != true {
		t.Fatalf("search truncation not reported: %#v\n%s", cov, text)
	}
	notes, _ := cov["notes"].([]string)
	if !packNotesMention(notes, "truncated") {
		t.Fatalf("notes do not mention the truncation: %#v", notes)
	}
}

// packNotesMention reports whether any note mentions the given word.
func packNotesMention(notes []string, word string) bool {
	for _, n := range notes {
		if strings.Contains(n, word) {
			return true
		}
	}
	return false
}

// TestContextPack_HashMatchesSnapshot checks that the published hash comes
// from the same bytes the fragments were cut from, and that it is the OCC
// token the rest of the server uses.
func TestContextPack_HashMatchesSnapshot(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	raw := []byte("package p\n\nfunc Alpha() {\n\tprintln(\"alpha\")\n}\n")
	packWriteFile(t, dir, "a.go", string(raw))

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Alpha", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) == 0 {
		t.Fatalf("no files selected:\n%s", text)
	}
	got, _ := files[0]["content_hash"].(string)
	if want := packFNV(raw); got != want {
		t.Fatalf("content_hash=%q want %q", got, want)
	}
	if len(got) != 8 {
		t.Fatalf("content_hash %q is not 8 hex chars", got)
	}
}

// TestContextPack_HashMatchesReadFile checks that the hash is the same token
// read_file publishes, so it can be used as expected_hash.
func TestContextPack_HashMatchesReadFile(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "a.go", "package p\n\nfunc Alpha() {}\n")

	_, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Alpha", "include_git": false,
	})
	files := packFilesOf(t, payload)
	packHash, _ := files[0]["content_hash"].(string)

	res, err := reg.handlers["read_file"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "read_file", Arguments: map[string]interface{}{"path": files[0]["path"]},
	}})
	if err != nil || res.IsError {
		t.Fatalf("read_file failed: %v %v", err, res)
	}
	readPayload, _ := res.StructuredContent.(map[string]any)
	readHash, _ := readPayload["content_hash"].(string)
	if packHash == "" || packHash != readHash {
		t.Fatalf("pack hash %q != read_file hash %q", packHash, readHash)
	}
}

// TestContextPack_DeterministicOrder checks that the same tree yields the
// same pack, and that the focus order decides the tie.
func TestContextPack_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "a.go", "package p\n\nfunc Alpha() {}\n")
	packWriteFile(t, dir, "b.go", "package q\n\nfunc Beta() {}\n")
	packWriteFile(t, dir, "c.go", "package r\n\nfunc Gamma() {}\n")

	args := map[string]interface{}{
		"path": dir, "query": "Alpha", "include_git": false,
		"focus_paths": []interface{}{"c.go", "b.go"},
	}
	first, firstPayload := packCall(t, reg, args)
	second, secondPayload := packCall(t, reg, args)
	if first != second {
		t.Fatalf("text is not deterministic:\n%s\n---\n%s", first, second)
	}
	a, _ := json.Marshal(firstPayload)
	b, _ := json.Marshal(secondPayload)
	if string(a) != string(b) {
		t.Fatalf("payload is not deterministic:\n%s\n---\n%s", a, b)
	}
	files := packFilesOf(t, firstPayload)
	if len(files) < 2 {
		t.Fatalf("expected at least two files: %#v", files)
	}
	// focus_paths order decides the tie between equally relevant files.
	firstPath, _ := files[0]["path"].(string)
	if !strings.HasSuffix(firstPath, "c.go") {
		t.Fatalf("focus order not honoured, got %s", firstPath)
	}
}

// TestContextPack_RegexQueryStaysRegex checks that a regex query is not
// reinterpreted as an identifier.
func TestContextPack_RegexQueryStaysRegex(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "a.go", "package p\n\nfunc Alpha() {}\nfunc Beta() {}\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Alph|Bet", "include_git": false,
	})
	if !strings.Contains(text, "func Alpha") && !strings.Contains(text, "func Beta") {
		t.Fatalf("regex query found nothing:\n%s", text)
	}
	cov := packCoverageOf(t, payload)
	if cov["query_matched"] != true {
		t.Fatalf("query_matched=%v", cov["query_matched"])
	}
	// A regex is not an identifier, so no definition ranking is claimed.
	files := packFilesOf(t, payload)
	for _, f := range files {
		if f["range_source"] == packSourceWindow {
			t.Fatalf("a regex query must not produce window fallbacks: %#v", f)
		}
	}
}

// TestContextPack_NoMatchesIsExplicit checks that an empty result says so.
func TestContextPack_NoMatchesIsExplicit(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "a.go", "package p\n\nfunc Alpha() {}\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "NoSuchSymbolAnywhere", "include_git": false,
	})
	if !strings.Contains(text, "no matches for query") {
		t.Fatalf("missing the no-match note:\n%s", text)
	}
	cov := packCoverageOf(t, payload)
	if cov["query_matched"] != false {
		t.Fatalf("query_matched=%v", cov["query_matched"])
	}
	if len(packFilesOf(t, payload)) != 0 {
		t.Fatalf("unexpected files: %#v", payload["files"])
	}
}

// TestContextPack_AccessRestrictions checks that the new read paths honour
// allowed-paths, the file policy and secret protection, and that the
// coverage metadata never names a file the caller was not allowed to see.
func TestContextPack_AccessRestrictions(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	reg := newHelpTestRegistry(t, dir)

	packWriteFile(t, dir, "visible.go", "package p\n\nfunc Visible() {}\n")
	packWriteFile(t, dir, ".env", "TOKEN=hidden\n")
	packWriteFile(t, outside, "outside.go", "package q\n\nfunc Outside() {}\n")
	cfg := packWriteFile(t, dir, "policy.json", `{
	  "version": 1,
	  "rules": [{"pattern": "hidden.go", "level": "hidden"}]
	}`)
	packWriteFile(t, dir, "hidden.go", "package r\n\nfunc Hidden() {}\n")

	policy, err := core.LoadFilePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.PinConfig(cfg); err != nil {
		t.Fatal(err)
	}
	reg.engine.SetFilePolicy(policy)

	// The caller names the files it expects; the ones it may not see are
	// counted, never named.
	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "func", "include_git": false,
		"focus_paths": []interface{}{"hidden.go", ".env", filepath.Join(outside, "outside.go"), "visible.go"},
	})
	if strings.Contains(text, "Outside") || strings.Contains(text, "outside.go") {
		t.Fatalf("a file outside allowed-paths leaked:\n%s", text)
	}
	if strings.Contains(text, "hidden.go") || strings.Contains(text, "Hidden") {
		t.Fatalf("a policy-hidden file leaked:\n%s", text)
	}
	if strings.Contains(text, ".env") || strings.Contains(text, "TOKEN") {
		t.Fatalf("a secret file leaked:\n%s", text)
	}
	cov := packCoverageOf(t, payload)
	denied, _ := cov["files_denied"].(int)
	if denied != 3 {
		t.Fatalf("files_denied=%d want 3: %#v", denied, cov)
	}
	// The coverage metadata carries counts, never the denied paths.
	notes, _ := cov["notes"].([]string)
	for _, n := range notes {
		if strings.Contains(n, "outside.go") || strings.Contains(n, "hidden.go") || strings.Contains(n, ".env") {
			t.Fatalf("coverage metadata names a denied path: %q", n)
		}
	}
}

// TestContextPack_VeryLongPathStillFitsBudget checks the last-resort path:
// when the envelope alone is larger than the budget the text is cut on a
// rune boundary rather than allowed to overflow.
func TestContextPack_VeryLongPathStillFitsBudget(t *testing.T) {
	root := t.TempDir()
	deep := root
	for i := 0; i < 12; i++ {
		deep = filepath.Join(deep, "a-very-long-directory-segment-name")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	packWriteFile(t, deep, "a.go", "package p\n\nfunc Alpha() {}\n")

	reg := newHelpTestRegistry(t, root)
	text, payload := packCall(t, reg, map[string]interface{}{
		"path": deep, "query": "Alpha", "budget_chars": float64(500), "include_git": false,
	})
	if got := packRuneLen(text); got > 500 {
		t.Fatalf("text has %d characters, budget 500:\n%s", got, text)
	}
	if !utf8.ValidString(text) {
		t.Fatal("the text is not valid UTF-8")
	}
	if payload["truncated"] != true {
		t.Fatalf("truncated=%v", payload["truncated"])
	}
}

// TestContextPack_ExactRangeSurvivesAdjacentWindow covers the case found in
// a real repository: the identifier also appears in the doc comment above
// the definition, so a textual window overlaps the analyzer range. The
// analyzer range must survive — its bounds are what replace_range and
// analyze_code agree on.
func TestContextPack_ExactRangeSurvivesAdjacentWindow(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "feedback.go", `package core

func Other() {
	return
}

// RecordReadHash records the content_hash the session observed for a file.
func RecordReadHash(path, hash string) {
	if hash == "" {
		return
	}
}
`)

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "RecordReadHash", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) == 0 {
		t.Fatalf("no files selected:\n%s", text)
	}
	if files[0]["range_status"] != packRangeExact {
		t.Fatalf("range_status=%v, the analyzer range must survive", files[0]["range_status"])
	}
	if files[0]["range_source"] != packSourceGoAST {
		t.Fatalf("range_source=%v", files[0]["range_source"])
	}
	if files[0]["start_line"] != 8 {
		t.Fatalf("start_line=%v want 8 (the func keyword)", files[0]["start_line"])
	}
	if files[0]["end_line"] != 12 {
		t.Fatalf("end_line=%v want 12", files[0]["end_line"])
	}
	// The doc comment is not part of the Go AST range of the declaration.
	if strings.Contains(text, "records the content_hash the session observed") {
		t.Fatalf("the window widened the exact range:\n%s", text)
	}
	// The fold is still counted.
	cov := packCoverageOf(t, payload)
	if merged, _ := cov["ranges_merged"].(int); merged == 0 {
		t.Fatalf("the fold was not counted: %#v", cov)
	}
}

// TestContextPack_DefinitionSurvivesTruncatedSearch covers the case found in
// a real repository: the general search is ordered by path, so its cap can be
// spent entirely on a prose file and the source file that defines the
// identifier is never a candidate. The source-only scan must still reach it.
func TestContextPack_DefinitionSurvivesTruncatedSearch(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)

	var prose strings.Builder
	for i := 0; i < 260; i++ {
		prose.WriteString("the Widget mention\n")
	}
	packWriteFile(t, dir, "a-prose.md", prose.String())
	packWriteFile(t, dir, "z-source.go", "package p\n\ntype Widget struct {\n\tName string\n}\n")

	text, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Widget", "include_git": false,
	})
	files := packFilesOf(t, payload)
	if len(files) == 0 {
		t.Fatalf("no files selected:\n%s", text)
	}
	first, _ := files[0]["path"].(string)
	if !strings.HasSuffix(first, "z-source.go") {
		t.Fatalf("the definition file must rank first, got %s\n%s", first, text)
	}
	if files[0]["symbol"] != "Widget" {
		t.Fatalf("symbol=%v", files[0]["symbol"])
	}
	if files[0]["range_status"] != packRangeExact {
		t.Fatalf("range_status=%v", files[0]["range_status"])
	}
	cov := packCoverageOf(t, payload)
	if cov["search_truncated"] != true {
		t.Fatalf("the truncation was not reported: %#v", cov)
	}
	if src, _ := cov["source_candidates"].(int); src == 0 {
		t.Fatalf("the source-file scan was not credited: %#v", cov)
	}
}

// TestContextPack_StructuredPayloadIsValidJSON checks the structured payload
// survives as JSON, including with Unicode content.
func TestContextPack_StructuredPayloadIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	packWriteFile(t, dir, "unicode.go", "package p\n\n// áéí 中文\nfunc Acentuado() {\n\tprintln(\"áéí 中文\")\n}\n")

	_, payload := packCall(t, reg, map[string]interface{}{
		"path": dir, "query": "Acentuado", "include_git": false,
	})
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("payload is not serializable: %v", err)
	}
	if !utf8.Valid(raw) {
		t.Fatal("payload is not valid UTF-8")
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
}
