package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

func inspectTestCall(t *testing.T, reg *toolRegistry, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	handler, ok := reg.handlers[tool]
	if !ok {
		t.Fatalf("missing tool %s", tool)
	}
	result, err := handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool, Arguments: wire}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func inspectTestPayload(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, result))
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
func inspectTestError(t *testing.T, result *mcp.CallToolResult, code string) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected %s; got success: %#v", code, result.StructuredContent)
	}
	var envelope struct {
		Error struct {
			Code      string
			Retryable bool
			Details   map[string]any
		}
	}
	if err := json.Unmarshal([]byte(resultText(t, result)), &envelope); err != nil {
		t.Fatalf("non-envelope error: %v %s", err, resultText(t, result))
	}
	if envelope.Error.Code != code {
		t.Fatalf("code=%s want %s: %s", envelope.Error.Code, code, resultText(t, result))
	}
	if code == "VALIDATION" && envelope.Error.Retryable {
		t.Fatal("validation is retryable")
	}
}
func inspectTestFile(t *testing.T, dir, name string, raw []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectJSONDocumentExamples(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	source := `{"linies":[{"codArticulo":"A","coincident":false,"cantidadReal":2,"cantidadSimulada":1,"precioReal":0.1,"precioSimulada":0.2,"categoriesAmbDiferencies":["cantidad"],"numOrden":1,"cantidad":2,"precio":0.1,"descSubArticulo":"cafè"}],"problemes":[{"mena":"text","ocurrencies":2},{"mena":"number","ocurrencies":1}]}`
	path := inspectTestFile(t, dir, "fixture.json", []byte(source))
	document, err := os.ReadFile(filepath.Join("..", "..", "doc", "json-query-file-bytes-file-hash-2026-10-20.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```json\\s*\\n(.*?)\\n```").FindAllSubmatch(document, -1)
	examples := 0
	for _, block := range blocks {
		var args map[string]any
		if json.Unmarshal(block[1], &args) != nil {
			continue
		}
		if _, ok := args["query"]; !ok {
			continue
		}
		examples++
		args["path"] = path
		payload := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", args))
		if payload["type"] != "array" || payload["returned_count"].(float64) < 1 {
			t.Fatalf("example %v returned %#v", args["query"], payload)
		}
	}
	if examples != 4 {
		t.Fatalf("executed %d document examples, want 4", examples)
	}
	for _, query := range []string{"problemes[?mena == 'text']", "sort_by(problemes, &ocurrencies)"} {
		payload := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "query": query}))
		rows := payload["result"].([]any)
		if strings.HasPrefix(query, "sort_by") && rows[0].(map[string]any)["ocurrencies"] != float64(1) {
			t.Fatal("sort order")
		}
		if strings.HasPrefix(query, "problemes[") && len(rows) != 1 {
			t.Fatal("filter did not select exactly one problem")
		}
	}
}

func TestInspectJSONPaginationAndOCC(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := inspectTestFile(t, dir, "pages.json", []byte("\xef\xbb\xbf[{\"b\":1},{\"a\":2},{\"z\":3}]"))
	first := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "limit": 1, "output_format": "table"}))
	if first["content_hash"] != contentHashBytes([]byte("\xef\xbb\xbf[{\"b\":1},{\"a\":2},{\"z\":3}]")) {
		t.Fatal("hash did not include BOM")
	}
	if first["total_count"] != float64(3) || first["returned_count"] != float64(1) || first["truncated"] != true {
		t.Fatalf("bad counts %#v", first)
	}
	if !reflect.DeepEqual(first["columns"], []any{"a", "b", "z"}) {
		t.Fatalf("unstable columns: %#v", first["columns"])
	}
	next := first["continuation"].(map[string]any)
	second := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", next))
	if !reflect.DeepEqual(first["columns"], second["columns"]) {
		t.Fatal("columns changed between pages")
	}
	if second["result"].([]any)[0].(map[string]any)["a"] != float64(2) {
		t.Fatal("wrong page")
	}
	if err := os.WriteFile(path, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	inspectTestError(t, inspectTestCall(t, reg, "json_query", next), "OCC_MISMATCH")
}

