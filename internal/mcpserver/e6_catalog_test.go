package mcpserver

import (
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestE6_RegisteredToolsHaveContractCatalogEntries(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	names := map[string]bool{}
	for _, e := range core.ContractCatalog() {
		names[e.Name] = true
	}
	for name := range reg.server.ListTools() {
		if !names[name] {
			t.Errorf("registered tool %q missing from contract catalog", name)
		}
	}
}

func TestE6_HelpServesContractEnumsAndStructuredNote(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	cat := resultText(t, callHelp(t, reg, nil))
	if !strings.Contains(cat, "structuredContent") || !strings.Contains(cat, "retryable") {
		t.Fatalf("help() missing structured/retryable note:\n%s", cat)
	}
	edit := resultText(t, callHelp(t, reg, map[string]interface{}{"tool": "edit_file"}))
	if !strings.Contains(edit, "### Contract enums") || !strings.Contains(edit, "replace_range") {
		t.Fatalf("help(edit_file) missing contract enums:\n%s", edit)
	}
}
