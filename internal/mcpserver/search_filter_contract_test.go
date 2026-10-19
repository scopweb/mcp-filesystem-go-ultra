package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSearchFiles_FilterIntersectionContract(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"RootWriter.cs", "Program.cs", "Cobertura.cs", "ProvesBatch.cs", "src/CsvWriter.cs", "src/Reader.cs", "src/nested/JsonWriter.cs", "src/nested/Writer.txt", "Test/Alpha.razor", "ObservacionesPedido/Beta.razor", "Other/Gamma.razor"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("needle needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reg := newHelpTestRegistry(t, dir)
	for _, tc := range []struct {
		name, include, types string
		want                 int
	}{
		{"include_only", "**/*Writer*.cs", "", 3},
		{"intersection", "**/*Writer*", ".cs", 3},
		{"empty_intersection", "**/*Writer*", ".go", 0},
		{"brace_dirs", "{Test,ObservacionesPedido}/*.razor", ".razor", 2},
		{"windows_separators", `src\**\*Writer*.cs`, ".cs", 2},
	} {
		for _, mode := range []string{"names", "content", "count"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				pattern := "needle"
				if mode == "names" {
					pattern = "."
				}
				args := map[string]any{"path": dir, "pattern": pattern, "include": tc.include, "file_types": tc.types, "include_content": mode == "content", "count_only": mode == "count", "max_results": float64(100)}
				res, err := reg.searchFilesHandler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "search_files", Arguments: args}})
				if err != nil {
					t.Fatal(err)
				}
				if res.IsError {
					t.Fatal(resultText(t, res))
				}
				raw, err := json.Marshal(res.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				var out struct {
					MatchCount int  `json:"match_count"`
					Truncated  bool `json:"truncated"`
					Matches    []struct {
						File string `json:"file"`
					} `json:"matches"`
				}
				if err := json.Unmarshal(raw, &out); err != nil {
					t.Fatal(err)
				}
				want := tc.want
				if mode == "count" {
					want *= 2
				}
				if out.MatchCount != want {
					t.Fatalf("count=%d want=%d: %s", out.MatchCount, want, raw)
				}
				if mode != "count" && len(out.Matches) != tc.want {
					t.Fatalf("wrong filtered result set: %s", raw)
				}
			})
		}
	}
	for _, inc := range []string{"{A.cs,B.cs", "{A.cs,}", "[bad"} {
		res, err := reg.searchFilesHandler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "search_files", Arguments: map[string]any{"path": dir, "pattern": ".", "file_types": ".cs", "include": inc}}})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("invalid include ignored with file_types: %q", inc)
		}
		var envelope pathErrorEnvelope
		if err := json.Unmarshal([]byte(resultText(t, res)), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error.Code != errCodeInvalidParams || envelope.Error.Retryable {
			t.Fatalf("wrong validation contract: %+v", envelope)
		}
	}
}
