package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestReadFile_StructuredModeKeepsBatchBodyOnce(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha-body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("beta-body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ResponseMode = core.ResponseModeStructured
	res := callPatchTool(t, reg, "read_file", map[string]any{"paths": []any{a, b}})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "alpha-body") || strings.Contains(text, "beta-body") {
		t.Fatalf("text fallback repeated bodies:\n%s", text)
	}
	m, _ := res.StructuredContent.(map[string]any)
	if strings.Contains(m["content"].(string), "alpha-body") {
		t.Fatalf("top-level content repeated body: %#v", m["content"])
	}
	files, _ := m["files"].([]map[string]any)
	if len(files) != 2 {
		raw, _ := m["files"].([]any)
		if len(raw) != 2 {
			t.Fatalf("files %#v", m["files"])
		}
		for _, item := range raw {
			fm := item.(map[string]any)
			body, _ := fm["content"].(string)
			if body == "" || fm["content_hash"] == "" {
				t.Fatalf("missing body or hash: %#v", fm)
			}
		}
		return
	}
	for _, fm := range files {
		if fm["content"] == "" || fm["content_hash"] == "" {
			t.Fatalf("%#v", fm)
		}
	}
	if !strings.Contains(text, "structuredContent") {
		t.Fatalf("summary does not say where the body is: %s", text)
	}
	if !strings.Contains(text, "--response-mode=compat") {
		t.Fatalf("summary does not offer the compat fallback: %s", text)
	}
}

func TestReadFile_StructuredSummaryPointsToCompatFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("alpha-body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ResponseMode = core.ResponseModeStructured
	res := callPatchTool(t, reg, "read_file", map[string]any{"path": p})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "alpha-body") {
		t.Fatalf("text fallback repeated the body:\n%s", text)
	}
	if !strings.Contains(text, "structuredContent") {
		t.Fatalf("summary does not say where the body is: %s", text)
	}
	if !strings.Contains(text, "--response-mode=compat") {
		t.Fatalf("summary does not offer the compat fallback: %s", text)
	}
	m, _ := res.StructuredContent.(map[string]any)
	body, _ := m["content"].(string)
	if !strings.Contains(body, "alpha-body") {
		t.Fatalf("body missing from structuredContent: %#v", m)
	}
}

func TestReadFile_CompatKeepsTextBody(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("alpha-body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "read_file", map[string]any{"path": p})
	if !strings.Contains(resultText(t, res), "alpha-body") {
		t.Fatalf("compat dropped body: %s", resultText(t, res))
	}
}

// -----------------------------------------------------------------------------
// Listing, search and project map: --response-mode=structured keeps the body
// once, in structuredContent. compat keeps the historical text fallback.
// -----------------------------------------------------------------------------

func TestLineCensus_ResponseModesPreserveRows(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"alpha.txt": "one\ntwo\n", "beta.txt": "no newline"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{core.ResponseModeCompat, core.ResponseModeStructured} {
		for _, compact := range []bool{false, true} {
			reg := newHelpTestRegistry(t, dir)
			reg.engine.GetConfig().ResponseMode = mode
			reg.engine.GetConfig().CompactMode = compact
			for _, tc := range []struct {
				path      string
				limit     float64
				count     int
				truncated bool
			}{
				{dir, 20, 2, false},
				{dir, 1, 1, true},
				{filepath.Join(dir, "alpha.txt"), 20, 1, false},
			} {
				res := callPatchTool(t, reg, "list_directory", map[string]any{
					"path": tc.path, "output_format": "lines", "max_nodes": tc.limit,
				})
				if res.IsError {
					t.Fatal(resultText(t, res))
				}
				m := res.StructuredContent.(map[string]any)
				rows, _ := m["entries"].([]map[string]any)
				if len(rows) != tc.count || m["entry_count"] != tc.count || m["truncated"] != tc.truncated {
					t.Fatalf("mode=%s compact=%v: census metadata lost: %#v", mode, compact, m)
				}
				if m["status"] == "empty" || (tc.truncated && m["continuation"] == nil) {
					t.Fatalf("invalid census state: %#v", m)
				}
				for _, row := range rows {
					p, _ := row["path"].(string)
					want := 0
					switch filepath.Base(p) {
					case "alpha.txt":
						want = 2
					case "beta.txt":
					default:
						t.Fatalf("unexpected census path: %#v", row)
					}
					if row["lines"] != want {
						t.Fatalf("newline count lost: %#v", row)
					}
				}
				text := resultText(t, res)
				if strings.Contains(text, "2 "+core.NormalizePath(filepath.Join(dir, "alpha.txt"))) != (mode == core.ResponseModeCompat) {
					t.Fatalf("mode=%s: wrong census fallback: %s", mode, text)
				}
			}
		}
	}
}
func TestListDirectory_StructuredModeKeepsListingOnce(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("body\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ResponseMode = core.ResponseModeStructured
	res := callPatchTool(t, reg, "list_directory", map[string]any{"path": dir})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "alpha.txt") || strings.Contains(text, "beta.txt") {
		t.Fatalf("text fallback repeated the listing:\n%s", text)
	}
	if !strings.Contains(text, "structuredContent") || !strings.Contains(text, "--response-mode=compat") {
		t.Fatalf("summary does not point at the body and the fallback: %s", text)
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["presentation"] != core.ResponseModeStructured {
		t.Fatalf("presentation not marked: %#v", m)
	}
	entries, _ := m["entries"].([]map[string]any)
	if len(entries) != 2 {
		t.Fatalf("entries lost: %#v", m["entries"])
	}
	// The invariant "message == text content block" must hold in every mode.
	if msg, _ := m["message"].(string); msg != text {
		t.Fatalf("message != text block:\nmessage: %q\ntext: %q", msg, text)
	}
}

func TestListDirectory_CompatKeepsListingText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	res := callPatchTool(t, reg, "list_directory", map[string]any{"path": dir})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "alpha.txt") {
		t.Fatalf("compat dropped the listing: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if msg, _ := m["message"].(string); msg != resultText(t, res) {
		t.Fatalf("message != text block in compat mode")
	}
}

func TestSearchFiles_StructuredModeKeepsHitsOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("needle here\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ResponseMode = core.ResponseModeStructured
	res := callPatchTool(t, reg, "search_files", map[string]any{
		"path": dir, "pattern": "needle", "include_content": true,
	})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "needle here") {
		t.Fatalf("text fallback repeated the hits:\n%s", text)
	}
	m, _ := res.StructuredContent.(map[string]any)
	matches, _ := m["matches"].([]map[string]any)
	if len(matches) == 0 {
		t.Fatalf("hits lost: %#v", m)
	}
}

// projectMapHasStructure accepts the two shapes the skeleton can take in a
// structured payload: the typed entries and the plain maps.
func projectMapHasStructure(v any) bool {
	switch s := v.(type) {
	case []core.ProjectMapEntry:
		return len(s) > 0
	case []map[string]any:
		return len(s) > 0
	case []any:
		return len(s) > 0
	}
	return false
}

func TestProjectMap_StructuredModeKeepsSkeletonOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "alpha.txt"), []byte("body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ResponseMode = core.ResponseModeStructured
	res := callPatchTool(t, reg, "project_map", map[string]any{"path": dir})
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "alpha.txt") {
		t.Fatalf("text fallback repeated the skeleton:\n%s", text)
	}
	m, _ := res.StructuredContent.(map[string]any)
	if !projectMapHasStructure(m["structure"]) {
		t.Fatalf("skeleton lost: %#v", m)
	}
}
