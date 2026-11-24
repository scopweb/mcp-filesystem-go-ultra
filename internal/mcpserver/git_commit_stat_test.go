package mcpserver

// Regression tests for two defects reported against the git tool:
//
//   - `git commit` reported "+0 -N": the summary always showed 0 insertions,
//     so the risk level was computed over a wrong number.
//   - `git status` returned only the counters: a client that renders
//     structuredContent could not see which files changed.
//
// The shortstat line git writes is
// " 3 files changed, 93 insertions(+), 18 deletions(-)": the insertions token
// carries the comma that separates it from the deletions part, so a parse by
// field position misses it whenever a deletions part follows.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseShortStat_Counts(t *testing.T) {
	cases := []struct {
		name                    string
		in                      string
		files, insertions, dels int
	}{
		{"mixed", " 1 file changed, 93 insertions(+), 18 deletions(-)", 1, 93, 18},
		{"insertions only", " 1 file changed, 3 insertions(+)", 1, 3, 0},
		{"deletions only", " 1 file changed, 12 deletions(-)", 1, 0, 12},
		{"many files", " 11 files changed, 353 insertions(+), 29 deletions(-)", 11, 353, 29},
		{"four files", " 4 files changed, 8 insertions(+), 60 deletions(-)", 4, 8, 60},
		{"empty", "", 0, 0, 0},
		{"garbage", "not a shortstat line", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, i, d := parseShortStat(tc.in)
			if f != tc.files || i != tc.insertions || d != tc.dels {
				t.Fatalf("parseShortStat(%q) = files=%d ins=%d del=%d; want %d/%d/%d",
					tc.in, f, i, d, tc.files, tc.insertions, tc.dels)
			}
		})
	}
}

// The commit summary must carry the real counters, not zeros.
func TestGitCommit_SummaryCountsInsertionsAndDeletions(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()

	path := dir + "/f.txt"
	if err := os.WriteFile(path, []byte("v1\nv2\nv3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "base")
	if err := os.WriteFile(path, []byte("v1\nv2\nv3\nv4\nv5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGitIn(t, dir, "add", "f.txt")

	res := callRegisteredGit(t, newRegisteredGitHandler(engine), map[string]interface{}{
		"action": "commit", "message": "probe: two more lines", "path": dir,
	})
	if res.IsError {
		t.Fatalf("commit failed: %s", mcpText(t, res))
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "Files: 1 | +2 -0") {
		t.Fatalf("commit summary does not report the real counters: %s", text)
	}
}

// A client that renders structuredContent must see the changed files.
func TestGitStatus_PayloadCarriesChangedFiles(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()

	if err := os.WriteFile(dir+"/tracked.txt", []byte("base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "base")
	if err := os.WriteFile(dir+"/tracked.txt", []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/new.txt", []byte("fresh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, output := range []string{"name-only", "full"} {
		t.Run(output, func(t *testing.T) {
			res := callRegisteredGit(t, newRegisteredGitHandler(engine), map[string]interface{}{
				"action": "status", "output": output, "path": dir,
			})
			if res.IsError {
				t.Fatalf("status failed: %s", mcpText(t, res))
			}
			m, ok := res.StructuredContent.(map[string]any)
			if !ok {
				t.Fatalf("no structured payload: %#v", res.StructuredContent)
			}
			files, _ := m["files"].([]map[string]any)
			if len(files) != 2 {
				t.Fatalf("payload does not list the changed files: %#v", m["files"])
			}
			byPath := map[string]map[string]any{}
			for _, f := range files {
				p, _ := f["path"].(string)
				byPath[p] = f
			}
			if e := byPath["tracked.txt"]; e == nil || e["code"] != " M" {
				t.Fatalf("tracked file missing or wrong code: %#v", byPath)
			}
			if e := byPath["new.txt"]; e == nil || e["code"] != "??" {
				t.Fatalf("untracked file missing or wrong code: %#v", byPath)
			}
		})
	}
}

// The payload is bounded by max_lines, and the omitted files are reported.
func TestGitStatus_PayloadFileListIsBounded(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()

	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(dir+"/"+name, []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	res := callRegisteredGit(t, newRegisteredGitHandler(engine), map[string]interface{}{
		"action": "status", "output": "name-only", "max_lines": float64(2), "path": dir,
	})
	if res.IsError {
		t.Fatalf("status failed: %s", mcpText(t, res))
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("no structured payload: %#v", res.StructuredContent)
	}
	files, _ := m["files"].([]map[string]any)
	if len(files) != 2 {
		t.Fatalf("payload file list is not bounded: %d", len(files))
	}
	if m["files_omitted"] != 1 {
		t.Fatalf("omitted files not reported: %#v", m)
	}
	if m["file_count"] != 3 {
		t.Fatalf("file_count must describe the whole scope: %#v", m)
	}
}

// mustGitIn runs a git command in dir and fails the test on error.
func mustGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
