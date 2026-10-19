package mcpserver

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Budget the serialized catalog, not estimated tokens. This catches accidental
// schema/instruction duplication before a client pays for every new session.
func TestCatalogSerializedBudget(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	var ultra, strict []mcp.Tool
	for name, registered := range reg.server.ListTools() {
		ultra = append(ultra, registered.Tool)
		if profileStrict.allows(name) {
			strict = append(strict, registered.Tool)
		}
	}
	for _, tools := range [][]mcp.Tool{ultra, strict} {
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	}
	u, err := json.Marshal(ultra)
	if err != nil {
		t.Fatal(err)
	}
	s, err := json.Marshal(strict)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("catalog JSON bytes: ultra=%d (%d tools), strict=%d (%d tools), duplicated=%d, removed=%.1f%%", len(u), len(ultra), len(s), len(strict), len(u)+len(s), 100*float64(len(s))/float64(len(u)+len(s)))
	// Baseline (2026-10-19): ultra 70,918 B, strict 43,984 B. Leave about
	// 16% headroom; inspect growth before deliberately changing these budgets.
	if len(u) > 80*1024 || len(s) > 50*1024 {
		t.Fatalf("catalog exceeds byte budget (ultra 80 KiB / strict 50 KiB): ultra=%d strict=%d", len(u), len(s))
	}
}
