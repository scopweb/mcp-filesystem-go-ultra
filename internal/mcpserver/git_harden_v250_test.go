package mcpserver

// Regression tests for the git hardening aligned with mcp-server-git
// v2.5.0 (2026.11.18), which fixed eight vulnerabilities across
// mcp-server-git, server-filesystem and server-everything.
//
// Each test names the advisory it pins.

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// pairValue returns the value of key in pairs, or "<missing>".
func pairValue(pairs [][2]string, key string) string {
	for _, kv := range pairs {
		if kv[0] == key {
			return kv[1]
		}
	}
	return "<missing>"
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- GHSA-h2g3-rwxm-xfm3: automatic maintenance must not start ---

// A merge (or any command that ends in "git maintenance run --auto")
// starts git's automatic maintenance, whose cruft repacks run each command
// the repository's gc.recentObjectsHook names. Stopping the launch covers
// every task; gc.auto is a backstop for the gc task only.
func TestGitSafeConfigPairs_StopsAutomaticMaintenance(t *testing.T) {
	pairs := gitSafeConfigPairs(t.TempDir())
	want := map[string]string{
		"maintenance.auto":    "false",
		"gc.auto":             "0",
		"core.untrackedCache": "false",
		"diff.submodule":      "short",
	}
	for key, value := range want {
		if got := pairValue(pairs, key); got != value {
			t.Fatalf("%s=%q, want %q", key, got, value)
		}
	}
}

// --- GHSA-87qg-cmw8-jc5m: lazy fetch off ---

func TestGitSafeEnviron_NoLazyFetch(t *testing.T) {
	env := gitSafeEnviron(gitSafeConfigPairs(t.TempDir()))
	found := false
	for _, e := range env {
		if e == "GIT_NO_LAZY_FETCH=1" {
			found = true
			continue
		}
		if strings.HasPrefix(e, "GIT_NO_LAZY_FETCH=") {
			t.Fatalf("GIT_NO_LAZY_FETCH overridden: %q", e)
		}
	}
	if !found {
		t.Fatal("GIT_NO_LAZY_FETCH=1 missing from the git environment")
	}
}

func TestParseGitVersion_HonorsNoLazyFetch(t *testing.T) {
	cases := []struct {
		in     string
		major  int
		minor  int
		patch  int
		honors bool
	}{
		{"git version 2.56.1.windows.1", 2, 56, 1, true},
		{"git version 2.46.0", 2, 46, 0, true},
		{"git version 2.45.1", 2, 45, 1, true},
		{"git version 2.45.0", 2, 45, 0, false},
		{"git version 2.44.1", 2, 44, 1, true},
		{"git version 2.44.0", 2, 44, 0, false},
		{"git version 2.39.4", 2, 39, 4, true},
		{"git version 2.39.3", 2, 39, 3, false},
		{"git version 2.38.0", 2, 38, 0, false},
	}
	for _, c := range cases {
		v := parseGitVersion(c.in)
		if v.major != c.major || v.minor != c.minor || v.patch != c.patch {
			t.Fatalf("%q parsed as %d.%d.%d, want %d.%d.%d", c.in, v.major, v.minor, v.patch, c.major, c.minor, c.patch)
		}
		if got := v.honorsNoLazyFetch(); got != c.honors {
			t.Fatalf("%q honorsNoLazyFetch=%v, want %v", c.in, got, c.honors)
		}
	}
}

func TestRepoHasPromisorRemote(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q")
	has, err := repoHasPromisorRemote(context.Background(), dir)
	if err != nil {
		t.Fatalf("fresh repo: %v", err)
	}
	if has {
		t.Fatal("a fresh repository declares a promisor remote")
	}
	mustGit(t, dir, "config", "remote.origin.promisor", "true")
	has, err = repoHasPromisorRemote(context.Background(), dir)
	if err != nil {
		t.Fatalf("promisor repo: %v", err)
	}
	if !has {
		t.Fatal("promisor remote not detected")
	}
}

// --- GHSA-pm7p-vphh-2cg7: a driver with an empty name is still emptied ---

// A driver's name may be empty (`[filter ""]`, read back as
// `filter..clean`, selected by `filter=`), so the name is matched with
// `.*`, not `.+`.
func TestLocalDriverOverrides_EmptyDriverName(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "--local", "filter..clean", "echo pwned")
	mustGit(t, dir, "config", "--local", "merge..driver", "echo pwned")

	pairs := localDriverOverrides(context.Background(), dir)
	for _, key := range []string{"filter..clean", "merge..driver"} {
		if pairValue(pairs, key) != "" {
			t.Fatalf("%s not emptied: %v", key, pairs)
		}
	}
}

