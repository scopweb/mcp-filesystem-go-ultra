package core

import (
	"strings"
	"testing"
)

func TestE6_ContractCatalogHasEnumsAndExamples(t *testing.T) {
	cat := ContractCatalog()
	if len(cat) < 20 {
		t.Fatalf("catalog too small: %d", len(cat))
	}
	byName := map[string]CatalogTool{}
	for _, e := range cat {
		if e.Name == "" || e.Class == "" {
			t.Fatalf("empty name/class: %+v", e)
		}
		byName[e.Name] = e
	}
	read, ok := byName["read_file"]
	if !ok {
		t.Fatal("read_file missing")
	}
	if read.Class != "read" {
		t.Fatalf("class=%s", read.Class)
	}
	if len(read.Enums["mode"]) == 0 || len(read.Examples) == 0 {
		t.Fatalf("read_file missing enums/examples: %+v", read)
	}
	edit := byName["edit_file"]
	found := false
	for _, v := range edit.Enums["mode"] {
		if v == "replace_range" {
			found = true
		}
	}
	if !found {
		t.Fatalf("edit_file mode enum missing replace_range: %v", edit.Enums["mode"])
	}
	md := RenderContractCatalog()
	for _, want := range []string{"read_file", "edit_file", "batch_operations", "required:"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown catalog missing %q", want)
		}
	}
}
