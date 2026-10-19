//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestE2E_UndoPreviewCTemp(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows C:\\temp live regression")
	}
	root := `C:\temp`
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Skip("C:\\temp not present")
	}
	dir := liveTempDir(t, root, "fsu-undo-fixed")
	exe := buildServer(t)
	c := startRaw(t, exe, dir)
	path := filepath.Join(dir, "undo.txt")
	original := "alpha\r\nbeta\r\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("disk bytes=%q error=%v, want %q", got, err, want)
		}
	}
	res := c.tool(t, "edit_file", map[string]any{
		"path": path, "old_text": "beta", "new_text": "BETA",
	})
	if rawIsError(res) {
		t.Fatal(rawText(res))
	}
	check("alpha\r\nBETA\r\n")
	res = c.tool(t, "apply_patch", map[string]any{
		"path":  path,
		"patch": "--- a/undo.txt\n+++ b/undo.txt\n@@ -1,2 +1,2 @@\n-alpha\n+ALPHA\n BETA\n",
	})
	if rawIsError(res) {
		t.Fatal(rawText(res))
	}
	check("ALPHA\r\nBETA\r\n")
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"preview", "dry_run"} {
		res = c.tool(t, "backup", map[string]any{
			"action": "undo_last", "file_path": path, flag: true,
		})
		if rawIsError(res) {
			t.Fatal(rawText(res))
		}
		check("ALPHA\r\nBETA\r\n")
		after, err := os.Stat(path)
		if err != nil || !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("%s modified file timestamp: %v", flag, err)
		}
	}
	for _, want := range []string{"alpha\r\nBETA\r\n", original} {
		res = c.tool(t, "backup", map[string]any{
			"action": "undo_last", "file_path": path,
		})
		if rawIsError(res) {
			t.Fatal(rawText(res))
		}
		check(want)
	}
}
