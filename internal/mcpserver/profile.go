package mcpserver

import (
	"fmt"
	"strings"
)

type toolProfile string

const (
	profileUltra  toolProfile = "ultra"
	profileStrict toolProfile = "strict"
	profileAgent  toolProfile = "agent"
)

// strictToolSet is the agent-core catalog. Order matches the discovery
// workflow: roots → orientation → I/O → patch → undo → help. Everything else
// (git, wsl, minify_js, batch, …) stays on profile=ultra.
//
// Inventory (18 tools, v4.8.1): project_map joins the catalog as the cheap
// structural orientation step. It is a STABLE tool from the start (explicit
// exception to the experimental policy in experimental.go): it declares a
// full outputSchema and has contract coverage. The 17 core tools stay
// frozen; nothing was removed to make room for it.
var strictToolSet = map[string]struct{}{
	"list_allowed_directories": {},
	"project_map":              {},
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
	"backup":                   {},
	"help":                     {},
	"security_policy":          {},
}

func parseToolProfile(s string) (toolProfile, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "ultra":
		return profileUltra, nil
	case "strict":
		return profileStrict, nil
	case "agent":
		return profileAgent, nil
	default:
		return "", fmt.Errorf("invalid --profile %q (want strict|agent|ultra)", s)
	}
}

func (p toolProfile) allows(name string) bool {
	if p == "" || p == profileUltra {
		return true
	}
	if p == profileAgent && name == "git" {
		return true
	}
	_, ok := strictToolSet[name]
	return ok
}

type registerOpts struct {
	Profile    toolProfile
	GitNetwork bool
}
