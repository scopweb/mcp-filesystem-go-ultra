package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	localmcp "github.com/mcp/filesystem-ultra/mcp"
)

// setupIssue202607Engine creates a test engine for the search / counter fixes.
func setupIssue202607Engine(t *testing.T, compact bool) (*core.UltraFastEngine, string) {
	t.Helper()
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	config := &core.Config{
		Cache:            cacheInstance,
		AllowedPaths:     []string{tempDir},
		ParallelOps:      2,
		CompactMode:      compact,
		MaxSearchResults: 100,
	}

	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("Failed to create test engine: %v", err)
	}

	return engine, tempDir
}

func makeIssueSearchRequest(args map[string]interface{}) localmcp.CallToolRequest {
	return localmcp.CallToolRequest{Arguments: args}
}

func responseIssueText(r *localmcp.CallToolResponse) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// =============================================================================
// BUG 1 / 2 — search_files compact output must include line content
// =============================================================================

// TestIssue202607_AdvancedCompactIncludesLine ensures the compact branch of
// AdvancedTextSearch prints the matched line content, not just coordinates.
func TestIssue202607_AdvancedCompactIncludesLine(t *testing.T) {
	engine, tempDir := setupIssue202607Engine(t, true)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "hits.txt")
	lines := []string{
		"alpha",
		"beta",
		"gamma",
		"delta",
		"epsilon",
		"zeta",
		"eta",
	}
	// Pattern 'e' matches every test line (unlike 'a', which misses "epsilon").
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := makeIssueSearchRequest(map[string]interface{}{
		"path":          tempDir,
		"pattern":       ".",
		"output_format": "auto",
	})

	resp, err := engine.AdvancedTextSearch(ctx, req)
	if err != nil {
		t.Fatalf("AdvancedTextSearch error: %v", err)
	}
	text := responseIssueText(resp)

	for _, line := range lines {
		if !strings.Contains(text, line) {
			t.Errorf("compact output missing line content %q; got:\n%s", line, text)
		}
	}
}

// TestIssue202607_OutputTextOverridesCompact verifies that an explicit
// output_format:"text" opts out of CompactMode and returns the verbose header.
func TestIssue202607_OutputTextOverridesCompact(t *testing.T) {
	engine, tempDir := setupIssue202607Engine(t, true)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "hits.txt")
	content := strings.Join([]string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"}, "\n") + "\n"
	// Pattern 'e' matches every test line.
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := makeIssueSearchRequest(map[string]interface{}{
		"path":          tempDir,
		"pattern":       ".",
		"output_format": "text",
	})

	resp, err := engine.AdvancedTextSearch(ctx, req)
	if err != nil {
		t.Fatalf("AdvancedTextSearch error: %v", err)
	}
	text := responseIssueText(resp)

	if !strings.Contains(text, "Found 7 matches") {
		t.Errorf("explicit text output did not produce verbose format; got:\n%s", text)
	}
}

// TestIssue202607_SmartSearchCompactIncludesLine ensures SmartSearch content
// matches in compact mode include the matched line text.
func TestIssue202607_SmartSearchCompactIncludesLine(t *testing.T) {
	engine, tempDir := setupIssue202607Engine(t, true)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "hits.go")
	if err := os.WriteFile(testFile, []byte("package main\n\n// target_marker\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := makeIssueSearchRequest(map[string]interface{}{
		"path":            tempDir,
		"pattern":         "target_marker",
		"include_content": true,
	})

	resp, err := engine.SmartSearch(ctx, req)
	if err != nil {
		t.Fatalf("SmartSearch error: %v", err)
	}
	text := responseIssueText(resp)

	if !strings.Contains(text, "target_marker") {
		t.Errorf("SmartSearch compact output missing line content; got:\n%s", text)
	}
}

// =============================================================================
// BUG 3 — total_lines consistent with read_file (no phantom trailing line)
// =============================================================================

// TestIssue202607_CountLinesAndTotalLines verifies CountLines and the line
// counters reported by EditFile/MultiEdit agree with read_file semantics.
func TestIssue202607_CountLinesAndTotalLines(t *testing.T) {
	engine, tempDir := setupIssue202607Engine(t, false)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "lines.txt")
	content := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if got := core.CountLines(content); got != 10 {
		t.Errorf("CountLines(trailing newline) = %d, want 10", got)
	}

	res, err := engine.EditFile(ctx, testFile, "5\n", "FIVE\n", false, false, false)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if res.TotalLines != 10 {
		t.Errorf("EditFile TotalLines = %d, want 10", res.TotalLines)
	}

	edits := []core.MultiEditOperation{
		{OldText: "FIVE\n", NewText: "five\n"},
	}
	multiRes, err := engine.MultiEdit(ctx, testFile, edits, false, false, false, "")
	if err != nil {
		t.Fatalf("MultiEdit failed: %v", err)
	}
	if multiRes.TotalLines != 10 {
		t.Errorf("MultiEdit TotalLines = %d, want 10", multiRes.TotalLines)
	}
}

