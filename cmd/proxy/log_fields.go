package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// argumentKeys returns the NAMES of the arguments of a tool call, sorted.
//
// Only the names, never the values: a value can carry a path, a token, a
// message body or a secret. The names are what the weekly analysis needs to
// tell a bounded read (start_line/end_line) from an unbounded one, and a
// single file (path) from a batch (paths), without reading a single value.
//
// The proxy log used to carry the tool name and the path only, so the
// analysis had to guess why a read_file call had cost 30k tokens: a range, a
// batch, or a file read without a bound. With the names, that question has
// an answer.
func argumentKeys(args map[string]any) []string {
	if len(args) == 0 {
		return nil
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// targetProfile reads the tool profile of the wrapped server from its command
// line.
//
// The proxy wraps any MCP server, and only some of them have profiles, so the
// field is left empty when there is nothing to say. When the wrapped server is
// filesystem-ultra and no --profile is given, the profile is the documented
// default (ultra): the server registers the full catalog without the flag.
//
// Knowing the profile per call is what makes the weekly measurement
// attributable: the catalog cost of the ultra profile is paid in every ultra
// session, and the usage of a tool that only exists in ultra (context_pack,
// json_query, gitlab_issues) can only be read against the ultra sessions.
func targetProfile(target []string) string {
	for i, arg := range target {
		if strings.HasPrefix(arg, "--profile=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--profile="))
		}
		if arg == "--profile" && i+1 < len(target) {
			return strings.TrimSpace(target[i+1])
		}
	}
	if len(target) > 0 && isFilesystemUltra(target[0]) {
		return "ultra"
	}
	return ""
}

// isFilesystemUltra reports whether the command is the filesystem-ultra server.
// The name is matched loosely because the binary is deployed under several
// names (filesystem-ultra.exe, filesystem-ultra-v4-embed_rg.exe, …).
func isFilesystemUltra(command string) bool {
	base := strings.ToLower(filepath.Base(command))
	return strings.Contains(base, "filesystem-ultra")
}
