package mcpserver

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Budget the serialized catalog, not estimated tokens. This catches accidental
// schema/instruction duplication before a client pays for every new session.
//
// Each profile is measured on its OWN registry, because the descriptions are
// not the same in the three profiles: a tool that only exists in ultra can
// carry a different text (read_file points at context_pack in ultra only), and
// slicing one ultra registry to build the strict catalog would measure the
// strict catalog with ultra descriptions. The README quotes these numbers, and
// a change in the catalog has to be a decision, not a surprise.
func TestCatalogSerializedBudget(t *testing.T) {
	catalogOf := func(profile toolProfile) []mcp.Tool {
		reg := newProfileRegistry(t, t.TempDir(), registerOpts{Profile: profile})
		var tools []mcp.Tool
		for _, registered := range reg.server.ListTools() {
			tools = append(tools, registered.Tool)
		}
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		return tools
	}
	ultra, agent, strict := catalogOf(profileUltra), catalogOf(profileAgent), catalogOf(profileStrict)
	u, err := json.Marshal(ultra)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(agent)
	if err != nil {
		t.Fatal(err)
	}
	s, err := json.Marshal(strict)
	if err != nil {
		t.Fatal(err)
	}
	// The token column is the bytes/4 estimate used by the proxy and by the
	// research baseline. It is an estimate, not a tokenizer measurement, and
	// the README says so where it quotes these numbers.
	t.Logf("catalog JSON bytes: ultra=%d (%d tools, ~%d tok), agent=%d (%d tools, ~%d tok), strict=%d (%d tools, ~%d tok), duplicated=%d, removed=%.1f%%",
		len(u), len(ultra), len(u)/4,
		len(a), len(agent), len(a)/4,
		len(s), len(strict), len(s)/4,
		len(u)+len(s), 100*float64(len(s))/float64(len(u)+len(s)))
	// Report a projection without metadata separately from wire size. This is
	// a client-dependent byte measure, not a tokenizer measurement.
	strictNoMeta := make([]mcp.Tool, len(strict))
	copy(strictNoMeta, strict)
	for i := range strictNoMeta {
		strictNoMeta[i].Meta = nil
	}
	model, err := json.Marshal(strictNoMeta)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("strict JSON excluding _meta: %d bytes", len(model))
	// Baseline (2026-10-19): ultra 70,918 B, strict 43,984 B.
	// 2026-11-10: optional auth-trace fields (names and types only; the
	// meaning is in llms.txt) measured ultra 83,507 B, strict 51,011 B.
	// Strict stays at 50 KiB. Ultra cap moves to 84 KiB for that addition.
	// 2026-11-16 (v4.9.0): the diff_files schema declares truncated and
	// continuation, which a cut diff needs to conform to its own schema
	// (+318 B on every profile that ships diff_files). The graduation of the
	// v4.8.0 cohort removed the [EXPERIMENTAL] prefixes (-386 B on ultra).
	// Measured: ultra 84,443 B, strict 51,434 B. Strict cap moves to 51 KiB.
	// 2026-11-24 (v4.9.1): each profile is measured on its own registry, so
	// the numbers are the ones the client really receives (the strict and
	// agent catalogs no longer carry the ultra-only descriptions).
	if len(u) > 84*1024 || len(s) > 51*1024 {
		t.Fatalf("catalog exceeds byte budget (ultra 84 KiB / strict 51 KiB): ultra=%d strict=%d", len(u), len(s))
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