// The end-to-end case: with the driver emptied, the program it names is
// never run, even when a .gitattributes selects it by its empty name.
func TestExecGitCommand_EmptyFilterDriverCannotRun(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran.txt")
	exe := buildMarkerExe(t, marker)
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "t@example.com")
	mustGit(t, dir, "config", "user.name", "T")
	mustGit(t, dir, "config", "filter..clean", exe)
	writeFileT(t, filepath.Join(dir, ".gitattributes"), "a.txt filter=\n")
	writeFileT(t, filepath.Join(dir, "a.txt"), "a\n")

	if _, err := execGitCommand(dir, "git", "add", "--", "a.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the empty-named filter driver ran a program (marker %v)", err)
	}
}

// --- the worktree scope is the repository's own too ---

// A linked worktree's config.worktree travels with the repository, so its
// drivers are emptied as well; the scopes the user wrote (system, global)
// are left alone.
func TestLocalDriverOverrides_WorktreeScope(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	mustGit(t, root, "init", "-q")
	mustGit(t, root, "config", "user.email", "t@example.com")
	mustGit(t, root, "config", "user.name", "T")
	writeFileT(t, filepath.Join(root, "a.txt"), "a\n")
	mustGit(t, root, "add", "a.txt")
	mustGit(t, root, "commit", "-q", "-m", "init")
	mustGit(t, root, "config", "extensions.worktreeConfig", "true")

	wt := filepath.Join(root, "wt")
	mustGit(t, root, "worktree", "add", "-q", wt, "-b", "wtbranch")
	mustGit(t, wt, "config", "--worktree", "filter.wt.clean", "echo pwned")

	pairs := localDriverOverrides(context.Background(), wt)
	if pairValue(pairs, "filter.wt.clean") != "" {
		t.Fatalf("worktree-scope driver not emptied: %v", pairs)
	}
}

// --- GHSA-9vjp-x34c-xg6v: drivers that appear after a write ---

// A command that writes the working tree can create or change
// configuration, so the drivers are read again before the next command.
func TestLocalDriverOverrides_ReenumeratesAfterWrite(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q")

	pairs := localDriverOverrides(context.Background(), dir)
	if pairValue(pairs, "filter.late.clean") != "<missing>" {
		t.Fatalf("unexpected driver before the write: %v", pairs)
	}
	mustGit(t, dir, "config", "--local", "filter.late.clean", "echo pwned")
	pairs = localDriverOverrides(context.Background(), dir)
	if pairValue(pairs, "filter.late.clean") != "" {
		t.Fatalf("driver created by a write not emptied: %v", pairs)
	}
}

// --- GHSA-8wv7-r664-7594: a submodule's own drivers ---

// A submodule is found at each gitlink in the index, then inside it at
// each of its own, the way git descends.
func TestSubmoduleDriverOverrides(t *testing.T) {
	requireGit(t)
	base := t.TempDir()

	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, sub, "init", "-q")
	mustGit(t, sub, "config", "user.email", "t@example.com")
	mustGit(t, sub, "config", "user.name", "T")
	writeFileT(t, filepath.Join(sub, "f.txt"), "f\n")
	mustGit(t, sub, "add", "f.txt")
	mustGit(t, sub, "commit", "-q", "-m", "sub")

	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init", "-q")
	mustGit(t, repo, "config", "user.email", "t@example.com")
	mustGit(t, repo, "config", "user.name", "T")
	mustGit(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	mustGit(t, repo, "commit", "-q", "-m", "with submodule")

	// The submodule's own configuration names a filter driver.
	mustGit(t, filepath.Join(repo, "sub"), "config", "filter.sub.clean", "echo pwned")

	pairs := submoduleDriverOverrides(context.Background(), repo)
	if pairValue(pairs, "filter.sub.clean") != "" {
		t.Fatalf("submodule driver not emptied: %v", pairs)
	}
}

