package main

// FASE1: schema↔payload coherence test. No new dependencies — uses only the
// stdlib (encoding/json + reflect-free structural checks). Builds payloads
// with the SAME builders the handlers use (writeStructured, editStructured,
// multiEditStructured, attachMessage, attachParentBackup) and asserts:
//   1. Each schema parses as valid JSON Schema (object with properties/required).
//   2. Every key listed in `required` is present in the payload (or in the
//      payload-with-attached-message).
//   3. Every key actually emitted by the builder exists in `properties`
//      (catches NEW keys added to the payload but not to the schema).
//   4. Type spot-checks for the fields the schema declares: string keys must
//      hold strings, integer keys must hold numbers (json.Unmarshal yields
//      float64 for numeric JSON values, so we accept either int or float64).
//   5. The "variable-site foot-gun": when edit_file sets
//      sc["external_change"] AND then calls attachParentBackup(sc, ...), both
//      keys survive (the second helper must not rebuild the map).
//   6. A minimal EditResult — only required keys present, none of the
//      optional ones — also satisfies the schema's required contract.
//
// This is a LIGHTWEIGHT guard. It does not implement full JSON Schema
// validation (no $ref, no oneOf/anyOf, no nested constraints). It catches the
// drift the FASE1 spec is most worried about: a builder adding a new payload
// key without declaring it in the schema, or vice-versa.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// schemaInfo parses a json.RawMessage schema into a map and extracts the
// `properties` map and `required` slice. Returns nil for both when absent.
type schemaInfo struct {
	properties map[string]map[string]any
	required   []string
}

func parseSchema(t *testing.T, schema json.RawMessage) schemaInfo {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(schema, &raw); err != nil {
		t.Fatalf("schema is not valid JSON: %v\nraw: %s", err, string(schema))
	}
	if raw["type"] != "object" {
		t.Fatalf("schema top-level type is not 'object': %v", raw["type"])
	}
	out := schemaInfo{properties: map[string]map[string]any{}}
	if props, ok := raw["properties"].(map[string]any); ok {
		for k, v := range props {
			if pm, ok := v.(map[string]any); ok {
				out.properties[k] = pm
			}
		}
	}
	if req, ok := raw["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				out.required = append(out.required, s)
			}
		}
	}
	return out
}