// TestIssue202607_CountLinesNoTrailingNewline verifies CountLines and edit
// counters handle files without a trailing newline and empty files correctly.
func TestIssue202607_CountLinesNoTrailingNewline(t *testing.T) {
	engine, tempDir := setupIssue202607Engine(t, false)
	ctx := context.Background()

	if got := core.CountLines(""); got != 0 {
		t.Errorf("CountLines(empty) = %d, want 0", got)
	}

	content := "1\n2\n3"
	if got := core.CountLines(content); got != 3 {
		t.Errorf("CountLines(no trailing newline) = %d, want 3", got)
	}

	testFile := filepath.Join(tempDir, "no_trailing.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	res, err := engine.EditFile(ctx, testFile, "2\n", "TWO\n", false, false, false)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if res.TotalLines != 3 {
		t.Errorf("EditFile TotalLines (no trailing newline) = %d, want 3", res.TotalLines)
	}
}

// TestIssue202607_PureInsertionNoRemoval verifies a pure insertion reports
// zero lines removed.
func TestIssue202607_PureInsertionNoRemoval(t *testing.T) {
	engine, tempDir := setupIssue202607Engine(t, false)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "insert.txt")
	content := "aaa\nbbb\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	res, err := engine.EditFile(ctx, testFile, "bbb\n", "bbb\nccc\n", false, false, false)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if res.LinesRemoved != 0 {
		t.Errorf("EditFile LinesRemoved = %d, want 0 for pure insertion", res.LinesRemoved)
	}
	if res.LinesAdded != 1 {
		t.Errorf("EditFile LinesAdded = %d, want 1", res.LinesAdded)
	}
}

// =============================================================================
// OBS 5 — max_results honored on content search + JSON metadata
// =============================================================================

// TestIssue202607_MaxResultsCapJSON verifies that max_results caps the content
// search results and the JSON payload reports total/returned/truncated.
func TestIssue202607_MaxResultsCapJSON(t *testing.T) {
	engine, tempDir := setupIssue202607Engine(t, false)
	ctx := context.Background()

	testFile := filepath.Join(tempDir, "cap.txt")
	content := "m1\nm2\nm3\nm4\nm5\nm6\nm7\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := makeIssueSearchRequest(map[string]interface{}{
		"path":          tempDir,
		"pattern":       "m",
		"output_format": "json",
		"max_results":   3.0,
	})

	resp, err := engine.AdvancedTextSearch(ctx, req)
	if err != nil {
		t.Fatalf("AdvancedTextSearch error: %v", err)
	}
	text := responseIssueText(resp)

	var payload struct {
		TotalMatches    int  `json:"total_matches"`
		ReturnedMatches int  `json:"returned_matches"`
		Truncated       bool `json:"truncated"`
		Matches         []struct {
			LineContent string `json:"line_content"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("JSON parse failed: %v\nbody:\n%s", err, text)
	}

	if payload.TotalMatches != 7 {
		t.Errorf("total_matches = %d, want 7", payload.TotalMatches)
	}
	if payload.ReturnedMatches != 3 {
		t.Errorf("returned_matches = %d, want 3", payload.ReturnedMatches)
	}
	if !payload.Truncated {
		t.Errorf("truncated = false, want true")
	}
	if len(payload.Matches) != 3 {
		t.Errorf("len(matches) = %d, want 3", len(payload.Matches))
	}

	// Text output branch must also be capped.
	req2 := makeIssueSearchRequest(map[string]interface{}{
		"path":        tempDir,
		"pattern":     "m",
		"max_results": 3.0,
	})
	resp2, err := engine.AdvancedTextSearch(ctx, req2)
	if err != nil {
		t.Fatalf("AdvancedTextSearch text error: %v", err)
	}
	text2 := responseIssueText(resp2)
	count := strings.Count(text2, "m")
	if count > 6 { // 3 lines, each contains one 'm'
		t.Errorf("text output returned more than 3 match lines; got:\n%s", text2)
	}
}
