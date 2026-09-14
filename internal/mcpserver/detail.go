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
		if detail != detailSummary && e.Size > 0 {
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
	text = fmt.Sprintf("%d\n%s", count, strings.Join(lines, "\n"))
	sc["message"] = text
	return sc, text
}

func applyBatchReadDetail(files []map[string]any, combined string, detail string) ([]map[string]any, string) {
	if detail != detailSummary {
		return files, combined
	}
	var b strings.Builder
	out := make([]map[string]any, 0, len(files))
	for i, f := range files {
		item := map[string]any{"path": f["path"]}
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
