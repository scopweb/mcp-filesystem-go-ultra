package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
// (GHSA-6hqx-gqrx-c5r2). They are applied with -c (wins over every config
// file for this process) and GIT_CONFIG_* (inherited by Git children;
// requires Git 2.31). A later caller -c is placed first, so these win.
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
	}
}

func isGitBin(command string) bool {
	base := strings.ToLower(filepath.Base(command))
	return base == "git" || base == "git.exe"
}

// applyGitHardening returns the argv and environment for one git invocation.
// Non-git commands are returned unchanged. enumerate adds empty overrides for
// command-bearing keys defined in this repository's own config (filter,
// merge and diff drivers). Global drivers such as Git LFS are left alone.
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
	case "diff", "show", "log":
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
		out = append(out, e)
	}
	out = append(out, "GIT_PAGER=")
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

// localDriverOverrides empties command-bearing keys from this repository's
// config only. It does not enumerate again, and the probe itself already
// carries the static overrides, so a core.fsmonitor on the repo cannot run
// while the list is being read.
func localDriverOverrides(ctx context.Context, dir string) [][2]string {
	out, err := runGitCaptured(ctx, dir, "git", []string{"config", "--local", "--list"}, false)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	seen := map[string]bool{}
	var pairs [][2]string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		key, _, ok := strings.Cut(line, "=")
		if !ok || !repoLocalCommandKey(key) {
			continue
		}
		key = strings.ToLower(key)
		if seen[key] {
			continue
		}
		seen[key] = true
		pairs = append(pairs, [2]string{key, ""})
	}
	return pairs
}

func repoLocalCommandKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.HasPrefix(key, "filter.") && (strings.HasSuffix(key, ".clean") || strings.HasSuffix(key, ".smudge") || strings.HasSuffix(key, ".process")):
		return true
	case strings.HasPrefix(key, "merge.") && strings.HasSuffix(key, ".driver"):
		return true
	case strings.HasPrefix(key, "diff.") && (strings.HasSuffix(key, ".command") || strings.HasSuffix(key, ".textconv")):
		return true
	case strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".mergeoptions"):
		return true
	default:
		return false
	}
}
