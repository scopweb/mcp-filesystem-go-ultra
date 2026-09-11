package core

import (
	"fmt"
	"sort"
	"strings"
)

type CatalogTool struct {
	Name     string              `json:"name"`
	Class    string              `json:"class"`
	Required []string            `json:"required,omitempty"`
	Enums    map[string][]string `json:"enums,omitempty"`
	Examples []string            `json:"examples,omitempty"`
}

func (c ActionClass) String() string {
	switch c {
	case ClassRead:
		return "read"
	case ClassWrite:
		return "write"
	default:
		return "mixed"
	}
}

func ContractNames() []string {
	names := make([]string, 0, len(contracts))
	for n := range contracts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func ContractCatalog() []CatalogTool {
	names := ContractNames()
	out := make([]CatalogTool, 0, len(names))
	for _, name := range names {
		c := contracts[name]
		entry := CatalogTool{
			Name:     name,
			Class:    c.Class.String(),
			Examples: append([]string(nil), c.Examples...),
		}
		keys := make([]string, 0, len(c.Params))
		for k := range c.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		req := make([]string, 0)
		enums := map[string][]string{}
		for _, k := range keys {
			p := c.Params[k]
			if p.Required {
				req = append(req, k)
			}
			if len(p.Enum) > 0 {
				enums[k] = append([]string(nil), p.Enum...)
			}
		}
		entry.Required = req
		if len(enums) > 0 {
			entry.Enums = enums
		}
		out = append(out, entry)
	}
	return out
}

func RenderContractCatalog() string {
	cat := ContractCatalog()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Tool contract catalog (%d tools)\n\n", len(cat)))
	for _, t := range cat {
		sb.WriteString(fmt.Sprintf("## %s (%s)\n", t.Name, t.Class))
		if len(t.Required) > 0 {
			sb.WriteString("- required: " + strings.Join(t.Required, ", ") + "\n")
		}
		if len(t.Enums) > 0 {
			keys := make([]string, 0, len(t.Enums))
			for k := range t.Enums {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", k, strings.Join(t.Enums[k], " | ")))
			}
		}
		for _, ex := range t.Examples {
			sb.WriteString("- example: `" + ex + "`\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
