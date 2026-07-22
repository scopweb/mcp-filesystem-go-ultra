package main

// Handler-level outputSchema conformance sweep (Fase 1 del plan de endurecimiento).
//
// output_schema_test.go validates the payload BUILDERS with synthetic data — but
// the -32600 incident (2026-07-19) proved that is not enough: three edit_file
// code paths never called the builders and returned plain text, which strict MCP
// clients reject. This sweep drives the REAL handlers through every success path
// of every tool that declares an outputSchema and validates the actual
// StructuredContent against the declared schema (required keys, no orphan keys,
// type spot-checks). A handler path returning plain text fails here immediately.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// sweepCase is one handler invocation to validate against its outputSchema.
type sweepCase struct {
	name   string
	tool   string
	args   map[string]any
	schema schemaInfo
	// textMessage: when true, the structured "message" field must equal the
	// text content block byte-for-byte (the attachMessage contract).
	textMessage bool
}

// callToolSweep invokes a tool through the registry dispatch map.
func callToolSweep(t *testing.T, reg *toolRegistry, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	handler, ok := reg.handlers[tool]
	if !ok {
		t.Fatalf("tool %q not registered", tool)
	}
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool, Arguments: args}}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("%s handler error: %v", tool, err)
	}
	if res == nil {
		t.Fatalf("%s returned nil result", tool)
	}
	if res.IsError {
		t.Fatalf("%s returned tool error: %v", tool, res.Content)
	}
	return res
}

// structuredPayload extracts the StructuredContent map or fails the test —
// this is the exact assertion that would have caught the -32600 paths.
func structuredPayload(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T, want map[string]any — plain-text result on a schema-declared tool", res.StructuredContent)
	}
	return m
}

// textBlock returns the text of the first content block.
func textBlock(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("result has no text content block")
	return ""
}

func TestOutputSchema_HandlerSweep(t *testing.T) {
	dir := t.TempDir()
	reg := buildEditRegistry(t, dir, false)
	registerBatchTools(reg)

	readSchema := parseSchema(t, readFileOutputSchema)
	writeSchema := parseSchema(t, writeFileOutputSchema)
	editSchema := parseSchema(t, editFileOutputSchema)
	multiSchema := parseSchema(t, multiEditOutputSchema)

	// freshFile creates a per-case file so edit cases cannot interfere.
	freshFile := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	const sample = "alpha beta gamma\nsecond line foo\nthird line\n"
	b64 := base64.StdEncoding.EncodeToString([]byte("hello world"))

	// pathsJSON marshals a slice of host paths into the JSON string the
	// read_file batch param expects (Windows backslashes must be escaped).
	pathsJSON := func(paths ...string) string {
		b, err := json.Marshal(paths)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	cases := []sweepCase{
		// ---- read_file (required: content) ----
		{name: "read full", tool: "read_file", schema: readSchema,
			args: map[string]any{"path": freshFile("rf1.txt", sample)}},
		{name: "read range", tool: "read_file", schema: readSchema,
			args: map[string]any{"path": freshFile("rf2.txt", sample), "start_line": float64(1), "end_line": float64(2)}},
		{name: "read head", tool: "read_file", schema: readSchema,
			args: map[string]any{"path": freshFile("rf3.txt", sample), "max_lines": float64(1), "mode": "head"}},
		{name: "read base64", tool: "read_file", schema: readSchema,
			args: map[string]any{"path": freshFile("rf4.txt", sample), "encoding": "base64"}},
		{name: "read batch", tool: "read_file", schema: readSchema,
			args: map[string]any{"paths": pathsJSON(freshFile("rf5a.txt", sample), freshFile("rf5b.txt", sample))}},

		// ---- write_file (required: path, bytes_written, verified, message) ----
		{name: "write text", tool: "write_file", schema: writeSchema, textMessage: true,
			args: map[string]any{"path": filepath.Join(dir, "wf1.txt"), "content": sample}},
		{name: "write base64", tool: "write_file", schema: writeSchema, textMessage: true,
			args: map[string]any{"path": filepath.Join(dir, "wf2.txt"), "content_base64": b64}},

		// ---- edit_file (required: path, replacements, lines_added, lines_removed, total_lines, message) ----
		{name: "edit replace", tool: "edit_file", schema: editSchema, textMessage: true,
			args: map[string]any{"path": freshFile("e1.txt", sample), "old_text": "beta", "new_text": "BETA"}},
		{name: "edit search_replace", tool: "edit_file", schema: editSchema, textMessage: true,
			args: map[string]any{"path": freshFile("e2.txt", sample), "mode": "search_replace", "pattern": "foo", "replacement": "FOO"}},
		{name: "edit occurrence", tool: "edit_file", schema: editSchema, textMessage: true,
			args: map[string]any{"path": freshFile("e3.txt", sample), "old_text": "foo", "new_text": "z", "occurrence": float64(1)}},
		{name: "edit regex", tool: "edit_file", schema: editSchema, textMessage: true,
			args: map[string]any{"path": freshFile("e4.txt", sample), "mode": "regex", "pattern": "(alpha) (beta)", "replacement": "$2 $1"}},
		{name: "edit delete_range", tool: "edit_file", schema: editSchema, textMessage: true,
			args: map[string]any{"path": freshFile("e5.txt", sample), "mode": "delete_range", "start_line": float64(2), "end_line": float64(2)}},
		{name: "edit replace_range", tool: "edit_file", schema: editSchema, textMessage: true,
			args: map[string]any{"path": freshFile("e6.txt", sample), "mode": "replace_range", "start_line": float64(1), "end_line": float64(1), "new_text": "replaced line"}},
		{name: "edit dry_run", tool: "edit_file", schema: editSchema, textMessage: true,
			args: map[string]any{"path": freshFile("e7.txt", sample), "old_text": "alpha", "new_text": "X", "dry_run": true}},

		// ---- multi_edit (required: path, successful_edits, total_edits, message) ----
		{name: "multi_edit basic", tool: "multi_edit", schema: multiSchema, textMessage: true,
			args: map[string]any{"path": freshFile("me1.txt", sample),
				"edits_json": `[{"old_text":"beta","new_text":"B"},{"old_text":"gamma","new_text":"G"}]`}},
		// Regression (2026-07-22): multi_edit dry_run returned plain text and
		// strict clients rejected it with a bare "Tool execution failed".
		{name: "multi_edit dry_run", tool: "multi_edit", schema: multiSchema, textMessage: true,
			args: map[string]any{"path": freshFile("me2.txt", sample),
				"edits_json": `[{"old_text":"beta","new_text":"B"}]`, "dry_run": true}},
	}

	for _, tc := range cases {
		t.Run(tc.tool+"/"+tc.name, func(t *testing.T) {
			res := callToolSweep(t, reg, tc.tool, tc.args)
			payload := structuredPayload(t, res)
			assertPayloadConforms(t, tc.schema, payload)
			if tc.textMessage {
				if msg, _ := payload["message"].(string); msg != textBlock(t, res) {
					t.Errorf("structured message != text content block\nstructured: %q\ntext: %q", msg, textBlock(t, res))
				}
			}
		})
	}
}
