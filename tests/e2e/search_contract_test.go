//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Verify filters across the real stdio boundary, including the release binary
// supplied through MCP_E2E_BINARY (plain or embed_rg).
func TestE2E_SearchFilterContract(t *testing.T) {
	exe := buildServer(t)
	dir := t.TempDir()
	for _, rel := range []string{"RootWriter.cs", "Program.cs", "src/CsvWriter.cs", "src/Reader.cs", "src/nested/JsonWriter.cs", "src/nested/Writer.txt", "Test/Alpha.razor", "ObservacionesPedido/Beta.razor", "Other/Gamma.razor"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("needle needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := startRaw(t, exe, dir)
	for _, tc := range []struct {
		name, include, types string
		want                 int
	}{
		{"writers", "**/*Writer*", ".cs", 3},
		{"brace_dirs", "{Test,ObservacionesPedido}/*.razor", ".razor", 2},
		{"empty", "**/*Writer*", ".go", 0},
	} {
		for _, mode := range []string{"names", "content", "count"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				pattern := "needle"
				if mode == "names" {
					pattern = "."
				}
				args := map[string]any{"path": dir, "pattern": pattern, "include": tc.include, "file_types": tc.types, "include_content": mode == "content", "count_only": mode == "count", "max_results": 100}
				res := c.tool(t, "search_files", args)
				if rawIsError(res) {
					t.Fatal(rawText(res))
				}
				out := rawStructured(res)
				want := tc.want
				if mode == "count" {
					want *= 2
				}
				if got, ok := asInt(out["match_count"]); !ok || got != want {
					t.Fatalf("count=%v want=%d: %#v", out["match_count"], want, out)
				}
				if mode != "count" {
					matches, _ := out["matches"].([]any)
					if len(matches) != tc.want {
						t.Fatalf("wrong match set: %#v", out)
					}
					for _, item := range matches {
						m, _ := item.(map[string]any)
						file, _ := m["file"].(string)
						if strings.Contains(file, "Program.cs") || strings.Contains(file, "Reader.cs") || strings.Contains(file, "Writer.txt") || strings.Contains(file, "Gamma.razor") {
							t.Fatalf("unfiltered result: %s", file)
						}
					}
				}
			})
		}
	}
	res := c.tool(t, "search_files", map[string]any{"path": dir, "pattern": "needle", "include_content": true, "file_types": ".cs", "include": "[bad"})
	if !rawIsError(res) || !strings.Contains(rawText(res), `"code":"VALIDATION"`) {
		t.Fatalf("invalid include was ignored: %#v", res)
	}
}