func TestInspectJSONValidationAndTypes(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	cases := []struct{ name, source, query, code, kind string }{
		{"null", `null`, "", "", "null"}, {"empty", `[]`, "", "", "array"}, {"missing", `{}`, "missing", "", "null"},
		{"string", `"cafè"`, "", "", "string"}, {"decimal", `0.1`, "", "", "number"},
		{"safe", `9007199254740991`, "", "", "number"}, {"unsafe", `9007199254740992`, "", "VALIDATION", ""},
		{"unsafe_exponent", `9.007199254740992e15`, "", "VALIDATION", ""}, {"overflow", `1e999`, "", "VALIDATION", ""},
		{"invalid", `{"a":}`, "", "INVALID_JSON", ""}, {"trailing", `{} {}`, "", "INVALID_JSON", ""},
		{"utf8", string([]byte{'"', 0xff, '"'}), "", "INVALID_JSON", ""},
		{"jq", `{}`, `.linies[] | {article}`, "VALIDATION", ""}, {"group", `[]`, `group_by(@, &article)`, "VALIDATION", ""},
		{"literal_unsafe", `{}`, "`9007199254740992`", "VALIDATION", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := inspectTestFile(t, dir, tc.name+".json", []byte(tc.source))
			result := inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "query": tc.query})
			if tc.code != "" {
				inspectTestError(t, result, tc.code)
				return
			}
			payload := inspectTestPayload(t, result)
			if payload["type"] != tc.kind {
				t.Fatalf("type=%v", payload["type"])
			}
			if tc.name == "empty" {
				if payload["status"] != "empty" || payload["result"] == nil {
					t.Fatal("empty array is not null")
				}
			}
		})
	}
	path := inspectTestFile(t, dir, "args.json", []byte(`[]`))
	for _, extra := range []map[string]any{{"limit": 0}, {"limit": 1.5}, {"offset": -1}, {"bogus": true}, {"detail": "bad"}, {"columns": []any{1}}, {"paths": []any{path}}} {
		extra["path"] = path
		inspectTestError(t, inspectTestCall(t, reg, "json_query", extra), "VALIDATION")
	}
}

func TestInspectJSONCSVAndResponseBudget(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := inspectTestFile(t, dir, "csv.json", []byte(`[{"text":"cafè, \"sí\"\nsegona","nested":{"v":2},"nil":null},{"text":"last"}]`))
	result := inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "output_format": "csv", "columns": []string{"text", "nested.v", "nil"}})
	payload := inspectTestPayload(t, result)
	records, err := csv.NewReader(strings.NewReader(resultText(t, result))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[1][0] != "cafè, \"sí\"\nsegona" || records[1][1] != "2" {
		t.Fatalf("CSV lost content: %#v", records)
	}
	rows := payload["rows"].([]any)
	if _, ok := rows[1].(map[string]any)["nil"]; ok {
		t.Fatal("missing field conflated with null")
	}
	reg.engine.GetConfig().ResponseMode = core.ResponseModeStructured
	structured := inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "output_format": "csv"})
	if strings.Contains(resultText(t, structured), "segona") {
		t.Fatal("structured mode duplicated body")
	}
	values := make([]string, 30)
	for i := range values {
		values[i] = strings.Repeat("x", 1000)
	}
	raw, _ := json.Marshal(values)
	path = inspectTestFile(t, dir, "large.json", raw)
	reg.engine.GetConfig().MaxResponseSize = 4000
	page := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path}))
	if page["truncation_reason"] != "response_bytes" || page["returned_count"].(float64) >= 30 {
		t.Fatal("did not paginate by response bytes")
	}
	huge, _ := json.Marshal(strings.Repeat("x", 5000))
	path = inspectTestFile(t, dir, "scalar.json", huge)
	inspectTestError(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path}), "LIMIT_EXCEEDED")
}

func TestInspectJSONWorkBounds(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := inspectTestFile(t, dir, "complex.json", []byte(`[1,2,3]`))
	query := strings.Repeat("[@,@] | ", 15) + "@"
	inspectTestError(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "query": query}), "VALIDATION")
	nested := strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)
	path = inspectTestFile(t, dir, "deep.json", []byte(nested))
	inspectTestError(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path}), "VALIDATION")
}

