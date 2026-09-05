package core

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// FileURIToPath converts a MCP Roots file:// URI into a host filesystem path.
// Accepts file:///C:/proj, file:///c:/proj, file://localhost/C:/proj,
// file://wsl.localhost/Ubuntu/home/..., and file:///home/user/proj.
func FileURIToPath(uri string) (string, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return "", fmt.Errorf("empty root URI")
	}
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid root URI %q: %w", uri, err)
	}
	if !strings.EqualFold(u.Scheme, "file") {
		return "", fmt.Errorf("root URI must use file://, got %q", u.Scheme)
	}

	p := u.Path
	if decoded, err := url.PathUnescape(p); err == nil {
		p = decoded
	}

	host := u.Hostname()
	if host != "" && !strings.EqualFold(host, "localhost") {
		rest := strings.TrimPrefix(p, "/")
		unc := `\\` + host + `\` + strings.ReplaceAll(rest, "/", `\`)
		return filepath.Clean(unc), nil
	}

	if len(p) >= 3 && p[0] == '/' && ((p[2] == ':') || (len(p) >= 4 && p[1] == '/' && p[3] == ':')) {
		p = strings.TrimPrefix(p, "/")
	}
	return filepath.Clean(filepath.FromSlash(p)), nil
}
