package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// expectedHookEvents is the canonical list of hook event names exposed by the
// HookManager (see core/hooks.go). The example files must cover ALL of them
// (even if disabled by default) so a user copying the example gets a complete
// reference for the events the server can dispatch.
//
// Keep in sync with the HookEvent constants in core/hooks.go.
var expectedHookEvents = []string{
	"pre-write", "post-write",
	"pre-edit", "post-edit",
	"pre-delete", "post-delete",
	"pre-create", "post-create",
	"pre-move", "post-move",
	"pre-copy", "post-copy",
	"pre-read", "post-read",
	"pre-search", "post-search",
}

// loadHookConfig loads a hooks JSON file from the project root's examples/
// directory. The tests run from the project root (go test ./tests/...).
func loadHookConfig(t *testing.T, name string) map[string]interface{} {
	t.Helper()
	// Walk up from the test binary's working directory to the project root.
	// The tests are in <project>/tests/ and the examples are in <project>/examples/.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Try the current directory first (typical: go test from project root),
	// then walk up to find the examples/ directory.
	candidates := []string{
		filepath.Join(wd, "examples", name),
		filepath.Join(wd, "..", "examples", name),
		filepath.Join(wd, "..", "..", "examples", name),
	}
	var path string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Skipf("examples/%s not found (looked in: %v)", name, candidates)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v (file is not valid JSON — this is the bug that prompted this test)", path, err)
	}
	return cfg
}

// TestHooksExampleJSONIsValid guards against the regression of duplicate
// content being pasted into examples/hooks.example.json (the file is the
// primary copy-paste template for users; if it's broken, the user's setup
// breaks too).
func TestHooksExampleJSONIsValid(t *testing.T) {
	cfg := loadHookConfig(t, "hooks.example.json")
	hooks, ok := cfg["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks.example.json: top-level \"hooks\" object missing or wrong type")
	}
	// Must cover every event (even if disabled)
	for _, evt := range expectedHookEvents {
		if _, ok := hooks[evt]; !ok {
			t.Errorf("hooks.example.json: missing event %q (must be present in the reference template, even if all entries are disabled)", evt)
		}
	}
}

// TestHooksTestJSONIsValid guards the same for the testing config.
func TestHooksTestJSONIsValid(t *testing.T) {
	cfg := loadHookConfig(t, "hooks-test.json")
	hooks, ok := cfg["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks-test.json: top-level \"hooks\" object missing or wrong type")
	}
	for _, evt := range expectedHookEvents {
		if _, ok := hooks[evt]; !ok {
			t.Errorf("hooks-test.json: missing event %q", evt)
		}
	}
}

// TestHooksExampleHasNoDuplicateStructure is a focused regression test: in
// the original broken file, the JSON had duplicate content after the closing
// `}` of the root object, which made the file invalid. The fix trims it back
// to a single well-formed object. This test detects the same class of
// corruption (extra trailing content after the root) on either example file.
func TestHooksExampleHasNoDuplicateStructure(t *testing.T) {
	wd, _ := os.Getwd()
	path := filepath.Join(wd, "examples", "hooks.example.json")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(wd, "..", "examples", "hooks.example.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("hooks.example.json not found")
	}
	// El bug original era JSON duplicado pegado tras el `}` raíz; json.Unmarshal
	// lo ignora en silencio. Con un Decoder, tras decodificar un valor dec.More()
	// delata cualquier token extra. Agnóstico a indentación, orden de claves y CRLF.
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var parsed interface{}
	if err := dec.Decode(&parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if dec.More() {
		t.Errorf("hooks.example.json tiene contenido tras el objeto raíz (el bug de duplicado)")
	}
}
