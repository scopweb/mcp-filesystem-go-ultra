package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	localmcp "github.com/mcp/filesystem-ultra/mcp"
)

// ripgrepTestEngine builds a minimal engine + cache for testing the
// AdvancedTextSearch output formatting. CompactMode=false so we exercise the
// verbose branch; MaxSearchResults=100 is well above the ripgrep threshold of 5.
func ripgrepTestEngine(t *testing.T) *core.UltraFastEngine {
	t.Helper()
	c, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:                c,
		ParallelOps:          2,
		MaxSearchResults:     100,
		MaxSearchOutputBytes: 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("engine init: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

// TestRipgrepFormat_FewMatches: ≤5 matches → ripgrep-style 'path:line:content'.
// Should NOT contain the verbose header (🔍) or emoji markers (📁).
func TestRipgrepFormat_FewMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\nfoo bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello again\nskip\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e := ripgrepTestEngine(t)
	resp, err := e.AdvancedTextSearch(context.Background(), localmcp.CallToolRequest{
		Arguments: map[string]interface{}{
			"path": dir, "pattern": "hello", "case_sensitive": true,
			// output_format omitted → engine sees "" → defaults to "auto"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("no content in response")
	}
	out := resp.Content[0].Text

	// Expected: ripgrep-style lines
	if !strings.Contains(out, "a.txt:1:hello world") {
		t.Errorf("expected ripgrep line for a.txt, got: %q", out)
	}
	if !strings.Contains(out, "b.txt:1:hello again") {
		t.Errorf("expected ripgrep line for b.txt, got: %q", out)
	}
	// NOT expected: verbose header or emoji marker
	if strings.Contains(out, "🔍 Found") {
		t.Errorf("did not expect verbose header for ≤5 matches, got: %q", out)
	}
	if strings.Contains(out, "📁") {
		t.Errorf("did not expect emoji marker for ≤5 matches, got: %q", out)
	}
}

// TestRipgrepFormat_ManyMatchesFallsBackVerbose: >5 matches → verbose header.
// Should contain '🔍 Found N matches' and at least one 📁 line.
func TestRipgrepFormat_ManyMatchesFallsBackVerbose(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "match line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	e := ripgrepTestEngine(t)
	resp, err := e.AdvancedTextSearch(context.Background(), localmcp.CallToolRequest{
		Arguments: map[string]interface{}{
			"path": dir, "pattern": "match", "case_sensitive": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("no content in response")
	}
	out := resp.Content[0].Text

	if !strings.Contains(out, "🔍 Found") {
		t.Errorf("expected verbose header for >5 matches, got: %q", out)
	}
	if !strings.Contains(out, "📁") {
		t.Errorf("expected emoji marker for verbose path, got: %q", out)
	}
}

// TestRipgrepFormat_ContextForcesVerbose: include_context:true always forces
// verbose (Context: blocks don't fit the single-line ripgrep layout).
func TestRipgrepFormat_ContextForcesVerbose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("alpha\nbeta MATCH gamma\ndelta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e := ripgrepTestEngine(t)
	resp, err := e.AdvancedTextSearch(context.Background(), localmcp.CallToolRequest{
		Arguments: map[string]interface{}{
			"path": dir, "pattern": "MATCH", "case_sensitive": true,
			"include_context": true, "context_lines": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("no content in response")
	}
	out := resp.Content[0].Text

	if !strings.Contains(out, "Context:") {
		t.Errorf("include_context should force verbose with Context: block, got: %q", out)
	}
	if !strings.Contains(out, "🔍 Found") {
		t.Errorf("include_context should preserve verbose header, got: %q", out)
	}
}

// TestRipgrepFormat_JSONUnchanged: explicit output_format:"json" still emits
// structured JSON regardless of match count.
func TestRipgrepFormat_JSONUnchanged(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "j.txt"), []byte("find me\nskip\nfind me too\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e := ripgrepTestEngine(t)
	resp, err := e.AdvancedTextSearch(context.Background(), localmcp.CallToolRequest{
		Arguments: map[string]interface{}{
			"path": dir, "pattern": "find", "case_sensitive": true,
			"output_format": "json",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("no content in response")
	}
	out := resp.Content[0].Text

	if !strings.Contains(out, `"matches":`) {
		t.Errorf("JSON output_format must still emit structured JSON, got: %q", out)
	}
	if strings.Contains(out, "🔍") {
		t.Errorf("JSON path should not contain emoji headers, got: %q", out)
	}
	if strings.Contains(out, "📁") {
		t.Errorf("JSON path should not contain emoji markers, got: %q", out)
	}
}

// TestRipgrepFormat_ThresholdBoundary: 5 matches → ripgrep; 6 matches → verbose.
// Verifies the threshold is strictly ≤5 (>=6 falls back to verbose header).
func TestRipgrepFormat_ThresholdBoundary(t *testing.T) {
	for _, n := range []int{5, 6} {
		n := n
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte(strings.Repeat("hit\n", n)), 0644); err != nil {
				t.Fatal(err)
			}
			e := ripgrepTestEngine(t)
			resp, err := e.AdvancedTextSearch(context.Background(), localmcp.CallToolRequest{
				Arguments: map[string]interface{}{
					"path": dir, "pattern": "hit", "case_sensitive": true,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Content) == 0 {
				t.Fatal("no content in response")
			}
			out := resp.Content[0].Text

			isRipgrep := strings.Contains(out, "x.txt:1:hit")
			isVerbose := strings.Contains(out, "🔍 Found")
			if n == 5 && !isRipgrep {
				t.Errorf("n=5 should use ripgrep-style, got: %q", out)
			}
			if n == 6 && !isVerbose {
				t.Errorf("n=6 should use verbose, got: %q", out)
			}
		})
	}
}

// TestRipgrepFormat_ExplicitTextBackwardCompat: passing output_format:"text"
// explicitly should preserve the legacy verbose format even for ≤5 matches.
// This is the contract: explicit "text" = backward-compatible, only the empty
// default switches to auto.
func TestRipgrepFormat_ExplicitTextBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "t.txt"), []byte("only match\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e := ripgrepTestEngine(t)
	resp, err := e.AdvancedTextSearch(context.Background(), localmcp.CallToolRequest{
		Arguments: map[string]interface{}{
			"path": dir, "pattern": "only", "case_sensitive": true,
			"output_format": "text",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("no content in response")
	}
	out := resp.Content[0].Text

	// Legacy verbose: header + emoji + line content separately
	if !strings.Contains(out, "🔍 Found") {
		t.Errorf("explicit output_format:text should keep legacy verbose header, got: %q", out)
	}
	if !strings.Contains(out, "📁") {
		t.Errorf("explicit output_format:text should keep legacy emoji marker, got: %q", out)
	}
	if strings.Contains(out, "t.txt:1:only match") {
		t.Errorf("explicit output_format:text should NOT use ripgrep layout, got: %q", out)
	}
}