func TestInspectBytesEncodingAndBoundaries(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for _, tc := range []struct {
		name                            string
		raw                             []byte
		scan                            int
		bom                             string
		crlf, lf, cr, nonascii, invalid int
		complete, pending               bool
	}{
		{"mixed", []byte("a\r\nb\nc\rè\u00a0"), 100, "none", 1, 1, 1, 2, 0, true, false},
		{"utf8_bom", []byte("\xef\xbb\xbfè\n"), 100, "utf8", 0, 1, 0, 1, 0, true, false},
		{"utf16le", []byte{0xff, 0xfe, 0xe9, 0, 13, 0, 10, 0}, 100, "utf16le", 1, 0, 0, 1, 0, true, false},
		{"utf16be", []byte{0xfe, 0xff, 0, 0xe9, 0, 10}, 100, "utf16be", 0, 1, 0, 1, 0, true, false},
		{"utf8_cut", []byte("aè"), 2, "none", 0, 0, 0, 0, 0, false, true},
		{"crlf_cut", []byte("a\r\nb"), 2, "none", 0, 0, 0, 0, 0, false, true},
		{"utf16_cut", []byte{0xff, 0xfe, 0x3d, 0xd8, 0, 0xde}, 4, "utf16le", 0, 0, 0, 0, 0, false, true},
		{"invalid", []byte{0xff, '\n'}, 100, "none", 0, 1, 0, 0, 1, true, false},
		{"empty", []byte{}, 100, "none", 0, 0, 0, 0, 0, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := inspectTestFile(t, dir, tc.name+".bin", tc.raw)
			p := inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path, "max_scan_bytes": tc.scan}))
			e := p["encoding"].(map[string]any)
			if e["bom"] != tc.bom || e["crlf_count"] != float64(tc.crlf) || e["lf_count"] != float64(tc.lf) || e["cr_count"] != float64(tc.cr) {
				t.Fatalf("encoding %#v", e)
			}
			if p["scan_complete"] != tc.complete || p["sample_boundary_pending"] != tc.pending {
				t.Fatalf("scan metadata %#v", p)
			}
			if p["non_ascii"].(map[string]any)["count"] != float64(tc.nonascii) || p["invalid_sequences"].(map[string]any)["count"] != float64(tc.invalid) {
				t.Fatalf("positions %#v", p)
			}
		})
	}
}

func TestInspectBytesDumpAndRanges(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := inspectTestFile(t, dir, "range.txt", []byte("è\r\nsecond\nlast"))
	p := inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path, "mode": "bytes", "start_line": 2}))
	if p["byte_offset"] != float64(4) || p["hex"] != hex.EncodeToString([]byte("second\n")) {
		t.Fatalf("line range %#v", p)
	}
	p = inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path, "mode": "bytes", "byte_offset": 4, "max_bytes": 2}))
	if p["hex"] != "7365" || p["truncated"] != true {
		t.Fatal("bad dump")
	}
	next := p["continuation"].(map[string]any)
	p = inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", next))
	if p["byte_offset"] != float64(6) || p["hex"] != "636f" {
		t.Fatal("bad continuation")
	}
	path = inspectTestFile(t, dir, "long.txt", []byte(strings.Repeat("a", 100000)+"Z"))
	p = inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path, "mode": "bytes", "byte_offset": 100000}))
	if p["hex"] != "5a" {
		t.Fatal("long-line offset failed")
	}
	path = inspectTestFile(t, dir, "positions.txt", []byte(strings.Repeat("è", 60)))
	p = inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path}))
	positions := p["non_ascii"].(map[string]any)
	if positions["count"] != float64(60) || positions["returned_count"] != float64(50) || positions["truncated"] != true {
		t.Fatal("position cap")
	}
	reg.engine.GetConfig().MaxInspectScanBytes = 8
	p = inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path}))
	if p["bytes_scanned"] != float64(8) || p["scan_complete"] != false {
		t.Fatal("server scan cap")
	}
	for _, extra := range []map[string]any{{"max_scan_bytes": 0}, {"max_scan_bytes": 9}, {"end_line": 2}, {"mode": "bytes", "start_line": 1, "byte_offset": 0}, {"start_line": 1}, {"max_bytes": 0}} {
		extra["path"] = path
		inspectTestError(t, inspectTestCall(t, reg, "file_bytes", extra), "VALIDATION")
	}
}

