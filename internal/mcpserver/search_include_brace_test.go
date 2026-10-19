package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSearchFiles_IncludeBraceAlternatives(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("A.cs", "needle in A\n")
	write("B.cs", "needle in B\n")
	write("C.cs", "needle in C\n")
	write("notes.go", "needle in go\n")
	write("Test/Alpha.razor", "needle in alpha\n")
	write("ObservacionesPedido/Beta.razor", "needle in beta\n")
	write("Other/Gamma.razor", "needle in gamma\n")

	reg := newHelpTestRegistry(t, dir)
	h := reg.searchFilesHandler
	call := func(args map[string]interface{}) *mcp.CallToolResult {
		t.Helper()
		res, err := h(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name: "search_files", Arguments: args,
		}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	names := call(map[string]interface{}{
		"path": dir, "pattern": ".", "include": "{A.cs,B.cs}",
	})
	if names.IsError {
		t.Fatalf("name include errored: %s", resultText(t, names))
	}
	nameText := resultText(t, names)
	if !strings.Contains(nameText, "A.cs") || !strings.Contains(nameText, "B.cs") {
		t.Fatalf("include {A.cs,B.cs} should find both names, got:\n%s", nameText)
	}
	if strings.Contains(nameText, "C.cs") {
		t.Fatalf("include {A.cs,B.cs} should skip C.cs, got:\n%s", nameText)
	}

	globs := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "include_content": true,
		"include": "{Test,ObservacionesPedido}/*.razor",
	})
	if globs.IsError {
		t.Fatalf("dir include errored: %s", resultText(t, globs))
	}
	globText := resultText(t, globs)
	if !strings.Contains(globText, "Alpha.razor") || !strings.Contains(globText, "Beta.razor") {
		t.Fatalf("include brace dirs should find razor files, got:\n%s", globText)
	}
	if strings.Contains(globText, "Gamma.razor") || strings.Contains(globText, "A.cs") {
		t.Fatalf("include brace dirs should skip other files, got:\n%s", globText)
	}

	simple := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "include_content": true, "include": "*.go",
	})
	if simple.IsError {
		t.Fatalf("simple include errored: %s", resultText(t, simple))
	}
	simpleText := resultText(t, simple)
	if !strings.Contains(simpleText, "notes.go") || strings.Contains(simpleText, "A.cs") {
		t.Fatalf("include *.go regression, got:\n%s", simpleText)
	}

	types := call(map[string]interface{}{
		"path": dir, "pattern": "needle", "include_content": true, "file_types": ".cs,.go",
	})
	if types.IsError {
		t.Fatalf("file_types errored: %s", resultText(t, types))
	}
	typeText := resultText(t, types)
	if !strings.Contains(typeText, "A.cs") || !strings.Contains(typeText, "notes.go") || strings.Contains(typeText, "Alpha.razor") {
		t.Fatalf("file_types .cs,.go regression, got:\n%s", typeText)
	}

	empty := call(map[string]interface{}{
		"path": dir, "pattern": ".", "include": "{Nope.cs,Missing.cs}",
	})
	if empty.IsError {
		t.Fatalf("valid include with no hits must not error: %s", resultText(t, empty))
	}
	emptyText := resultText(t, empty)
	if strings.Contains(emptyText, errCodeInvalidParams) || strings.Contains(emptyText, "A.cs") {
		t.Fatalf("valid empty include should be an empty search, got:\n%s", emptyText)
	}

	bad := call(map[string]interface{}{
		"path": dir, "pattern": ".", "include": "{A.cs,B.cs",
	})
	if !bad.IsError {
		t.Fatalf("unbalanced braces should error, got:\n%s", resultText(t, bad))
	}
	badText := resultText(t, bad)
	var envelope pathErrorEnvelope
	if err := json.Unmarshal([]byte(badText), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != errCodeInvalidParams || envelope.Error.Retryable || envelope.Error.Details["field"] != "include" || !strings.Contains(envelope.Error.Message, "unbalanced") {
		t.Fatalf("invalid braces must use the VALIDATION envelope, got:\n%s", badText)
	}
	if strings.Contains(badText, "0 coincidencias") {
		t.Fatalf("invalid braces must not look like an empty search, got:\n%s", badText)
	}
}
