package mcpserver

import (
	"fmt"
	"strings"
)

type toolProfile string

const (
	profileUltra  toolProfile = "ultra"
	profileStrict toolProfile = "strict"
)

// strictToolSet is the agent-core catalog. Order matches the discovery
// workflow: roots → tree → I/O → patch → help. Everything else (git, wsl,
// minify_js, batch, backup, …) stays on profile=ultra.
var strictToolSet = map[string]struct{}{
	"list_allowed_directories": {},
	"directory_tree":           {},
	"list_directory":           {},
	"search_files":             {},
	"get_file_info":            {},
	"read_file":                {},
	"write_file":               {},
	"edit_file":                {},
	"multi_edit":               {},
	"apply_patch":              {},
	"diff_files":               {},
	"create_directory":         {},
	"move_file":                {},
	"delete_file":              {},
	"help":                     {},
}

func parseToolProfile(s string) (toolProfile, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "ultra":
		return profileUltra, nil
	case "strict":
		return profileStrict, nil
	default:
		return "", fmt.Errorf("invalid --profile %q (want strict|ultra)", s)
	}
}

func (p toolProfile) allows(name string) bool {
	if p == "" || p == profileUltra {
		return true
	}
	_, ok := strictToolSet[name]
	return ok
}

type registerOpts struct {
	Profile    toolProfile
	GitNetwork bool
}
