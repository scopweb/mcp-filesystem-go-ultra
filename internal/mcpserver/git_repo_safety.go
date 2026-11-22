package mcpserver

// Repository-level safety checks, aligned with mcp-server-git v2.5.0
// (2026.11.18). The per-command hardening in git_safe.go keeps a
// repository's own configuration from running a program; these checks keep
// the repository itself from making git read or run anything outside it:
//
//   - an index entry naming a path outside the working tree
//     (GHSA-r2q8-f657-4r56): git reads each entry's file at the working
//     tree joined with the entry, and checks the entry only when it writes
//     one (git add), never when it reads the index.
//   - a promisor remote on a git too old to honor GIT_NO_LAZY_FETCH
//     (GHSA-87qg-cmw8-jc5m): such a git fetches the missing objects by
//     itself, through a transport program the repository's configuration
//     names.
//   - a checked-out submodule on a git older than 2.33: the nested git
//     would not inherit the configuration overrides that keep the
//     submodule's own drivers from running.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// gitVersionInfo is the parsed output of `git --version`.
type gitVersionInfo struct {
	major int
	minor int
	patch int
}

// gitVersion is read once per process: the git on PATH does not change
// while the server runs.
var gitVersion = sync.OnceValue(func() gitVersionInfo {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return gitVersionInfo{}
	}
	return parseGitVersion(string(out))
})

// parseGitVersion reads "git version 2.47.1.windows.1" into its numbers.
func parseGitVersion(s string) gitVersionInfo {
	fields := strings.Fields(strings.TrimSpace(s))
	ver := ""
	for i, f := range fields {
		if f == "version" && i+1 < len(fields) {
			ver = fields[i+1]
			break
		}
	}
	if ver == "" && len(fields) > 0 {
		ver = fields[len(fields)-1]
	}
	var v gitVersionInfo
	for i, part := range strings.Split(ver, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		switch i {
		case 0:
			v.major = n
		case 1:
			v.minor = n
		case 2:
			v.patch = n
		}
	}
	return v
}

// honorsNoLazyFetch reports whether this git honors GIT_NO_LAZY_FETCH.
// Git honors it from 2.45.1 and the maintenance releases made with it
// (2.39.4, 2.40.2, 2.41.1, 2.42.2, 2.43.4, 2.44.1); an older git ignores
// it, so a repository with a promisor remote is refused there.
func (v gitVersionInfo) honorsNoLazyFetch() bool {
	if v.major > 2 {
		return true
	}
	if v.major < 2 {
		return false
	}
	if v.minor > 45 {
		return true
	}
	since, ok := map[int]int{39: 4, 40: 2, 41: 1, 42: 2, 43: 4, 44: 1, 45: 1}[v.minor]
	return ok && v.patch >= since
}

// atLeast reports whether this git is at least major.minor.
func (v gitVersionInfo) atLeast(major, minor int) bool {
	if v.major != major {
		return v.major > major
	}
	return v.minor >= minor
}

// minSubmoduleGit is the first git whose nested git inherits the
// configuration overrides (GIT_CONFIG_COUNT reaches a submodule from 2.33).
var minSubmoduleGit = gitVersionInfo{major: 2, minor: 33}

// gitIndexEntry is one entry of a repository's index.
type gitIndexEntry struct {
	mode string
	path string
}

// gitIndexEntries lists the repository's index the way git uses it: a
// split index's shared part and a sparse index's collapsed directories are
// listed as git lists them, and `:(top)` lists the whole index wherever
// the command runs from.
func gitIndexEntries(ctx context.Context, repoRoot string) ([]gitIndexEntry, error) {
	out, err := runGitCaptured(ctx, repoRoot, "git", []string{"ls-files", "-z", "--stage", "--full-name", "--", ":(top)"}, false)
	if err != nil {
		if strings.TrimSpace(out) == "" {
			// Nothing matched: an empty index, or a repository with no
			// commits yet.
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read the index of %s: %w", repoRoot, err)
	}
	var entries []gitIndexEntry
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		info, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		mode := ""
		if fields := strings.Fields(info); len(fields) > 0 {
			mode = fields[0]
		}
		entries = append(entries, gitIndexEntry{mode: mode, path: path})
	}
	return entries, nil
}

