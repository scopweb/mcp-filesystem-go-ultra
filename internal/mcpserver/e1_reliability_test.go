package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func fileBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEditFile_DryRun_PreservesBytesAllModes(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	original := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	path := filepath.Join(dir, "dry.txt")
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	modes := []struct {
		name string
		args map[string]interface{}
	}{
		{"replace", map[string]interface{}{"path": path, "old_text": "line 2", "new_text": "LINE 2", "dry_run": true}},
		{"occurrence", map[string]interface{}{"path": path, "old_text": "line", "new_text": "LINE", "occurrence": float64(1), "dry_run": true}},
		{"replace_range", map[string]interface{}{"path": path, "mode": "replace_range", "start_line": float64(2), "end_line": float64(3), "new_text": "X", "dry_run": true}},
		{"delete_range", map[string]interface{}{"path": path, "mode": "delete_range", "start_line": float64(2), "end_line": float64(3), "dry_run": true}},
		{"insert", map[string]interface{}{"path": path, "mode": "insert", "anchor": "line 1", "new_text": "INSERTED", "dry_run": true}},
		{"search_replace", map[string]interface{}{"path": path, "mode": "search_replace", "pattern": "line", "replacement": "LINE", "dry_run": true}},
	}
	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			before := fileBytes(t, path)
			res := callEdit(t, reg, tc.args)
			if res.IsError {
				t.Fatalf("dry_run errored: %s", resultText(t, res))
			}
			text := resultText(t, res)
			if !strings.Contains(strings.ToUpper(text), "DRY RUN") {
				t.Fatalf("expected DRY RUN marker, got:\n%s", text)
			}
			if !strings.Contains(text, "current_hash:") || !strings.Contains(text, "predicted_hash:") {
				t.Fatalf("expected current/predicted hashes, got:\n%s", text)
			}
			after := fileBytes(t, path)
			if string(after) != string(before) {
				t.Fatalf("dry_run mutated disk:\nbefore=%q\nafter=%q", before, after)
			}
		})
	}
}

func TestEditFile_DryRun_PredictedMatchesApply(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "pred.txt")
	original := "alpha beta\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	preview := callEdit(t, reg, map[string]interface{}{
		"path": path, "old_text": "alpha", "new_text": "ALPHA", "dry_run": true,
	})
	if preview.IsError {
		t.Fatalf("preview: %s", resultText(t, preview))
	}
	text := resultText(t, preview)
	predLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "predicted_hash: ") {
			predLine = strings.TrimPrefix(line, "predicted_hash: ")
			break
		}
	}
	if predLine == "" {
		t.Fatalf("missing predicted_hash:\n%s", text)
	}
	if string(fileBytes(t, path)) != original {
		t.Fatal("preview wrote the file")
	}

	apply := callEdit(t, reg, map[string]interface{}{
		"path": path, "old_text": "alpha", "new_text": "ALPHA",
	})
	if apply.IsError {
		t.Fatalf("apply: %s", resultText(t, apply))
	}
	m, ok := apply.StructuredContent.(map[string]any)
	if !ok {
		t.Fatal("missing structured content")
	}
	got, _ := m["content_hash"].(string)
	if got != predLine {
		t.Fatalf("predicted_hash %s != applied content_hash %s", predLine, got)
	}
}

func TestEditFile_OCC_BlocksAllModes(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "occ.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}

	modes := []map[string]interface{}{
		{"path": path, "old_text": "one", "new_text": "ONE", "expected_hash": "deadbeef"},
		{"path": path, "mode": "replace_range", "start_line": float64(1), "end_line": float64(1), "new_text": "X", "expected_hash": "deadbeef"},
		{"path": path, "mode": "delete_range", "start_line": float64(1), "end_line": float64(1), "expected_hash": "deadbeef"},
		{"path": path, "old_text": "one", "new_text": "ONE", "occurrence": float64(1), "expected_hash": "deadbeef"},
		{"path": path, "mode": "search_replace", "pattern": "one", "replacement": "ONE", "expected_hash": "deadbeef"},
		{"path": path, "mode": "insert", "anchor": "one", "new_text": "IN", "expected_hash": "deadbeef"},
	}
	original := fileBytes(t, path)
	for i, args := range modes {
		res := callEdit(t, reg, args)
		if !res.IsError {
			t.Fatalf("mode %d: expected OCC error, got %s", i, resultText(t, res))
		}
		if !strings.Contains(resultText(t, res), "stale edit") {
			t.Fatalf("mode %d: want stale edit, got %s", i, resultText(t, res))
		}
		if string(fileBytes(t, path)) != string(original) {
			t.Fatalf("mode %d mutated the file under OCC reject", i)
		}
	}
}

