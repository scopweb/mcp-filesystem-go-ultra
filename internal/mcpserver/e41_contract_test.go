package mcpserver

import (
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

func TestE41_RegisteredSchemaSubsetOfContract(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	for name, st := range reg.server.ListTools() {
		c, ok := core.Contract(name)
		if !ok {
			t.Errorf("no contract for registered tool %q", name)
			continue
		}
		for k := range st.Tool.InputSchema.Properties {
			if _, ok := c.Params[k]; !ok {
				t.Errorf("%s: MCP property %q missing from contract", name, k)
			}
		}
	}
}

func TestE41_HelpServesContractExamples(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	res := callHelp(t, reg, map[string]interface{}{"tool": "read_file"})
	text := resultText(t, res)
	if !strings.Contains(text, "### Examples") || !strings.Contains(text, "read_file(path:") {
		t.Fatalf("help(read_file) missing contract examples:\n%s", text)
	}
	git := callHelp(t, reg, map[string]interface{}{"tool": "git"})
	if !strings.Contains(resultText(t, git), `git(action:"status")`) {
		t.Fatalf("help(git) missing examples:\n%s", resultText(t, git))
	}
}