func TestInspectHashBinaryParity(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for _, tc := range []struct {
		name         string
		a, b         []byte
		offset       int
		aByte, bByte any
	}{
		{"empty", []byte{}, []byte{}, -1, nil, nil},
		{"equal", []byte("\xef\xbb\xbfè\r\n"), []byte("\xef\xbb\xbfè\r\n"), -1, nil, nil},
		{"eol", []byte("a\r\n"), []byte("a\n"), 1, float64(13), float64(10)},
		{"eof", []byte("a"), []byte("ab"), 1, nil, float64('b')},
		{"block", append(bytes.Repeat([]byte{'a'}, core.DefaultBufferSize), 1), append(bytes.Repeat([]byte{'a'}, core.DefaultBufferSize), 2), core.DefaultBufferSize, float64(1), float64(2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := inspectTestFile(t, dir, "a.bin", tc.a)
			b := inspectTestFile(t, dir, "b.bin", tc.b)
			p := inspectTestPayload(t, inspectTestCall(t, reg, "file_hash", map[string]any{"paths": []string{a, b}, "compare": true}))
			if p["identical"] != (tc.offset < 0) {
				t.Fatal("wrong parity")
			}
			hashes := p["hashes"].([]any)
			for i, raw := range [][]byte{tc.a, tc.b} {
				digest := sha256.Sum256(raw)
				item := hashes[i].(map[string]any)
				if item["hash"] != hex.EncodeToString(digest[:]) || item["size"] != float64(len(raw)) {
					t.Fatal("wrong full hash/size")
				}
				stamp, err := time.Parse(time.RFC3339Nano, item["mtime"].(string))
				if err != nil || stamp.Location() != time.UTC {
					t.Fatal("mtime is not UTC RFC3339")
				}
			}
			if tc.offset >= 0 {
				d := p["first_difference"].(map[string]any)
				if d["offset"] != float64(tc.offset) || d["a"] != tc.aByte || d["b"] != tc.bByte {
					t.Fatalf("difference %#v", d)
				}
			}
		})
	}
	a := filepath.Join(dir, "a.bin")
	for _, args := range []map[string]any{{}, {"path": a, "paths": []string{a}}, {"paths": []string{}}, {"path": a, "compare": true}, {"paths": []string{a}, "compare": true}, {"paths": []string{a}, "algorithm": "md5"}} {
		inspectTestError(t, inspectTestCall(t, reg, "file_hash", args), "VALIDATION")
	}
	inspectTestError(t, inspectTestCall(t, reg, "file_hash", map[string]any{"paths": []string{a, filepath.Join(dir, "missing")}}), "NOT_FOUND")
}

func TestInspectAuthorizationReadonlyAndCancellation(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ReadOnly = true
	allowed := inspectTestFile(t, dir, "allowed.json", []byte(`{}`))
	outside := inspectTestFile(t, t.TempDir(), "outside.json", []byte(`{}`))
	secret := inspectTestFile(t, dir, ".env", []byte(`{}`))
	for _, name := range []string{"json_query", "file_bytes", "file_hash"} {
		inspectTestPayload(t, inspectTestCall(t, reg, name, map[string]any{"path": allowed}))
		inspectTestError(t, inspectTestCall(t, reg, name, map[string]any{"path": outside}), "NOT_ALLOWED")
		inspectTestError(t, inspectTestCall(t, reg, name, map[string]any{"path": secret}), "SECRET_DENIED")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := reg.handlers[name](ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: map[string]any{"path": allowed}}})
		if err != nil {
			t.Fatal(err)
		}
		inspectTestError(t, result, "CANCELLED")
	}
	policy, err := core.ParseFilePolicy([]byte(`{"version":1,"rules":[{"pattern":"allowed.json","level":"protected"},{"pattern":"hidden.json","level":"hidden"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	reg.engine.SetFilePolicy(policy)
	for _, name := range []string{"json_query", "file_bytes", "file_hash"} {
		inspectTestError(t, inspectTestCall(t, reg, name, map[string]any{"path": allowed}), "FILE_POLICY_DENIED")
		inspectTestError(t, inspectTestCall(t, reg, name, map[string]any{"path": filepath.Join(dir, "hidden.json")}), "NOT_FOUND")
	}
}

func TestInspectRegistrationProfilesAndExperimental(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for _, profile := range []toolProfile{profileStrict, profileAgent, profileUltra} {
		s := server.NewMCPServer("test", "0")
		r := &toolRegistry{server: s, engine: reg.engine, handlers: map[string]toolHandler{}, profile: profile}
		registerInspectTools(r)
		for _, name := range []string{"json_query", "file_bytes", "file_hash"} {
			tool, ok := s.ListTools()[name]
			if ok != (profile == profileUltra) {
				t.Fatalf("profile %s has %s=%v", profile, name, ok)
			}
			if ok {
				if !strings.Contains(tool.Tool.Description, "[EXPERIMENTAL") || tool.Tool.RawOutputSchema != nil || tool.Tool.OutputSchema.Type != "" {
					t.Fatalf("experimental contract: %+v", tool.Tool)
				}
				if tool.Tool.Annotations.ReadOnlyHint == nil || !*tool.Tool.Annotations.ReadOnlyHint {
					t.Fatal("not readonly")
				}
				if core.IsMutating(name, nil) {
					t.Fatal("classified as mutating")
				}
			}
		}
	}
}

func TestInspectConcurrentChangeDetection(t *testing.T) {
	dir := t.TempDir()
	path := inspectTestFile(t, dir, "change.txt", []byte("old"))
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := inspectStable(f, before, path); err == nil {
		t.Fatal("size change not detected")
	}
}

func Example_inspectQueryBudget() {
	multiplier, _, err := inspectQueryBudget(context.Background(), "linies[*].{a: a, b: b}")
	fmt.Println(multiplier, err)
	// Output: 2 <nil>
}
