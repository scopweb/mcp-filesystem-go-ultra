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
	// Report a projection without metadata separately from wire size. This is
	// a client-dependent byte measure, not a tokenizer measurement.
	for i := range strict {
		strict[i].Meta = nil
	}
	model, err := json.Marshal(strict)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("strict JSON excluding _meta: %d bytes", len(model))
	// Baseline (2026-10-19): ultra 70,918 B, strict 43,984 B. Leave about
	// 16% headroom; inspect growth before deliberately changing these budgets.
	if len(u) > 80*1024 || len(s) > 50*1024 {
		t.Fatalf("catalog exceeds byte budget (ultra 80 KiB / strict 50 KiB): ultra=%d strict=%d", len(u), len(s))
	}
}

func TestCatalogPresentationPreservesContract(t *testing.T) {
	tool := mcp.NewTool("edit_file",
		mcp.WithDescription("edit_file — Edit with expected_hash. Related: read_file, backup."),
		mcp.WithString("expected_hash", mcp.Required()),
		mcp.WithRawOutputSchema(json.RawMessage(`{"type":"object","required":["content_hash"]}`)),
		mcp.WithDestructiveHintAnnotation(true),
	)
	tool.Meta = mcp.NewMetaFromMap(map[string]any{"vendor/keep": "value"})
	got := compactToolPresentation(tool)
	if got.Description != "Edit with expected_hash." {
		t.Fatalf("operational guidance lost: %q", got.Description)
	}
	if _, changed := tool.Meta.AdditionalFields[relatedToolsMetaKey]; changed {
		t.Fatal("caller metadata was mutated")
	}
	if got.Meta.AdditionalFields["vendor/keep"] != "value" {
		t.Fatal("unrelated metadata was lost")
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	var meta map[string]string
	if err := json.Unmarshal(wire["_meta"], &meta); err != nil || meta[relatedToolsMetaKey] != "read_file, backup." {
		t.Fatalf("metadata missing on wire: %s (%v)", wire["_meta"], err)
	}
	// Restoring only presentation fields must recover the original contract.
	got.Description, got.Meta = tool.Description, tool.Meta
	want, _ := json.Marshal(tool)
	actual, _ := json.Marshal(got)
	if string(actual) != string(want) {
		t.Fatal("schema, annotations, or identity changed during compaction")
	}
}