// checkedOutSubmodules returns the directories of the submodules that are
// checked out under repoRoot: one per gitlink in the index (mode 160000)
// that has a .git entry. Git only looks inside a submodule that is checked
// out.
func checkedOutSubmodules(repoRoot string, entries []gitIndexEntry) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.mode != "160000" || e.path == "" {
			continue
		}
		sub := filepath.Join(repoRoot, filepath.FromSlash(e.path))
		if _, err := os.Stat(filepath.Join(sub, ".git")); err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(sub)
		if err != nil {
			continue
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		out = append(out, sub)
	}
	return out
}

// unsafeIndexEntry reports whether git would read the index entry from
// outside the working tree.
//
// Git reads each entry's file at the working tree joined with the entry,
// and checks the entry only when it writes one (git add), never when it
// reads the index. An entry is refused the way git add refuses it:
// absolute, or with an empty, `.`, `..` or `.git` component. On Windows
// `\` separates components too, a `:` makes a drive or stream path, and a
// component's trailing dots and spaces are dropped by the file system, so
// `...` or `.git.` count as well.
func unsafeIndexEntry(entry string) bool {
	windows := runtime.GOOS == "windows"
	parts := strings.Split(entry, "/")
	if windows {
		parts = strings.Split(strings.ReplaceAll(entry, "\\", "/"), "/")
	}
	for _, part := range parts {
		if windows {
			if strings.Contains(part, ":") {
				return true
			}
			part = strings.TrimRight(part, ". ")
		}
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
}

// gitPromisorPattern matches the keys that declare a promisor remote: a
// partial clone. Git also reads a `remote.promisor` with no remote name,
// as a promisor remote named "".
var gitPromisorPattern = `^(extensions\.partialclone|remote\.(.+\.)?(promisor|partialclonefilter))$`

// repoHasPromisorRemote reports whether the repository declares a promisor
// remote, from which git would fetch missing objects by itself.
func repoHasPromisorRemote(ctx context.Context, repoRoot string) (bool, error) {
	out, err := runGitCaptured(ctx, repoRoot, "git", []string{"config", "--get-regexp", gitPromisorPattern}, false)
	if err != nil {
		if strings.TrimSpace(out) == "" {
			// Exit 1 is "nothing matched": no promisor remote, nothing to
			// fetch.
			return false, nil
		}
		return false, fmt.Errorf("cannot read the configuration of %s: %w", repoRoot, err)
	}
	return strings.TrimSpace(out) != "", nil
}

// gitRepoSafetyCheck refuses a repository that git would read outside its
// working tree, that would fetch missing objects through a transport
// program its own configuration names, or whose checked-out submodule
// would run a driver this process cannot override.
func gitRepoSafetyCheck(ctx context.Context, repoRoot string) error {
	entries, err := gitIndexEntries(ctx, repoRoot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if unsafeIndexEntry(e.path) {
			return fmt.Errorf(
				"repository %s has an index entry %q that git would read outside the working tree",
				repoRoot, e.path)
		}
	}
	if !gitVersion().honorsNoLazyFetch() {
		promisor, err := repoHasPromisorRemote(ctx, repoRoot)
		if err != nil {
			return err
		}
		if promisor {
			return fmt.Errorf(
				"Git 2.45.1 or later (or 2.39.4, 2.40.2, 2.41.1, 2.42.2, 2.43.4, 2.44.1) " +
					"is required for a repository with a promisor remote (a partial clone): " +
					"an older git fetches missing objects from it, and the fetch runs a program " +
					"the repository's configuration names")
		}
	}
	if len(checkedOutSubmodules(repoRoot, entries)) > 0 && !gitVersion().atLeast(minSubmoduleGit.major, minSubmoduleGit.minor) {
		return fmt.Errorf(
			"Git 2.33 or later is required for a repository with a checked-out submodule: " +
				"an older git does not pass the configuration overrides to the git it runs inside one")
	}
	return nil
}
