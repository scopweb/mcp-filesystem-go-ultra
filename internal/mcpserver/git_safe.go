package mcpserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// gitNeutralHooksDir is an empty directory this process owns. Pointing
// core.hooksPath at it stops a repository's .git/hooks from running.
// One directory per process is enough: it is never given a hook.
var gitNeutralHooksDir = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "fsultra-githooks-")
	if err != nil || dir == "" {
		return os.DevNull
	}
	return dir
})

// gitSafeConfigPairs are the overrides from mcp-server-git 2026.11.11
// (GHSA-6hqx-gqrx-c5r2), extended with the ones mcp-server-git v2.5.0
// (2026.11.18) added. They are applied with -c (wins over every config
// file for this process) and GIT_CONFIG_* (inherited by Git children;
// requires Git 2.31). A later caller -c is placed first, so these win.
//
// The four added in v2.5.0:
//   - core.untrackedCache=false (GHSA-r2q8-f657-4r56): the cache names no
//     program, but it lives in the repository's own index and lists
//     directories by name, so a forged one names ".." and lists what is
//     outside the working tree.
//   - diff.submodule=short (GHSA-8wv7-r664-7594): "diff" makes git diff run
//     a nested git diff inside each changed submodule, without the
//     --no-ext-diff/--no-textconv flags, so the submodule's own
//     diff.external and textconv drivers would run.
//   - maintenance.auto=false and gc.auto=0 (GHSA-h2g3-rwxm-xfm3): a merge
//     (or any command ending in "git maintenance run --auto") starts git's
//     automatic maintenance, whose cruft repacks run each command the
//     repository's gc.recentObjectsHook names. Stopping the launch covers
//     every task; gc.auto is a backstop for the gc task only.
func gitSafeConfigPairs(hooks string) [][2]string {
	return [][2]string{
		{"core.fsmonitor", "false"},
		{"core.hooksPath", hooks},
		{"diff.external", ""},
		{"log.showSignature", "false"},
		{"commit.gpgSign", "false"},
		{"merge.verifySignatures", "false"},
		{"submodule.recurse", "false"},
		{"status.submoduleSummary", "false"},
		{"merge.autoStash", "false"},
		{"core.pager", ""},
		{"core.untrackedCache", "false"},
		{"diff.submodule", "short"},
		{"maintenance.auto", "false"},
		{"gc.auto", "0"},
	}
}

// gitDriverPattern matches the command-bearing configuration keys a
// repository's own config may define, the way mcp-server-git v2.5.0 lists
// them. A driver's name may be empty (`[filter ""]`, read back as
// `filter..clean`, selected by `filter=`), so the name part is `.*`, not
// `.+` (GHSA-pm7p-vphh-2cg7).
const gitDriverPattern = `^(filter\..*\.(clean|smudge|process)|merge\..*\.driver|diff\..*\.(command|textconv)|branch\..+\.mergeoptions)$`

// gitDriverKey matches the same keys in Go, for the listing this process
// parses back.
var gitDriverKey = regexp.MustCompile(gitDriverPattern)

// trustedGitScope are the config scopes the user wrote rather than the
// repository: their drivers (git-lfs, say) are the user's own and keep
// working. The "command" scope is what this process sets with -c.
var trustedGitScope = map[string]bool{
	"system":  true,
	"global":  true,
	"command": true,
}

func isGitBin(command string) bool {
	base := strings.ToLower(filepath.Base(command))
	return base == "git" || base == "git.exe"
}

