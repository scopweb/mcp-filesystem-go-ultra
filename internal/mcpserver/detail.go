package mcpserver

import (
	"fmt"
	"strings"

	"github.com/mcp/filesystem-ultra/core"
)

const (
	detailSummary = "summary"
	detailNormal  = "normal"
	detailFull    = "full"
)

func parseDetailArg(args map[string]interface{}) string {
	if args == nil {
		return detailNormal
	}
	s, _ := args["detail"].(string)
	switch s {
	case detailSummary, detailFull:
		return s
	default:
		return detailNormal
	}
}

func treeEntriesToMaps(entries []core.TreeEntry, detail string) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		item := map[string]any{"path": e.Path, "type": e.Type}
		if e.Protection != "" {
			item["protection"] = e.Protection
		}
		if detail != detailSummary && e.Size > 0 && e.Protection != "protected" && e.Protection != "hidden" {
			item["size"] = e.Size
		}
		out = append(out, item)
	}
	return out
}

func treeSummaryText(entries []core.TreeEntry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Path)
		if e.Type == "directory" && !strings.HasSuffix(e.Path, "/") {
			b.WriteByte('/')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func applySearchDetail(sc map[string]any, text string, detail string) (map[string]any, string) {
	if detail != detailSummary {
		return sc, text
	}
	raw, _ := sc["matches"].([]map[string]any)
	seen := map[string]bool{}
	paths := make([]map[string]any, 0, len(raw))
	var lines []string
	for _, m := range raw {
		p, _ := m["path"].(string)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, map[string]any{"path": p})
		lines = append(lines, p)
	}
	count, _ := sc["match_count"].(int)
	if count == 0 {
		count = len(paths)
		sc["match_count"] = count
	}
	sc["matches"] = paths
	if count == 0 {
		text = searchEmptyText(scopeOf(sc), sc["truncated"] == true)
	} else {
		text = fmt.Sprintf("%d\n%s", count, strings.Join(lines, "\n"))
	}
	sc["message"] = searchSummaryMessage(scopeOf(sc), count, len(paths), sc["truncated"] == true)
	return sc, text
}

// searchEmptyText names the searched scope so a client that only renders the
// text block does not read a silent "0". The structured message stays the
// short summary.
func searchEmptyText(scope map[string]any, truncated bool) string {
	kind := searchScopeKind(scope)
	text := "0 coincidencias en " + kind
	if truncated {
		text += " (truncado)"
	}
	if kind == "nombres de fichero" {
		text += " (filename-only search — file contents were NOT searched; pass include_content:true to search inside files)"
	}
	return text
}

func scopeOf(sc map[string]any) map[string]any {
	scope, _ := sc["scope"].(map[string]any)
	return scope
}

func applyBatchReadDetail(files []map[string]any, combined string, detail string) ([]map[string]any, string) {
	if detail != detailSummary {
		return files, combined
	}
	var b strings.Builder
	out := make([]map[string]any, 0, len(files))
	for i, f := range files {
		item := map[string]any{"path": f["path"]}
		for _, key := range []string{"truncated", "start_line", "end_line", "total_lines", "continuation"} {
			if value, ok := f[key]; ok {
				item[key] = value
			}
		}
		if h, ok := f["content_hash"]; ok {
			item["content_hash"] = h
		}
		if errv, ok := f["error"]; ok {
			item["error"] = errv
		}
		out = append(out, item)
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%v", f["path"])
		if errv, ok := f["error"]; ok {
			fmt.Fprintf(&b, " ERROR: %v", errv)
		}
	}
	return out, b.String()
}
