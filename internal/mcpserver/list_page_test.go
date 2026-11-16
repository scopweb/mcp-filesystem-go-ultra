package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestListDirectory_CompactTextHonorsMaxNodesAndOffset(t *testing.T) {
	dir := t.TempDir()
	const n = 12
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%04d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := newHelpTestRegistry(t, dir)

	first := callPatchTool(t, reg, "list_directory", map[string]any{
		"path": dir, "max_nodes": float64(5),
	})
	if first.IsError {
		t.Fatal(resultText(t, first))
	}
	text := resultText(t, first)
	assertCompactPage(t, text, dir)
	for _, name := range []string{"file-0000.txt", "file-0004.txt"} {
		if !strings.Contains(text, "FILE "+name) {
			t.Fatalf("first page missing %s:\n%s", name, text)
		}
	}
	if strings.Contains(text, "file-0005.txt") {
		t.Fatalf("first page ignored max_nodes:\n%s", text)
	}
	if !strings.Contains(text, "offset:5") {
		t.Fatalf("first page missing continuation offset:\n%s", text)
	}
	if strings.Contains(text, "narrow the path") {
		t.Fatalf("first page kept the old spill footer:\n%s", text)
	}
	if len(text) > claudeSpillChars {
		t.Fatalf("text %d exceeds %d", len(text), claudeSpillChars)
	}
	names := listingNames(t, first)
	if len(names) != 5 || names[0] != "file-0000.txt" || names[4] != "file-0004.txt" {
		t.Fatalf("structured page = %#v", names)
	}
	if got := continuationOffset(t, first); got != 5 {
		t.Fatalf("structured offset = %v, want 5", got)
	}

	second := callPatchTool(t, reg, "list_directory", map[string]any{
		"path": dir, "max_nodes": float64(5), "offset": float64(5),
	})
	if second.IsError {
		t.Fatal(resultText(t, second))
	}
	text2 := resultText(t, second)
	assertCompactPage(t, text2, dir)
	if !strings.Contains(text2, "FILE file-0005.txt") || strings.Contains(text2, "file-0000.txt") {
		t.Fatalf("second page window wrong:\n%s", text2)
	}
	if !strings.Contains(text2, "offset:10") {
		t.Fatalf("second page missing offset:10:\n%s", text2)
	}
	names2 := listingNames(t, second)
	if len(names2) != 5 || names2[0] != "file-0005.txt" {
		t.Fatalf("second structured page = %#v", names2)
	}

	last := callPatchTool(t, reg, "list_directory", map[string]any{
		"path": dir, "max_nodes": float64(5), "offset": float64(10),
	})
	text3 := resultText(t, last)
	assertCompactPage(t, text3, dir)
	if strings.Contains(text3, "continue with offset:") {
		t.Fatalf("last page must not ask for another offset:\n%s", text3)
	}
	if !strings.Contains(text3, "FILE file-0010.txt") || !strings.Contains(text3, "FILE file-0011.txt") {
		t.Fatalf("last page missing tail:\n%s", text3)
	}
	if sc, _ := last.StructuredContent.(map[string]any); sc["continuation"] != nil {
		t.Fatalf("last page structured continuation should be absent: %#v", sc["continuation"])
	}

	js := callPatchTool(t, reg, "list_directory", map[string]any{
		"path": dir, "output_format": "json", "max_nodes": float64(5),
	})
	jsText := resultText(t, js)
	if !strings.Contains(jsText, "\"entries\"") {
		t.Fatalf("json format changed:\n%s", jsText)
	}
	if got := listingNames(t, js); len(got) != n {
		t.Fatalf("json page should stay uncapped by max_nodes, got %d names", len(got))
	}
}

func TestListDirectory_CompactModeSecondPageStaysOneLine(t *testing.T) {
	dir := t.TempDir()
	const n = 8
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%04d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().CompactMode = true

	second := callPatchTool(t, reg, "list_directory", map[string]any{
		"path": dir, "max_nodes": float64(3), "offset": float64(3),
	})
	if second.IsError {
		t.Fatal(resultText(t, second))
	}
	text := resultText(t, second)
	if strings.HasPrefix(strings.TrimSpace(text), "{") || strings.Contains(text, "\"entries\"") || strings.Contains(text, "FILE ") {
		t.Fatalf("compact mode switched format on the second page:\n%s", text)
	}
	if !strings.Contains(text, "file-0003.txt") || strings.Contains(text, "file-0000.txt") {
		t.Fatalf("second compact page window wrong:\n%s", text)
	}
	if !strings.Contains(text, "offset:6") {
		t.Fatalf("second compact page missing offset:6:\n%s", text)
	}
	if got := continuationOffset(t, second); got != 6 {
		t.Fatalf("structured offset = %v, want 6", got)
	}
	if names := listingNames(t, second); len(names) != 3 || names[0] != "file-0003.txt" {
		t.Fatalf("structured compact page = %#v", names)
	}
}

func TestListDirectory_CompactTextStaysWithinBudget(t *testing.T) {
	dir := t.TempDir()
	const n = 400
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("w%03d-%s.txt", i, strings.Repeat("n", 120))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "list_directory", map[string]any{
		"path": dir, "max_nodes": float64(n),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if len(text) > claudeSpillChars {
		t.Fatalf("text %d exceeds %d", len(text), claudeSpillChars)
	}
	names := listingNames(t, res)
	if len(names) == 0 || len(names) >= n {
		t.Fatalf("budget fit should show a partial page, got %d", len(names))
	}
	want := fmt.Sprintf("offset:%d", len(names))
	if !strings.Contains(text, want) {
		t.Fatalf("text offset %q missing:\n%s", want, text)
	}
	if got := continuationOffset(t, res); got != float64(len(names)) {
		t.Fatalf("structured offset %v, text page %d", got, len(names))
	}
	for _, name := range names {
		if !strings.Contains(text, name) {
			t.Fatalf("structured name missing from text: %s", name)
		}
	}
}

func assertCompactPage(t *testing.T, text, dir string) {
	t.Helper()
	if strings.HasPrefix(strings.TrimSpace(text), "{") || strings.Contains(text, "\"entries\"") {
		t.Fatalf("compact page switched to JSON:\n%s", text)
	}
	if !strings.HasPrefix(text, dir+"\n") || !strings.Contains(text, "FILE ") || !strings.Contains(text, "--- |") {
		t.Fatalf("compact layout lost:\n%s", text)
	}
}

func listingNames(t *testing.T, res *mcp.CallToolResult) []string {
	t.Helper()
	sc, _ := res.StructuredContent.(map[string]any)
	raw, err := json.Marshal(sc["entries"])
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name, _ := e["name"].(string)
		names = append(names, name)
	}
	return names
}

func continuationOffset(t *testing.T, res *mcp.CallToolResult) float64 {
	t.Helper()
	sc, _ := res.StructuredContent.(map[string]any)
	cont, _ := sc["continuation"].(map[string]any)
	if cont == nil {
		t.Fatalf("missing continuation: %#v", sc)
	}
	return asFloatArg(cont["offset"])
}