func TestEditFile_AutoOCC_Block_RangeAndMultiEdit(t *testing.T) {
	core.SetAutoOCCMode("block")
	defer core.SetAutoOCCMode("warn")

	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "auto.txt")
	if err := os.WriteFile(path, []byte("alpha beta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.readFileHandler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "read_file", Arguments: map[string]interface{}{"path": path},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("alpha beta gamma\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rangeRes := callEdit(t, reg, map[string]interface{}{
		"path": path, "mode": "replace_range", "start_line": float64(1), "end_line": float64(1), "new_text": "X",
	})
	if !rangeRes.IsError || !strings.Contains(resultText(t, rangeRes), "changed on disk") {
		t.Fatalf("replace_range auto-OCC block: %s", resultText(t, rangeRes))
	}
	if string(fileBytes(t, path)) != "alpha beta gamma\n" {
		t.Fatal("auto-OCC block mutated the file")
	}

	me := reg.handlers["multi_edit"]
	res, err := me(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "multi_edit",
		Arguments: map[string]interface{}{
			"path": path, "edits_json": `[{"old_text":"alpha","new_text":"ALPHA"}]`,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "changed on disk") {
		t.Fatalf("multi_edit auto-OCC block: %s", resultText(t, res))
	}
}

func TestReadOnly_BlocksArtifactWrite_AllowsStatus(t *testing.T) {
	if !toolIsMutating("server_info", map[string]interface{}{"action": "artifact", "sub_action": "write"}) {
		t.Fatal("artifact write must be mutating")
	}
	if toolIsMutating("server_info", map[string]interface{}{"action": "stats"}) {
		t.Fatal("stats is read-only")
	}
	if toolIsMutating("wsl", map[string]interface{}{"action": "status"}) {
		t.Fatal("wsl status is read-only")
	}
	if !toolIsMutating("wsl", map[string]interface{}{"action": "sync"}) {
		t.Fatal("wsl sync is mutating")
	}
	if toolIsMutating("git", map[string]interface{}{"action": "branch"}) {
		t.Fatal("git branch listing is read-only")
	}

	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ReadOnly = true
	h := reg.handlers["server_info"]
	res, err := h(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "server_info",
		Arguments: map[string]interface{}{
			"action": "artifact", "sub_action": "write", "path": filepath.Join(dir, "art.txt"),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "READ_ONLY") {
		t.Fatalf("artifact write under --readonly: %s", resultText(t, res))
	}
}

func TestSearchFiles_FileTypesAndCountOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("needle in go\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("needle in txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	h := reg.searchFilesHandler

	call := func(args map[string]interface{}) string {
		t.Helper()
		res, err := h(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name: "search_files", Arguments: args,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("search error: %s", resultText(t, res))
		}
		return resultText(t, res)
	}

	zero := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "include_content": true, "file_types": ".xyz",
	})
	if strings.Contains(strings.ToLower(zero), "a.go") || strings.Contains(strings.ToLower(zero), "b.txt") {
		t.Fatalf("nonexistent extension should match nothing, got:\n%s", zero)
	}

	count := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "count_only": true, "file_types": ".go",
	})
	if !strings.Contains(count, "1") {
		t.Fatalf("count_only + .go should be 1, got:\n%s", count)
	}
	countAll := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "count_only": true,
	})
	if !strings.Contains(countAll, "2") {
		t.Fatalf("count_only without filter should be 2, got:\n%s", countAll)
	}

	glob := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "include_content": true, "include": "*.go",
	})
	if !strings.Contains(glob, "a.go") {
		t.Fatalf("include *.go should find a.go, got:\n%s", glob)
	}
	if strings.Contains(glob, "b.txt") {
		t.Fatalf("include *.go should skip b.txt, got:\n%s", glob)
	}

	noCtx := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "include_content": true, "include_context": true, "context_lines": float64(0),
		"file_types": ".go",
	})
	if strings.Contains(noCtx, "Context:") {
		t.Fatalf("context_lines:0 must not return context, got:\n%s", noCtx)
	}
}

func TestEditFile_SearchReplace_DryRunCountAndHeader(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	path := filepath.Join(dir, "sr.txt")
	original := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	res := callEdit(t, reg, map[string]interface{}{
		"path": path, "mode": "search_replace", "pattern": "line", "replacement": "LINE", "dry_run": true,
	})
	if res.IsError {
		t.Fatalf("dry_run errored: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "DRY RUN — No changes made") {
		t.Fatalf("want unified dry-run header, got:\n%s", text)
	}
	if !strings.Contains(text, "Would change: 5 replacement(s)") {
		t.Fatalf("want 5 replacements, got:\n%s", text)
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatal("missing structured content")
	}
	switch n := m["replacements"].(type) {
	case int:
		if n != 5 {
			t.Fatalf("structured replacements=%d, want 5", n)
		}
	case float64:
		if n != 5 {
			t.Fatalf("structured replacements=%v, want 5", n)
		}
	default:
		t.Fatalf("replacements type %T", n)
	}
	if string(fileBytes(t, path)) != original {
		t.Fatal("dry_run wrote the file")
	}
}

func TestMultiEdit_DryRun_HashesAndLineCount(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "me.txt")
	original := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	h := reg.handlers["multi_edit"]
	res, err := h(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "multi_edit",
		Arguments: map[string]interface{}{
			"path": path, "dry_run": true,
			"edits_json": `[{"old_text":"alpha\n","new_text":"ALPHA\n"}]`,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("multi_edit dry_run: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "current_hash:") || !strings.Contains(text, "predicted_hash:") {
		t.Fatalf("missing hashes:\n%s", text)
	}
	if strings.Contains(text, "Lines affected: 3") {
		t.Fatalf("trailing newline counted as a line:\n%s", text)
	}
	if !strings.Contains(text, "Lines affected: 1") && !strings.Contains(text, "1 lines affected") {
		t.Fatalf("want 1 line affected (not phantom), got:\n%s", text)
	}
	if string(fileBytes(t, path)) != original {
		t.Fatal("dry_run wrote the file")
	}
}
