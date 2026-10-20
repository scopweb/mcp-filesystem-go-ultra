package mcpserver

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectLineContinuationStaysInsideRange(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := inspectTestFile(t, dir, "range.txt", []byte("header\r\nabcdefghij\r\ntail"))
	args := map[string]any{"path": path, "mode": "all", "start_line": 2, "max_bytes": 3}
	var combined []byte
	for page := 0; page < 10; page++ {
		result := inspectTestCall(t, reg, "file_bytes", args)
		p := inspectTestPayload(t, result)
		raw, err := hex.DecodeString(p["hex"].(string))
		if err != nil {
			t.Fatal(err)
		}
		combined = append(combined, raw...)
		if page == 0 && !strings.Contains(resultText(t, result), "00000008") {
			t.Fatal("dump is missing absolute offset")
		}
		if p["truncated"] == false {
			break
		}
		args = p["continuation"].(map[string]any)
	}
	if string(combined) != "abcdefghij\r\n" {
		t.Fatalf("range continuation spilled or lost bytes: %q", combined)
	}
}

func TestInspectUTF16AndUnsupportedDiagnostics(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	raw := []byte{0xfe, 0xff, 0, 65, 0, 13, 0, 10, 0, 0xe9, 0, 10}
	path := inspectTestFile(t, dir, "utf16.txt", raw)
	p := inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path, "mode": "all", "start_line": 2}))
	if p["byte_offset"] != float64(8) || p["hex"] != hex.EncodeToString(raw[8:]) {
		t.Fatalf("UTF16 range %#v", p)
	}
	position := p["non_ascii"].(map[string]any)["positions"].([]any)[0].(map[string]any)
	if position["line"] != float64(2) || position["column"] != float64(1) || position["byte_offset"] != float64(8) {
		t.Fatalf("UTF16 position %#v", position)
	}
	path = inspectTestFile(t, dir, "utf32.txt", []byte{0xff, 0xfe, 0, 0, 65, 0, 0, 0})
	p = inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path, "mode": "all"}))
	if p["encoding"].(map[string]any)["bom"] != "utf32le" || p["encoding"].(map[string]any)["decoding_supported"] != false || p["non_ascii"] != nil {
		t.Fatalf("unsupported encoding presented as zero counts: %#v", p)
	}
	path = inspectTestFile(t, dir, "broken.txt", []byte{0xff, 0xfe, 0, 0xd8})
	p = inspectTestPayload(t, inspectTestCall(t, reg, "file_bytes", map[string]any{"path": path}))
	if p["invalid_sequences"].(map[string]any)["count"] != float64(1) {
		t.Fatal("true truncated UTF16 at EOF was not invalid")
	}
}

func TestInspectJSONColumnPathsAndBareCR(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := inspectTestFile(t, dir, "dots.json", []byte(`[{"a.b":"literal","a":{"b":"nested"},"text":"a\rb"}]`))
	p := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "output_format": "table"}))
	if p["rows"].([]any)[0].(map[string]any)["a.b"] != "literal" {
		t.Fatal("inferred literal key became a path")
	}
	result := inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "output_format": "csv", "columns": []string{"a.b", "text"}})
	p = inspectTestPayload(t, result)
	if p["rows"].([]any)[0].(map[string]any)["a.b"] != "nested" {
		t.Fatal("explicit path treated as a literal key")
	}
	records, err := csv.NewReader(strings.NewReader(resultText(t, result))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if records[1][1] != "a\rb" {
		t.Fatalf("CSV changed a bare CR: %q", records[1][1])
	}
}

func TestInspectJSONDefaultPageAndAllocationLimits(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	values := make([]int, 101)
	raw, _ := json.Marshal(values)
	path := inspectTestFile(t, dir, "default.json", raw)
	p := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path}))
	if p["returned_count"] != float64(100) || p["total_count"] != float64(101) {
		t.Fatal("default page not 100")
	}
	path = inspectTestFile(t, dir, "nodes.json", []byte("["+strings.Repeat("0,", inspectJSONNodes)+"0]"))
	inspectTestError(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path}), "VALIDATION")
	path = inspectTestFile(t, dir, "size.json", bytes.Repeat([]byte{' '}, inspectJSONInputLimit+1))
	inspectTestError(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path}), "LIMIT_EXCEEDED")
	query := strings.Repeat("to_string([@]) | ", 15) + "@"
	if _, _, err := inspectQueryBudget(context.Background(), query); err == nil {
		t.Fatal("nested serialization amplification was admitted")
	}
}

func TestInspectJSONUnorderedPagination(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := inspectTestFile(t, dir, "object.json", []byte(`{"b":2,"a":1}`))
	inspectTestError(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "query": "keys(@)", "limit": 1}), "VALIDATION")
	p := inspectTestPayload(t, inspectTestCall(t, reg, "json_query", map[string]any{"path": path, "query": "keys(@) | sort(@)", "limit": 1}))
	if p["result"].([]any)[0] != "a" {
		t.Fatal("wrong sorted page")
	}
	p = inspectTestPayload(t, inspectTestCall(t, reg, "json_query", p["continuation"].(map[string]any)))
	if p["result"].([]any)[0] != "b" {
		t.Fatal("wrong sorted continuation")
	}
	if inspectQueryUnordered("items[*].{key: key}") {
		t.Fatal("array wildcard mistaken for object iteration")
	}
}

func TestInspectSymlinkAuthorization(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	target := inspectTestFile(t, dir, "target.json", []byte(`{}`))
	alias := filepath.Join(dir, "alias.json")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	outside := inspectTestFile(t, t.TempDir(), "outside.json", []byte(`{}`))
	escape := filepath.Join(dir, "escape.json")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"json_query", "file_bytes", "file_hash"} {
		inspectTestPayload(t, inspectTestCall(t, reg, tool, map[string]any{"path": alias}))
		inspectTestError(t, inspectTestCall(t, reg, tool, map[string]any{"path": escape}), "NOT_ALLOWED")
	}
}
