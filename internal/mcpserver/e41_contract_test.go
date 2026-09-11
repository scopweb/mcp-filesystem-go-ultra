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

func TestE41_ContractMatchesWireSchema(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	for name, st := range reg.server.ListTools() {
		c, ok := core.Contract(name)
		if !ok {
			continue
		}
		props := st.Tool.InputSchema.Properties
		for k, spec := range c.Params {
			raw, exists := props[k]
			if !exists {
				continue
			}
			pm, ok := raw.(map[string]any)
			if !ok {
				t.Errorf("%s.%s: MCP property is %T, want map", name, k, raw)
				continue
			}
			got, _ := pm["type"].(string)
			if !contractTypeOK(spec.Type, got) {
				t.Errorf("%s.%s: MCP type %q incompatible with contract %s", name, k, got, spec.Type)
			}
			if spec.Items != "" {
				items, _ := pm["items"].(map[string]any)
				if items["type"] != spec.Items {
					t.Errorf("%s.%s: items.type=%v want %s", name, k, items["type"], spec.Items)
				}
			}
			if len(spec.Enum) == 0 {
				continue
			}
			gotEnum := map[string]bool{}
			switch rawEnum := pm["enum"].(type) {
			case []any:
				for _, v := range rawEnum {
					if s, ok := v.(string); ok {
						gotEnum[s] = true
					}
				}
			case []string:
				for _, s := range rawEnum {
					gotEnum[s] = true
				}
			}
			for _, want := range spec.Enum {
				if !gotEnum[want] {
					t.Errorf("%s.%s: contract enum %q missing from MCP", name, k, want)
				}
			}
		}
	}
}

func contractTypeOK(pt core.ParamType, mcpType string) bool {
	switch pt {
	case core.ParamString:
		return mcpType == "string"
	case core.ParamNumber:
		return mcpType == "number" || mcpType == "integer"
	case core.ParamBoolean:
		return mcpType == "boolean" || mcpType == "string"
	case core.ParamArray:
		return mcpType == "array"
	case core.ParamObject:
		return mcpType == "object"
	case core.ParamStringOrArray:
		return mcpType == "array" || mcpType == "string"
	default:
		return true
	}
}

func TestE41_ArrayItemsAreObjects(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	tools := reg.server.ListTools()
	multi, ok := tools["multi_edit"]
	if !ok {
		t.Fatal("multi_edit missing")
	}
	edits, ok := multi.Tool.InputSchema.Properties["edits"].(map[string]any)
	if !ok {
		t.Fatalf("edits schema %T", multi.Tool.InputSchema.Properties["edits"])
	}
	items, _ := edits["items"].(map[string]any)
	if items["type"] != "object" {
		t.Fatalf("edits.items want object, got %#v", edits["items"])
	}
	edit, ok := tools["edit_file"]
	if !ok {
		t.Fatal("edit_file missing")
	}
	patterns, ok := edit.Tool.InputSchema.Properties["patterns"].(map[string]any)
	if !ok {
		t.Fatalf("patterns schema %T", edit.Tool.InputSchema.Properties["patterns"])
	}
	pitems, _ := patterns["items"].(map[string]any)
	if pitems["type"] != "object" {
		t.Fatalf("patterns.items want object, got %#v", patterns["items"])
	}
}
