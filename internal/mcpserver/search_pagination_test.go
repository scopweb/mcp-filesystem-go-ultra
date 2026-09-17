package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearch_CompactHonorsMaxResultsOverHidden20(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().CompactMode = true
	for i := 1; i <= 25; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%02d.txt", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("needle %d\n", i)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "needle", "include_content": true, "max_results": float64(25),
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "first 20") {
		t.Fatalf("hidden 20 cap still present:\n%s", text)
	}
	m, _ := res.StructuredContent.(map[string]any)
	hits := e5Maps(m["matches"])
	if len(hits) != 25 {
		t.Fatalf("want 25 matches, got %d text=%s", len(hits), text)
	}
}

func TestSearch_PaginationNoDupNoGap(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	for i := 1; i <= 12; i++ {
		p := filepath.Join(dir, fmt.Sprintf("p%02d.txt", i))
		if err := os.WriteFile(p, []byte("token\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	page1 := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "token", "include_content": true, "max_results": float64(5),
	})
	m1, _ := page1.StructuredContent.(map[string]any)
	if m1["truncated"] != true {
		t.Fatalf("page1 should truncate: %#v", m1)
	}
	hits1 := e5Maps(m1["matches"])
	if len(hits1) != 5 {
		t.Fatalf("page1 len=%d", len(hits1))
	}
	cont, _ := m1["continuation"].(string)
	if !strings.Contains(cont, "offset:5") {
		t.Fatalf("continuation: %s", cont)
	}
	page2 := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "token", "include_content": true, "max_results": float64(5), "offset": float64(5),
	})
	m2, _ := page2.StructuredContent.(map[string]any)
	hits2 := e5Maps(m2["matches"])
	if len(hits2) != 5 {
		t.Fatalf("page2 len=%d", len(hits2))
	}
	page3 := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "token", "include_content": true, "max_results": float64(5), "offset": float64(10),
	})
	m3, _ := page3.StructuredContent.(map[string]any)
	hits3 := e5Maps(m3["matches"])
	if len(hits3) != 2 {
		t.Fatalf("page3 len=%d", len(hits3))
	}
	seen := map[string]bool{}
	for _, group := range [][]map[string]any{hits1, hits2, hits3} {
		for _, h := range group {
			p, _ := h["path"].(string)
			if seen[p] {
				t.Fatalf("duplicate %s", p)
			}
			seen[p] = true
		}
	}
	if len(seen) != 12 {
		t.Fatalf("got %d unique, want 12", len(seen))
	}
}

func TestSearch_ManyHitsOneFile(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "hit line %d\n", i)
	}
	p := filepath.Join(dir, "one.txt")
	if err := os.WriteFile(p, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "hit line", "include_content": true, "max_results": float64(10),
	})
	m, _ := res.StructuredContent.(map[string]any)
	if len(e5Maps(m["matches"])) != 10 {
		t.Fatalf("page: %#v", m)
	}
	if m["truncated"] != true {
		t.Fatal("expected truncated")
	}
}

func TestSearch_IncludeContextNumbered(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	p := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(p, []byte("a\nTARGET\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callNamed(t, reg, context.Background(), "search_files", map[string]interface{}{
		"path": dir, "pattern": "TARGET", "include_content": true, "include_context": true, "context_lines": float64(1),
		"output_format": "text",
	})
	text := resultText(t, res)
	if !strings.Contains(text, ">2 |") && !strings.Contains(text, ">2|") {
		if !strings.Contains(text, "2 |") {
			t.Fatalf("expected numbered match line:\n%s", text)
		}
	}
}
