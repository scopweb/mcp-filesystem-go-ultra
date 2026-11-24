package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// minGitLFSVersion is the first Git LFS release that fixes the four advisories
// published on 2026-11-20 (all High):
//
//	GHSA-x72p-hmqp-46hw  command execution through .lfsconfig
//	GHSA-xrgv-7vx6-5vx6  submodule config with an embedded LF
//	GHSA-w3f2-pr6x-wwjc  SSH URL handling
//	GHSA-x7xj-4j4f-88xr  Cookie forwarded after a redirect
//
// The server cannot patch the user's git-lfs binary, but it can tell the
// operator that the binary in PATH is the vulnerable one. The exposure is real
// here: the git hardening of v4.9.1 deliberately leaves the `system` and
// `global` scope drivers in place ("as with Git LFS"), so the machine's
// git-lfs runs on pull, on restore and on the checkout of a branch, in a
// repository the agent may have just cloned.
const minGitLFSVersion = "3.8.1"

// gitLFSAdvisories are the identifiers quoted in the warning, so the operator
// can look them up without a second search.
var gitLFSAdvisories = []string{
	"GHSA-x72p-hmqp-46hw",
	"GHSA-xrgv-7vx6-5vx6",
	"GHSA-w3f2-pr6x-wwjc",
	"GHSA-x7xj-4j4f-88xr",
}

// parseGitLFSVersion extracts the version from the output of
// `git lfs version`, which looks like:
//
//	git-lfs/3.8.1 (GitHub; windows amd64; go 1.27.3; git a662a6db)
//
// It returns the version and true when a version was found, and
// ("", false) otherwise. Only the numeric core is kept: the parenthesis
// carries the build metadata, not the version.
func parseGitLFSVersion(out string) (string, bool) {
	line := out
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "git-lfs/") {
		return "", false
	}
	rest := line[len("git-lfs/"):]
	end := strings.IndexAny(rest, " \t(")
	if end >= 0 {
		rest = rest[:end]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}

// versionParts splits a dotted version into its numeric parts. A part that is
// not a number (a "-rc1" suffix, for instance) stops the split: the numeric
// prefix is what matters for the comparison.
func versionParts(v string) []int {
	var parts []int
	for _, seg := range strings.Split(strings.TrimSpace(v), ".") {
		digits := seg
		for i, r := range seg {
			if r < '0' || r > '9' {
				digits = seg[:i]
				break
			}
		}
		if digits == "" {
			break
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		parts = append(parts, n)
	}
	return parts
}

// versionOlder reports whether v is strictly older than min. A version with
// fewer parts than min is compared on the parts it has: 3.8 is older than
// 3.8.1, and 3.8.1 is not older than 3.8.1.
func versionOlder(v, min string) bool {
	vp, mp := versionParts(v), versionParts(min)
	n := len(vp)
	if len(mp) < n {
		n = len(mp)
	}
	for i := 0; i < n; i++ {
		if vp[i] != mp[i] {
			return vp[i] < mp[i]
		}
	}
	return len(vp) < len(mp)
}

// gitLFSVersion runs `git lfs version` and returns the reported version. An
// error means there is nothing to check: git-lfs is not installed, or it is
// not a git-lfs that answers this way.
func gitLFSVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "lfs", "version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	v, ok := parseGitLFSVersion(string(out))
	if !ok {
		return "", errUnparsableGitLFSVersion
	}
	return v, nil
}

// errUnparsableGitLFSVersion is returned when `git lfs version` answers with
// something this build does not understand. It is not a failure of the check:
// the operator gets no warning, but the log records that the probe was
// inconclusive.
var errUnparsableGitLFSVersion = &lfsProbeError{"cannot parse the output of `git lfs version`"}

type lfsProbeError struct{ msg string }

func (e *lfsProbeError) Error() string { return e.msg }

// warnIfGitLFSVulnerable probes the git-lfs in PATH and logs a warning when
// it is older than minGitLFSVersion.
//
// The probe runs in its own goroutine and never blocks the startup: the
// server must be ready for the client, and `git lfs version` costs a process
// spawn that the operator's machine may be slow to answer.
func warnIfGitLFSVulnerable() {
	warnIfGitLFSVulnerableWith(slog.Default())
}

// warnIfGitLFSVulnerableWith is warnIfGitLFSVulnerable with an explicit
// logger. The logger is a parameter so the test can read the probe's output
// without swapping the process-wide logger, which would be a data race with
// the goroutine that writes into it.
func warnIfGitLFSVulnerableWith(logger *slog.Logger) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		v, err := gitLFSVersion(ctx)
		if err != nil {
			// No git-lfs in PATH: there is no driver to run, so there is
			// nothing to warn about. Any other failure is worth a line: the
			// operator cannot tell "checked and fine" from "not checked".
			if errors.Is(err, exec.ErrNotFound) {
				logger.Debug("git-lfs is not installed: no LFS driver can run")
				return
			}
			logger.Warn("could not read the version of git-lfs: the LFS drivers of scope system/global may be older than "+
				minGitLFSVersion+" ("+strings.Join(gitLFSAdvisories, ", ")+")",
				"error", err)
			return
		}
		if !versionOlder(v, minGitLFSVersion) {
			logger.Info("git-lfs version checked", "version", v, "minimum", minGitLFSVersion)
			return
		}
		logger.Warn("git-lfs in PATH is older than "+minGitLFSVersion+
			": the LFS drivers of scope system/global run on pull, restore and branch checkout. "+
			"Upgrade git-lfs ("+strings.Join(gitLFSAdvisories, ", ")+")",
			"version", v, "minimum", minGitLFSVersion)
	}()
}
