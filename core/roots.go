package core

import "strings"

type RootsMode string

const (
	RootsReplace RootsMode = "replace"
	RootsUnion   RootsMode = "union"
	RootsIgnore  RootsMode = "ignore"
)

const (
	AllowedSourceCLI      = "cli"
	AllowedSourceRoots    = "roots"
	AllowedSourceUnion    = "union"
	AllowedSourceInsecure = "insecure"
)

func ParseRootsMode(s string) RootsMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(RootsUnion):
		return RootsUnion
	case string(RootsIgnore):
		return RootsIgnore
	default:
		return RootsReplace
	}
}

// MergeAllowedPaths combines CLI roots with client MCP Roots.
// Empty client roots never wipe an existing CLI sandbox (fail-closed).
func MergeAllowedPaths(cli, client []string, mode RootsMode) (paths []string, source string) {
	cli = sanitizeNonEmpty(cli)
	client = sanitizeNonEmpty(client)
	switch mode {
	case RootsIgnore:
		if len(cli) == 0 {
			return nil, AllowedSourceInsecure
		}
		return cli, AllowedSourceCLI
	case RootsUnion:
		merged := uniquePaths(append(cli, client...))
		if len(cli) == 0 && len(client) == 0 {
			return nil, AllowedSourceInsecure
		}
		if len(client) == 0 {
			return merged, AllowedSourceCLI
		}
		if len(cli) == 0 {
			return merged, AllowedSourceRoots
		}
		return merged, AllowedSourceUnion
	default:
		if len(client) == 0 {
			if len(cli) == 0 {
				return nil, AllowedSourceInsecure
			}
			return cli, AllowedSourceCLI
		}
		return client, AllowedSourceRoots
	}
}

func sanitizeNonEmpty(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		key := strings.ToLower(filepathSlash(p))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}

func filepathSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