// applyGitHardening returns the argv and environment for one git invocation.
// Non-git commands are returned unchanged. enumerate adds empty overrides for
// command-bearing keys defined in this repository's own config (filter,
// merge and diff drivers) and in the config of each checked-out submodule.
// Global drivers such as Git LFS are left alone.
//
// The drivers are read again for every invocation, so a command that writes
// the working tree cannot leave a driver that a later command in the same
// call would run (GHSA-9vjp-x34c-xg6v).
func applyGitHardening(ctx context.Context, dir, command string, args []string, enumerate bool) ([]string, []string) {
	if !isGitBin(command) {
		return args, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var extra [][2]string
	if enumerate {
		extra = localDriverOverrides(ctx, dir)
		extra = append(extra, submoduleDriverOverrides(ctx, dir)...)
	}
	pairs := append(gitSafeConfigPairs(gitNeutralHooksDir()), extra...)
	return hardenGitArgv(args, pairs), gitSafeEnviron(pairs)
}

func hardenGitArgv(args []string, pairs [][2]string) []string {
	globals, rest := splitGitGlobals(args)
	rest = insertSubcommandFlags(rest)
	argv := make([]string, 0, 1+len(globals)+len(pairs)*2+len(rest))
	argv = append(argv, "--no-pager")
	argv = append(argv, globals...)
	for _, kv := range pairs {
		argv = append(argv, "-c", kv[0]+"="+kv[1])
	}
	return append(argv, rest...)
}

func splitGitGlobals(args []string) (globals, rest []string) {
	for len(args) > 0 {
		a := args[0]
		switch {
		case a == "-c" || a == "-C" || a == "--git-dir" || a == "--work-tree":
			if len(args) < 2 {
				return globals, args
			}
			globals = append(globals, args[0], args[1])
			args = args[2:]
		case strings.HasPrefix(a, "--git-dir=") || strings.HasPrefix(a, "--work-tree="):
			globals = append(globals, a)
			args = args[1:]
		case a == "--no-pager" || a == "--bare" || a == "--literal-pathspecs" || a == "--no-optional-locks":
			globals = append(globals, a)
			args = args[1:]
		default:
			return globals, args
		}
	}
	return globals, args
}

func insertSubcommandFlags(args []string) []string {
	if len(args) == 0 {
		return args
	}
	switch args[0] {
	case "diff":
		extra := missingFlags(args, "--no-ext-diff", "--no-textconv")
		// A diff that compares the working tree looks inside each submodule
		// by running git status there, under the submodule's own
		// configuration, whose filter drivers this process cannot see.
		// `dirty` still reports a submodule whose checked-out commit moved.
		// A staged diff compares the index with HEAD and looks nowhere.
		if !hasArgPrefix(args, "--cached") && !hasArg(args, "--ignore-submodules=dirty") {
			extra = append(extra, "--ignore-submodules=dirty")
		}
		return insertAt(args, 1, extra)
	case "show", "log":
		return insertAt(args, 1, missingFlags(args, "--no-ext-diff", "--no-textconv"))
	case "status":
		if hasArgPrefix(args, "--ignore-submodules") {
			return args
		}
		return insertAt(args, 1, []string{"--ignore-submodules=dirty"})
	case "merge":
		return insertAt(args, 1, missingFlags(args, "--no-gpg-sign", "--no-verify-signatures"))
	case "stash":
		show := indexArg(args, "show")
		if show < 0 {
			return args
		}
		return insertAt(args, show+1, missingFlags(args, "--no-ext-diff", "--no-textconv"))
	default:
		return args
	}
}

func missingFlags(args []string, flags ...string) []string {
	var out []string
	for _, f := range flags {
		if !hasArg(args, f) {
			out = append(out, f)
		}
	}
	return out
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func hasArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if a == prefix || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

func indexArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func insertAt(args []string, at int, extra []string) []string {
	if len(extra) == 0 {
		return args
	}
	if at < 0 || at > len(args) {
		at = len(args)
	}
	out := make([]string, 0, len(args)+len(extra))
	out = append(out, args[:at]...)
	out = append(out, extra...)
	return append(out, args[at:]...)
}

// stripGitRepoEnv drops variables that would select a repository, so a
// command run in a neutral directory cannot still load that repo's config.
func stripGitRepoEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_WORK_TREE=") || strings.HasPrefix(e, "GIT_NAMESPACE=") {
			continue
		}
		if strings.HasPrefix(e, "GIT_TERMINAL_PROMPT=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GIT_TERMINAL_PROMPT=0")
}