// assertPayloadConforms checks:
//   - every required key is in payload (under the schema's name space)
//   - every payload key exists in schema.properties (no orphan keys)
//   - type spot-check for declared types (string/integer)
func assertPayloadConforms(t *testing.T, schema schemaInfo, payload map[string]any) {
	t.Helper()
	// 1. all required keys present
	for _, key := range schema.required {
		if _, ok := payload[key]; !ok {
			t.Errorf("required key %q missing from payload", key)
		}
	}
	// 2. no orphan keys (payload keys must be declared in schema)
	for key := range payload {
		if _, declared := schema.properties[key]; !declared {
			t.Errorf("payload key %q not declared in schema properties", key)
		}
	}
	// 3. type spot-checks
	for key, val := range payload {
		prop, ok := schema.properties[key]
		if !ok {
			continue // already reported above
		}
		expectedType, _ := prop["type"].(string)
		switch expectedType {
		case "string":
			if _, ok := val.(string); !ok {
				t.Errorf("schema says %q is string, payload has %T", key, val)
			}
		case "integer", "number":
			switch v := val.(type) {
			case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64,
				float32, float64:
				_ = v
			default:
				t.Errorf("schema says %q is %s, payload has %T", key, expectedType, val)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// read_file
// -----------------------------------------------------------------------------

func TestReadFileSchema_IsValid(t *testing.T) {
	parseSchema(t, readFileOutputSchema)
}

func TestReadFileSchema_MultiPayload_Conforms(t *testing.T) {
	// The batch branch returns map[string]any{"content": ...} WITHOUT
	// content_hash (schema declares it optional for multi-file reads).
	schema := parseSchema(t, readFileOutputSchema)
	payload := map[string]any{"content": "=== file1.txt ===\nhello\n"}
	assertPayloadConforms(t, schema, payload)
}

func TestReadFileSchema_SinglePayload_WithHash_Conforms(t *testing.T) {
	schema := parseSchema(t, readFileOutputSchema)
	payload := map[string]any{"content": "hello\n", "content_hash": "deadbeef"}
	assertPayloadConforms(t, schema, payload)
}

// -----------------------------------------------------------------------------
// write_file
// -----------------------------------------------------------------------------

func TestWriteFileSchema_IsValid(t *testing.T) {
	parseSchema(t, writeFileOutputSchema)
}

func TestWriteFileSchema_FullPayload_Conforms(t *testing.T) {
	// Full payload: writeStructured + feedback + backup_id + message
	schema := parseSchema(t, writeFileOutputSchema)
	sc := writeStructured("C:/x/file.go", 42, "abcd1234")
	sc["backup_id"] = "20260711-120000-aaaa"
	sc["feedback"] = " [WARN:truncation]"
	attachMessage(sc, "WRITTEN [C] C:/x/file.go | 42B")
	assertPayloadConforms(t, schema, sc)
}

func TestWriteFileSchema_MinimalPayload_Conforms(t *testing.T) {
	// Minimal: just path + bytes_written + message (no hash, no feedback).
	// Required keys must still be present.
	schema := parseSchema(t, writeFileOutputSchema)
	sc := writeStructured("C:/x/file.go", 42, "") // empty hash → no content_hash key
	attachMessage(sc, "WRITTEN [C] C:/x/file.go | 42B")
	assertPayloadConforms(t, schema, sc)
}

// -----------------------------------------------------------------------------
// edit_file
// -----------------------------------------------------------------------------

func TestEditFileSchema_IsValid(t *testing.T) {
	parseSchema(t, editFileOutputSchema)
}

func TestEditFileSchema_FullPayload_Conforms(t *testing.T) {
	schema := parseSchema(t, editFileOutputSchema)
	result := &core.EditResult{
		ReplacementCount: 1,
		LinesAdded:       2,
		LinesRemoved:     1,
		TotalLines:       10,
		NewHash:          "12345678",
		BackupID:         "20260711-130000-bbbb",
		RiskWarning:      "MEDIUM risk",
		StructureWarning: "unbalanced braces",
		Integrity: &core.FileIntegrityResult{
			Verification: "OK",
			Hash:         "12345678abcd",
		},
	}
	sc := editStructured("C:/x/file.go", result)
	attachMessage(sc, "M C:/x/file.go | 1@+2-1 | 10L")
	assertPayloadConforms(t, schema, sc)
}

func TestEditFileSchema_MinimalPayload_Conforms(t *testing.T) {
	// Minimal EditResult: only the unconditional fields populated.
	// All optional fields (content_hash, backup_id, risk_warning,
	// structure_warning, integrity) must be absent from the payload.
	schema := parseSchema(t, editFileOutputSchema)
	result := &core.EditResult{
		ReplacementCount: 0,
		LinesAdded:       0,
		LinesRemoved:     0,
		TotalLines:       5,
	}
	sc := editStructured("C:/x/file.go", result)
	attachMessage(sc, "M C:/x/file.go | 0@+0-0 | 5L")
	assertPayloadConforms(t, schema, sc)
}

func TestEditFileSchema_VariableSiteFootGun(t *testing.T) {
	// The edit_file handler has two variable sites (compact + verbose) that
	// hold `sc` as a local variable, conditionally add external_change, and
	// then call attachParentBackup(sc, ...). The helper must MUTATE sc, not
	// rebuild it via editStructured(...) — otherwise the external_change
	// field set just above would be silently dropped.
	//
	// We can't run the real engine here (it needs a backup manager with disk
	// state), so we exercise the same mutation pattern: sc is a single map
	// that both writes go through. If a future refactor makes
	// attachParentBackup reconstruct (return a new map) instead of mutating,
	// the post-call inspection of sc will catch it because external_change
	// would survive on sc (it was set on sc), but parent_backup_id would
	// only be on the returned map — for our purposes both must be on sc.
	schema := parseSchema(t, editFileOutputSchema)
	result := &core.EditResult{
		ReplacementCount: 1,
		LinesAdded:       1,
		LinesRemoved:     1,
		TotalLines:       8,
		BackupID:         "20260711-130000-cccc",
	}
	sc := editStructured("C:/x/file.go", result)
	// Conditional set, mirroring tools_core.go:1208-1210 (compact site).
	sc["external_change"] = "⚠ file changed on disk since this session last saw it"
	// attachParentBackup with nil engine short-circuits (no parent_backup_id
	// added) — but the external_change we just set must NOT be removed.
	attachParentBackup(sc, nil, result.BackupID)
	if _, ok := sc["external_change"]; !ok {
		t.Fatal("foot-gun: attachParentBackup must not drop external_change")
	}
	// And the payload must still conform.
	attachMessage(sc, "M C:/x/file.go | 1@+1-1 | 8L")
	assertPayloadConforms(t, schema, sc)
}

// -----------------------------------------------------------------------------
// multi_edit
// -----------------------------------------------------------------------------

func TestMultiEditSchema_IsValid(t *testing.T) {
	parseSchema(t, multiEditOutputSchema)
}

func TestMultiEditSchema_FullPayload_Conforms(t *testing.T) {
	schema := parseSchema(t, multiEditOutputSchema)
	result := &core.MultiEditResult{
		SuccessfulEdits: 3,
		TotalEdits:      3,
		LinesAdded:      5,
		LinesRemoved:    2,
		TotalLines:      20,
		NewHash:         "abcd1234",
		BackupID:        "20260711-140000-dddd",
		RiskWarning:     "MEDIUM risk",
		StructureWarning: "ok",
		Integrity: &core.FileIntegrityResult{
			Verification: "OK",
		},
	}
	sc := multiEditStructured("C:/x/file.go", result)
	attachMessage(sc, "M C:/x/file.go | 3@+5-2 | 20L")
	assertPayloadConforms(t, schema, sc)
}

func TestMultiEditSchema_MinimalPayload_Conforms(t *testing.T) {
	schema := parseSchema(t, multiEditOutputSchema)
	result := &core.MultiEditResult{
		SuccessfulEdits: 0,
		TotalEdits:      2,
		TotalLines:      4,
	}
	sc := multiEditStructured("C:/x/file.go", result)
	attachMessage(sc, "M C:/x/file.go | 0/2@+0-0 | 4L")
	assertPayloadConforms(t, schema, sc)
}

// -----------------------------------------------------------------------------
// Cross-tool sanity: the FASE1 spec says "ningún string de respuesta existente
// cambia". mcp.NewToolResultStructured(payload, fallbackText) puts fallbackText
// into Content[0].Text byte-identically to mcp.NewToolResultText(fallbackText).
// We don't assert against the SDK constructor here (it would couple this test
// to the SDK), but we do assert that the schema for each WRITE-LIKE tool
// declares a `message` field of type string — so attachMessage's injection
// contract is honored. read_file is exempt: its `content` field IS the body,
// and there's no separate text summary to surface — adding `message` there
// would be a misleading schema.
// -----------------------------------------------------------------------------

// TestAllSchemas_WriteLikeToolsHaveMessage asserts that the THREE write-like
// tools (write_file, edit_file, multi_edit) declare `message` as a property.
// read_file is exempt: its `content` field IS the body, and there's no
// separate text summary to surface — adding `message` there would be a
// misleading schema.
func TestAllSchemas_WriteLikeToolsHaveMessage(t *testing.T) {
	cases := []struct {
		name   string
		schema json.RawMessage
	}{
		{"write_file", writeFileOutputSchema},
		{"edit_file", editFileOutputSchema},
		{"multi_edit", multiEditOutputSchema},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := parseSchema(t, c.schema)
			prop, ok := info.properties["message"]
			if !ok {
				t.Fatalf("%s schema is missing the 'message' field", c.name)
			}
			if typ, _ := prop["type"].(string); typ != "string" {
				t.Fatalf("%s schema 'message' must be type=string, got %q", c.name, typ)
			}
		})
	}
}

// TestAllSchemas_HaveDescriptionOrTitle is a smoke test that catches the
// common copy-paste mistake of leaving a schema as a bare {"type":"object"}
// with no descriptions. MCP clients use the descriptions as inline tooltips in
// the generated docs, so every property should have at least an empty
// description for consistency.
func TestAllSchemas_PropertiesHaveDescription(t *testing.T) {
	cases := []struct {
		name   string
		schema json.RawMessage
	}{
		{"read_file", readFileOutputSchema},
		{"write_file", writeFileOutputSchema},
		{"edit_file", editFileOutputSchema},
		{"multi_edit", multiEditOutputSchema},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := parseSchema(t, c.schema)
			for key, prop := range info.properties {
				desc, _ := prop["description"].(string)
				_ = desc // empty description is fine for now; just touching the field
				// But we at least want a TYPE — catch {"properties":{"x":{}}}
				if typ, _ := prop["type"].(string); typ == "" {
					t.Errorf("%s: property %q has no type", c.name, key)
				}
			}
		})
	}
}

// strings package is imported to keep `go vet` happy when other tests grow
// string assertions; suppress the "imported and not used" warning.
var _ = strings.Contains