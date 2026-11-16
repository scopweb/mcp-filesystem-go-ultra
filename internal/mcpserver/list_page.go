package mcpserver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mcp/filesystem-ultra/core"
)

// compactPageLimit is the shared page size of a compact listing. max_nodes
// is the tool cap (default 500). MaxListItems, when smaller, still wins so a
// configured list ceiling is not bypassed.
func compactPageLimit(engine *core.UltraFastEngine, maxNodes int) int {
	limit := maxNodes
	if limit <= 0 {
		limit = 500
	}
	if engine != nil {
		if ml := engine.GetMaxListItems(); ml > 0 && ml < limit {
			limit = ml
		}
	}
	return limit
}

func compactContinueLine(next int) string {
	return fmt.Sprintf("[truncated: continue with offset:%d. Entries are sorted by name. Do not raise max_nodes to recover a cut page.]", next)
}

func parseListPage(listing string) (path string, entries []map[string]any, total int, truncated bool, ok bool) {
	var raw struct {
		Path      string `json:"path"`
		Total     int    `json:"total"`
		Truncated bool   `json:"truncated"`
		Entries   []struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			Size       int64  `json:"size"`
			Modified   string `json:"modified"`
			Protection string `json:"protection"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(listing), &raw); err != nil || raw.Entries == nil && raw.Path == "" {
		return "", nil, 0, false, false
	}
	entries = make([]map[string]any, 0, len(raw.Entries))
	for _, e := range raw.Entries {
		item := map[string]any{"name": e.Name, "type": e.Type}
		if e.Size > 0 {
			item["size"] = e.Size
		}
		if e.Modified != "" {
			item["modified"] = e.Modified
		}
		if e.Protection != "" {
			item["protection"] = e.Protection
		}
		entries = append(entries, item)
	}
	return raw.Path, entries, raw.Total, raw.Truncated, true
}

func stripListingProtection(entries []map[string]any) {
	for _, e := range entries {
		delete(e, "protection")
	}
}

func listingLabel(e map[string]any) string {
	name, _ := e["name"].(string)
	if p, _ := e["protection"].(string); p != "" {
		return name + " [" + p + "]"
	}
	return name
}

func entrySize(e map[string]any) int64 {
	switch n := e["size"].(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func formatCompactPage(compact bool, path string, entries []map[string]any, total, offset int, truncated bool) string {
	if total < offset+len(entries) {
		total = offset + len(entries)
	}
	next := offset + len(entries)
	if compact {
		var b strings.Builder
		b.WriteString(path)
		b.WriteString(" |")
		for _, e := range entries {
			b.WriteByte(' ')
			label := listingLabel(e)
			if e["type"] == "dir" {
				b.WriteString(label)
				b.WriteByte('/')
				continue
			}
			if p, _ := e["protection"].(string); p != "" {
				b.WriteString(label)
				continue
			}
			sz := entrySize(e)
			if sz > 512 {
				fmt.Fprintf(&b, "%s(%s)", label, formatSize(sz))
			} else {
				b.WriteString(label)
			}
		}
		remain := total - next
		if truncated && remain > 0 {
			fmt.Fprintf(&b, " | ...+%d", remain)
		}
		fmt.Fprintf(&b, " | %d/%d", len(entries), total)
		if truncated {
			b.WriteByte('\n')
			b.WriteString(compactContinueLine(next))
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(path)
	b.WriteByte('\n')
	dirs, files := 0, 0
	for _, e := range entries {
		label := listingLabel(e)
		if e["type"] == "dir" {
			dirs++
			fmt.Fprintf(&b, "DIR  %s/\n", label)
			continue
		}
		files++
		if p, _ := e["protection"].(string); p != "" {
			fmt.Fprintf(&b, "FILE %s\n", label)
			continue
		}
		fmt.Fprintf(&b, "FILE %s %s\n", label, formatSize(entrySize(e)))
	}
	fmt.Fprintf(&b, "--- | %d dirs, %d files", dirs, files)
	if truncated {
		b.WriteByte('\n')
		b.WriteString(compactContinueLine(next))
	}
	return b.String()
}

// fitCompactListing keeps the compact text inside the 50 000 character
// budget without dropping the continuation line. Entries returned are the
// ones the text shows, so structuredContent and the text stay on the same page.
func fitCompactListing(compact bool, path string, entries []map[string]any, total, offset int, more bool) (string, []map[string]any, bool) {
	if len(entries) == 0 {
		return formatCompactPage(compact, path, entries, total, offset, more), entries, more
	}
	lo, hi := 1, len(entries)
	best := 0
	var bestText string
	for lo <= hi {
		mid := (lo + hi) / 2
		page := entries[:mid]
		truncated := more || mid < len(entries)
		text := formatCompactPage(compact, path, page, total, offset, truncated)
		if len(text) <= claudeSpillChars {
			best = mid
			bestText = text
			lo = mid + 1
			continue
		}
		hi = mid - 1
	}
	if best == 0 {
		page := entries[:1]
		text := formatCompactPage(compact, path, page, total, offset, true)
		if len(text) > claudeSpillChars {
			footer := "\n" + compactContinueLine(offset+1)
			budget := claudeSpillChars - len(footer)
			if budget < 1 {
				budget = 1
			}
			if len(text) > budget {
				text = text[:utf8SafeEnd(text, budget)]
			}
			text += footer
		}
		return text, page, true
	}
	truncated := more || best < len(entries)
	return bestText, entries[:best], truncated
}
