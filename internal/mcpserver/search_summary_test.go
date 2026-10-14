package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchSummary_DoesNotDuplicateMatches(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "hits.txt")
	var b strings.Builder
	for i := 1; i <= 116; i++ {
		fmt.Fprintf(&b, "HIT-%03d token\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": path, "pattern": "HIT-", "include_content": true, "max_results": float64(116),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	msg := asString(m["message"])
	if strings.Contains(msg, "HIT-001") || strings.Contains(msg, "token") {
		t.Fatalf("message copied match bodies: %q", msg)
	}
	if !strings.Contains(msg, "116 coincidencias en contenido") {
		t.Fatalf("summary: %q", msg)
	}
	matches := e5Maps(m["matches"])
	if len(matches) != 116 {
		t.Fatalf("matches=%d", len(matches))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "HIT-001") {
		t.Fatal("text fallback lost the listing")
	}
	actual, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dup := map[string]any{}
	for k, v := range m {
		dup[k] = v
	}
	dup["message"] = text
	before, err := json.Marshal(dup)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) >= len(before) {
		t.Fatalf("expected a smaller structured payload: now %d before %d", len(actual), len(before))
	}
	t.Logf("116-hit structured JSON %d bytes; duplicated message would be %d bytes (saved %d, %.1f%%). Measurement is json.Marshal of structuredContent only, not the text content block or model tokenizer.",
		len(actual), len(before), len(before)-len(actual), 100*float64(len(before)-len(actual))/float64(len(before)))
}

func TestSearchEmpty_StatesScope(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	content := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "ZZZ_NO_MATCH", "include_content": true,
	})
	cm, _ := content.StructuredContent.(map[string]any)
	if cm["status"] != statusEmpty || asString(cm["message"]) != "0 coincidencias en contenido" {
		t.Fatalf("content empty: %#v", cm)
	}
	if !strings.Contains(resultText(t, content), "0 coincidencias en contenido") {
		t.Fatalf("text missing content scope:\n%s", resultText(t, content))
	}
	name := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "ZZZ_NO_MATCH",
	})
	nm, _ := name.StructuredContent.(map[string]any)
	if asString(nm["message"]) != "0 coincidencias en nombres de fichero" {
		t.Fatalf("filename empty: %#v text=%s", nm, resultText(t, name))
	}
	if !strings.Contains(resultText(t, name), "0 coincidencias en nombres de fichero") {
		t.Fatalf("text missing filename scope:\n%s", resultText(t, name))
	}
}

func TestListAllowed_MessageIsSummary(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	res := callListAllowed(t, reg, nil)
	m, _ := res.StructuredContent.(map[string]any)
	msg := asString(m["message"])
	paths, _ := m["paths"].([]string)
	if len(paths) == 0 {
		if arr, ok := m["paths"].([]any); ok {
			paths = make([]string, 0, len(arr))
			for _, p := range arr {
				paths = append(paths, fmt.Sprint(p))
			}
		}
	}
	if len(paths) == 0 || !strings.Contains(msg, "raíces permitidas") {
		t.Fatalf("summary/paths: msg=%q paths=%v", msg, m["paths"])
	}
	if strings.Contains(msg, paths[0]) {
		t.Fatalf("message repeated a root: %q", msg)
	}
	if !strings.Contains(resultText(t, res), paths[0]) {
		t.Fatal("text fallback lost the root listing")
	}
}
