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
	base := filepath.Base(pathOrBase)
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

func sameLogicalServer(procName, logical string) bool {
	n := strings.ToLower(strings.TrimSuffix(procName, ".exe"))
	l := strings.ToLower(logical)
	if l == "" {
		return false
	}
	return n == l || strings.HasPrefix(n, l+"-")
}