func gitSafeEnviron(pairs [][2]string) []string {
	env := os.Environ()
	count := 0
	out := make([]string, 0, len(env)+2+len(pairs)*2)
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_CONFIG_COUNT=") {
			n, err := strconv.Atoi(strings.TrimPrefix(e, "GIT_CONFIG_COUNT="))
			if err == nil && n > count {
				count = n
			}
			continue
		}
		if strings.HasPrefix(e, "GIT_PAGER=") {
			continue
		}
		if strings.HasPrefix(e, "GIT_NO_LAZY_FETCH=") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "GIT_PAGER=")
	// A repository that declares a promisor remote makes git fetch any
	// object it is missing from that remote, by itself, on a read-only
	// command. The fetch runs a transport program the repository's
	// configuration names, and no override can pin it by name, since the
	// remote's name is the repository's choice. GIT_NO_LAZY_FETCH=1 turns
	// the fetch off, so a missing object is an error (GHSA-87qg-cmw8-jc5m).
	out = append(out, "GIT_NO_LAZY_FETCH=1")
	out = append(out, "GIT_CONFIG_COUNT="+strconv.Itoa(count+len(pairs)))
	for i, kv := range pairs {
		idx := count + i
		out = append(out,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", idx, kv[0]),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", idx, kv[1]),
		)
	}
	return out
}

// localDriverOverrides empties the command-bearing keys defined by this
// repository's own configuration: its .git/config, a worktree's
// config.worktree, and the files they include. The scopes the user wrote
// (system, global) and the command scope this process sets are left alone.
// The probe itself already carries the static overrides, so a core.fsmonitor
// on the repo cannot run while the list is being read.
func localDriverOverrides(ctx context.Context, dir string) [][2]string {
	out, err := runGitCaptured(ctx, dir, "git", []string{"config", "--show-scope", "-z", "--get-regexp", gitDriverPattern}, false)
	if err != nil {
		return nil
	}
	return scopedDriverPairs(out)
}

// scopedDriverPairs turns a `--show-scope -z` config listing into empty
// overrides for the command-bearing keys a repository defines. The listing
// is `<scope>\0<key>\n<value>\0` per entry.
func scopedDriverPairs(listing string) [][2]string {
	fields := strings.Split(listing, "\x00")
	seen := map[string]bool{}
	var pairs [][2]string
	for i := 0; i+1 < len(fields); i += 2 {
		scope := strings.ToLower(strings.TrimSpace(fields[i]))
		key, _, _ := strings.Cut(fields[i+1], "\n")
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || seen[key] || trustedGitScope[scope] {
			continue
		}
		if !gitDriverKey.MatchString(key) {
			continue
		}
		seen[key] = true
		pairs = append(pairs, [2]string{key, ""})
	}
	return pairs
}

// submoduleDriverOverrides empties the command-bearing keys defined by the
// configuration of each checked-out submodule. A submodule is found at each
// gitlink in the index (mode 160000) that has a .git entry, the way git
// descends into them. The overrides this process sets are inherited by the
// git git itself runs inside a submodule, so emptying the drivers by name
// works there too once their names are known.
//
// Listing them runs only `git config` and `git ls-files`, which start no
// program. A submodule whose configuration cannot be read is skipped: the
// nested git cannot read it either, and stops before it runs anything there.
func submoduleDriverOverrides(ctx context.Context, dir string) [][2]string {
	entries, err := gitIndexEntries(ctx, dir)
	if err != nil {
		return nil
	}
	var pairs [][2]string
	seen := map[string]bool{}
	for _, sub := range checkedOutSubmodules(dir, entries) {
		for _, kv := range submoduleDriverKeys(ctx, sub) {
			if seen[kv[0]] {
				continue
			}
			seen[kv[0]] = true
			pairs = append(pairs, kv)
		}
	}
	return pairs
}

// submoduleDriverKeys reads the command-bearing keys a submodule defines,
// under the submodule's own configuration.
func submoduleDriverKeys(ctx context.Context, subDir string) [][2]string {
	gitDir, err := submoduleGitDir(ctx, subDir)
	if err != nil {
		return nil
	}
	out, err := runGitCaptured(ctx, subDir, "git", []string{"--git-dir", gitDir, "config", "--show-scope", "-z", "--get-regexp", gitDriverPattern}, false)
	if err != nil {
		return nil
	}
	return scopedDriverPairs(out)
}

// submoduleGitDir resolves the git directory the nested git uses for
// subDir, the way git itself resolves it: a .git file is read and the
// directory it names is used, so no upward search makes the two disagree.
func submoduleGitDir(ctx context.Context, subDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--absolute-git-dir")
	cmd.Dir = subDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("no git directory for %s", subDir)
	}
	return dir, nil
}
