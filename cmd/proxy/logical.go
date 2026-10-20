package main

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	gitSHASuffix  = regexp.MustCompile(`(?i)^(.+)-([0-9a-f]{7,40})$`)
	versionSuffix = regexp.MustCompile(`(?i)^(.+)-v\d`)
)

func exeBasename(pathOrBase string) string {
	base := filepath.Base(strings.ReplaceAll(pathOrBase, `\`, `/`))
	return strings.TrimSuffix(base, ".exe")
}

func logicalServerName(pathOrBase string) string {
	base := exeBasename(pathOrBase)
	if m := gitSHASuffix.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	if m := versionSuffix.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	return base
}

// companionSuffixes name sibling binaries that share the server's logical
// prefix but are not the server: the proxy must never reap them.
var companionSuffixes = []string{"dashboard", "proxy"}

// sameLogicalServer reports whether procName is a build of the logical server
// (exact name, or name-<suffix>). Companion binaries such as
// filesystem-ultra-v4-dashboard.exe are excluded even though they share the
// prefix.
func sameLogicalServer(procName, logical string) bool {
	n := strings.ToLower(strings.TrimSuffix(procName, ".exe"))
	l := strings.ToLower(logical)
	if l == "" {
		return false
	}
	if n == l {
		return true
	}
	if !strings.HasPrefix(n, l+"-") {
		return false
	}
	rest := n[len(l)+1:]
	for _, c := range companionSuffixes {
		if strings.Contains(rest, c) {
			return false
		}
	}
	return true
}

// gitHashOf returns the git-sha suffix of a hashed build name
// (filesystem-ultra-c00a9cc.exe → "c00a9cc"), or "" for plain and versioned
// builds (filesystem-ultra.exe, filesystem-ultra-v4-embed_rg.exe).
func gitHashOf(pathOrBase string) string {
	if m := gitSHASuffix.FindStringSubmatch(exeBasename(pathOrBase)); m != nil {
		return strings.ToLower(m[2])
	}
	return ""
}
