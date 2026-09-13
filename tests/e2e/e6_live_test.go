//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_E6_CTemp(t *testing.T) {
	root := `C:\temp`
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Skip("C:\\temp not present")
	}
	workDir := filepath.Join(root, "fsu-e6-live")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	runLiveE6(t, workDir)
}

func runLiveE6(t *testing.T, workDir string) {
	t.Helper()
	exe := buildServer(t)
	if err := os.WriteFile(filepath.Join(workDir, "hit.go"), []byte("package hit\nfunc Target() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := startRaw(t, exe, workDir)

	listed := c.rpc(t, "tools/list", map[string]any{})
	result, _ := listed["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 25 {
		t.Fatalf("tools/list count=%d", len(tools))
	}

	wpath := filepath.Join(workDir, "w.txt")
	w := c.tool(t, "write_file", map[string]any{"path": wpath, "content": "hello e6 live\n"})
	mustRawOK(t, "write_file", w)
	if rawStructured(w)["status"] != "applied" {
		t.Fatalf("write status=%v", rawStructured(w)["status"])
	}

	r := c.tool(t, "read_file", map[string]any{"path": wpath})
	mustRawOK(t, "read_file", r)
	if rawStructured(r)["content_hash"] == nil {
		t.Fatalf("read missing hash: %#v", rawStructured(r))
	}

	empty := c.tool(t, "search_files", map[string]any{
		"path": workDir, "pattern": "ZZZ_NO_MATCH", "include_content": true,
	})
	if rawIsError(empty) || rawStructured(empty)["status"] != "empty" {
		t.Fatalf("empty search: isError=%v %#v", rawIsError(empty), rawStructured(empty))
	}

	hit := c.tool(t, "search_files", map[string]any{
		"path": workDir, "pattern": "Target", "include_content": true, "file_types": ".go", "output_format": "json",
	})
	mustRawOK(t, "search", hit)
	if n, _ := asInt(rawStructured(hit)["match_count"]); n == 0 {
		t.Fatalf("search matches=%#v", rawStructured(hit))
	}

	dry := c.tool(t, "edit_file", map[string]any{
		"path": wpath, "old_text": "hello", "new_text": "HELLO", "dry_run": true,
	})
	mustRawOK(t, "dry_run", dry)
	if rawStructured(dry)["status"] != "simulated" {
		t.Fatalf("dry_run status=%v", rawStructured(dry)["status"])
	}
	got, err := os.ReadFile(wpath)
	if err != nil || string(got) != "hello e6 live\n" {
		t.Fatalf("dry_run mutated disk: %q (%v)", got, err)
	}

	help := c.tool(t, "help", map[string]any{})
	mustRawOK(t, "help", help)
	if !strings.Contains(rawText(help), "retryable") {
		t.Fatalf("help missing retryable note: %s", rawText(help))
	}
}