// --- GHSA-r2q8-f657-4r56: an index entry outside the working tree ---

func TestUnsafeIndexEntry(t *testing.T) {
	cases := []struct {
		entry  string
		unsafe bool
	}{
		{"a.txt", false},
		{"dir/a.txt", false},
		{"dir/sub/a.txt", false},
		{"../outside/secret.txt", true},
		{"a/../b", true},
		{"/abs/path", true},
		{"", true},
		{".git/config", true},
		{"./.git/config", true},
	}
	for _, c := range cases {
		if got := unsafeIndexEntry(c.entry); got != c.unsafe {
			t.Fatalf("unsafeIndexEntry(%q)=%v, want %v", c.entry, got, c.unsafe)
		}
	}
	if runtime.GOOS == "windows" {
		windows := []struct {
			entry  string
			unsafe bool
		}{
			{`C:/Windows/system32`, true},
			{`dir\..\b`, true},
			{`..\outside`, true},
			{".git.", true},
			{"...", true},
			{"a.txt", false},
		}
		for _, c := range windows {
			if got := unsafeIndexEntry(c.entry); got != c.unsafe {
				t.Fatalf("unsafeIndexEntry(%q)=%v, want %v", c.entry, got, c.unsafe)
			}
		}
	}
}

// Git reads each entry's file at the working tree joined with the entry,
// and checks the entry only when it writes one (git add), never when it
// reads the index. An index written by hand can therefore name a path
// outside the working tree.
func TestGitRepoSafetyCheck_RefusesIndexEntryOutsideWorktree(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "t@example.com")
	mustGit(t, dir, "config", "user.name", "T")
	writeFileT(t, filepath.Join(dir, "a.txt"), "a\n")
	mustGit(t, dir, "add", "a.txt")
	mustGit(t, dir, "commit", "-q", "-m", "init")

	forgeGitIndex(t, filepath.Join(dir, ".git"), []string{"../outside/secret.txt"})

	err := gitRepoSafetyCheck(context.Background(), dir)
	if err == nil {
		t.Fatal("an index naming a path outside the working tree was accepted")
	}
	if !strings.Contains(err.Error(), "outside the working tree") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// forgeGitIndex writes an index (version 2) whose entries are paths, as
// is. Each entry points at the empty blob. `git update-index` refuses such
// paths, so the file is written byte by byte.
func forgeGitIndex(t *testing.T, gitDir string, paths []string) {
	t.Helper()
	blob := sha1.Sum([]byte("blob 0\x00"))
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)

	var body []byte
	for _, p := range sorted {
		entry := make([]byte, 0, 64+len(p))
		for _, field := range []uint32{0, 0, 0, 0, 0, 0, 0o100644, 0, 0, 0} {
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], field)
			entry = append(entry, b[:]...)
		}
		entry = append(entry, blob[:]...)
		var plen [2]byte
		binary.BigEndian.PutUint16(plen[:], uint16(len(p)))
		entry = append(entry, plen[:]...)
		entry = append(entry, []byte(p)...)
		for len(entry)%8 != 0 {
			entry = append(entry, 0)
		}
		body = append(body, entry...)
	}

	data := []byte("DIRC")
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], 2)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(sorted)))
	data = append(data, header[:]...)
	data = append(data, body...)
	sum := sha1.Sum(data)
	data = append(data, sum[:]...)

	if err := os.WriteFile(filepath.Join(gitDir, "index"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- GHSA-8wv7-r664-7594: diff does not look inside a submodule ---

func TestHardenGitArgv_DiffIgnoresSubmodules(t *testing.T) {
	argv := hardenGitArgv([]string{"diff"}, gitSafeConfigPairs(t.TempDir()))
	if !hasArg(argv, "--ignore-submodules=dirty") {
		t.Fatalf("diff does not ignore submodules: %q", argv)
	}
	argv = hardenGitArgv([]string{"diff", "--cached"}, gitSafeConfigPairs(t.TempDir()))
	if hasArg(argv, "--ignore-submodules=dirty") {
		t.Fatalf("a staged diff compares the index with HEAD and looks nowhere: %q", argv)
	}
}
