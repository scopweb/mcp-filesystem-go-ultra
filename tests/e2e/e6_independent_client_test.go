//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_E6_IndependentJSONRPCClient(t *testing.T) {
	exe := buildServer(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hit.go"), []byte("package hit\nfunc Target() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := startRaw(t, exe, dir)

	listed := c.rpc(t, "tools/list", map[string]any{})
	result, _ := listed["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 24 {
		t.Fatalf("tools/list count=%d", len(tools))
	}

	wpath := filepath.Join(dir, "w.txt")
	w := c.tool(t, "write_file", map[string]any{"path": wpath, "content": "hello e6\n"})
	mustRawOK(t, "write_file", w)
	sc := rawStructured(w)
	if sc["status"] != "applied" {
		t.Fatalf("write status=%v (independent client must see structuredContent)", sc["status"])
	}

	r := c.tool(t, "read_file", map[string]any{"path": wpath})
	mustRawOK(t, "read_file", r)
	rsc := rawStructured(r)
	if rsc["content_hash"] == nil || rsc["truncated"] == nil {
		t.Fatalf("read structured=%#v", rsc)
	}

	empty := c.tool(t, "search_files", map[string]any{
		"path": dir, "pattern": "ZZZ_NO_MATCH", "include_content": true,
	})
	if rawIsError(empty) {
		t.Fatalf("empty search isError: %s", rawText(empty))
	}
	esc := rawStructured(empty)
	if esc["status"] != "empty" {
		t.Fatalf("empty status=%v", esc["status"])
	}

	hit := c.tool(t, "search_files", map[string]any{
		"path": dir, "pattern": "Target", "include_content": true, "file_types": ".go", "output_format": "json",
	})
	mustRawOK(t, "search", hit)
	if n, _ := asInt(rawStructured(hit)["match_count"]); n == 0 {
		t.Fatalf("search matches=%#v", rawStructured(hit))
	}

	lst := c.tool(t, "list_directory", map[string]any{"path": dir})
	mustRawOK(t, "list", lst)
	if rawStructured(lst)["truncated"] == nil {
		t.Fatalf("list structured=%#v", rawStructured(lst))
	}

	missing := c.tool(t, "read_file", map[string]any{"path": filepath.Join(dir, "nope.txt")})
	if !rawIsError(missing) {
		t.Fatal("missing file must be isError")
	}
	txt := rawText(missing)
	if !strings.Contains(txt, `"retryable"`) {
		t.Fatalf("error envelope missing retryable: %s", txt)
	}
}
