package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// setupBug22Engine creates a test engine for multi_edit tests
func setupBug22Engine(t *testing.T) (*core.UltraFastEngine, string) {
	t.Helper()
	tempDir := t.TempDir()

	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{tempDir},
		ParallelOps:  2,
	}

	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	return engine, tempDir
}

// TestBug22_MultiEditOldStrAlias verifies that multi_edit accepts old_str/new_str
// parameter names (Claude Desktop convention) in addition to old_text/new_text.
func TestBug22_MultiEditOldStrAlias(t *testing.T) {
	engine, tempDir := setupBug22Engine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "alias_test.js")
	content := `function greet(name) {
    console.log("Hello, " + name);
    return true;
}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate JSON with old_str/new_str (what Claude Desktop sends)
	editsJSON := `[{"old_str": "console.log(\"Hello, \" + name);", "new_str": "console.log(\u0060Hello, ${name}\u0060);"}]`
	var edits []core.MultiEditOperation
	if err := json.Unmarshal([]byte(editsJSON), &edits); err != nil {
		t.Fatalf("Failed to parse edits JSON with old_str: %v", err)
	}

	if edits[0].OldText == "" {
		t.Fatal("OldText is empty — old_str alias not recognized")
	}
	if edits[0].NewText == "" {
		t.Fatal("NewText is empty — new_str alias not recognized")
	}

	result, err := engine.MultiEdit(ctx, filePath, edits, false)
	if err != nil {
		t.Fatalf("MultiEdit failed with old_str alias: %v", err)
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("Expected 1 successful edit, got %d", result.SuccessfulEdits)
	}

	// Verify file was modified
	modified, _ := os.ReadFile(filePath)
	if !strings.Contains(string(modified), "${name}") {
		t.Error("File was not modified — old_str/new_str edit did not apply")
	}
}

// TestBug22_MultiEditMixedAliases verifies mixing old_text and old_str in same edits array
func TestBug22_MultiEditMixedAliases(t *testing.T) {
	engine, tempDir := setupBug22Engine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "mixed_test.py")
	content := `def hello():
    print("hello")
    print("world")
`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Mix old_text and old_str in the same array
	editsJSON := `[
		{"old_text": "print(\"hello\")", "new_text": "print(\"HELLO\")"},
		{"old_str": "print(\"world\")", "new_str": "print(\"WORLD\")"}
	]`
	var edits []core.MultiEditOperation
	if err := json.Unmarshal([]byte(editsJSON), &edits); err != nil {
		t.Fatalf("Failed to parse mixed edits: %v", err)
	}

	if edits[0].OldText == "" || edits[1].OldText == "" {
		t.Fatalf("Mixed alias parsing failed: edit[0].OldText=%q, edit[1].OldText=%q", edits[0].OldText, edits[1].OldText)
	}

	result, err := engine.MultiEdit(ctx, filePath, edits, false)
	if err != nil {
		t.Fatalf("MultiEdit with mixed aliases failed: %v", err)
	}
	if result.SuccessfulEdits != 2 {
		t.Errorf("Expected 2 successful edits, got %d (failed: %d, errors: %v)",
			result.SuccessfulEdits, result.FailedEdits, result.Errors)
	}

	modified, _ := os.ReadFile(filePath)
	if !strings.Contains(string(modified), "HELLO") || !strings.Contains(string(modified), "WORLD") {
		t.Error("Not all mixed-alias edits were applied")
	}
}

// TestBug22_MultiEditLiteralEscapesContextValidation verifies that literal \n
// in old_text doesn't block context validation in multi_edit
func TestBug22_MultiEditLiteralEscapesContextValidation(t *testing.T) {
	engine, tempDir := setupBug22Engine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "escapes_test.txt")
	// File has real newlines
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate LLM sending literal \n (backslash + n) instead of real newline
	// In JSON, "line1\\nline2" becomes the string: line1\nline2 (with literal backslash-n)
	edits := []core.MultiEditOperation{
		{OldText: "line1\\nline2", NewText: "lineA\\nlineB"},
	}

	result, err := engine.MultiEdit(ctx, filePath, edits, false)
	if err != nil {
		t.Fatalf("MultiEdit failed with literal escapes: %v", err)
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("Expected 1 successful edit with literal escapes, got %d (failed: %d, errors: %v)",
			result.SuccessfulEdits, result.FailedEdits, result.Errors)
	}

	modified, _ := os.ReadFile(filePath)
	if !strings.Contains(string(modified), "lineA") {
		t.Error("Literal escape edit was not applied")
	}
}

// TestBug22_MultiEditOldTextStillWorks verifies that standard old_text/new_text still works
func TestBug22_MultiEditOldTextStillWorks(t *testing.T) {
	engine, tempDir := setupBug22Engine(t)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "standard_test.go")
	content := `package main

func main() {
	fmt.Println("old")
}
`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	editsJSON := `[{"old_text": "fmt.Println(\"old\")", "new_text": "fmt.Println(\"new\")"}]`
	var edits []core.MultiEditOperation
	if err := json.Unmarshal([]byte(editsJSON), &edits); err != nil {
		t.Fatalf("Failed to parse standard edits: %v", err)
	}

	result, err := engine.MultiEdit(ctx, filePath, edits, false)
	if err != nil {
		t.Fatalf("MultiEdit with standard old_text failed: %v", err)
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("Expected 1 successful edit, got %d", result.SuccessfulEdits)
	}

	modified, _ := os.ReadFile(filePath)
	if !strings.Contains(string(modified), `"new"`) {
		t.Error("Standard old_text edit was not applied")
	}
}

// TestBug22_PipelineTypeAlias verifies that pipeline steps accept "type" instead of "action"
// and auto-generate missing step IDs (Claude Desktop convention)
func TestBug22_PipelineTypeAlias(t *testing.T) {
	// Test that "type" is accepted as alias for "action" via UnmarshalJSON
	pipelineJSON := `{
		"name": "test-type-alias",
		"steps": [
			{
				"type": "search",
				"params": {"path": ".", "pattern": "TODO"}
			}
		]
	}`
	var req core.PipelineRequest
	if err := json.Unmarshal([]byte(pipelineJSON), &req); err != nil {
		t.Fatalf("Failed to parse pipeline with 'type' field: %v", err)
	}

	if req.Steps[0].Action != "search" {
		t.Errorf("Expected Action='search' from 'type' alias, got '%s'", req.Steps[0].Action)
	}

	// Validate should auto-generate the missing ID
	if err := req.Validate(); err != nil {
		t.Fatalf("Validation failed (should auto-generate ID): %v", err)
	}
	if req.Steps[0].ID != "step-0" {
		t.Errorf("Expected auto-generated ID 'step-0', got '%s'", req.Steps[0].ID)
	}
}

// TestBug22_PipelineExplicitIDPreserved verifies that explicit IDs are not overwritten
func TestBug22_PipelineExplicitIDPreserved(t *testing.T) {
	pipelineJSON := `{
		"name": "test-explicit-id",
		"steps": [
			{"id": "my-search", "action": "search", "params": {"path": ".", "pattern": "TODO"}},
			{"type": "count_occurrences", "params": {"pattern": "TODO"}, "input_from": "my-search"}
		]
	}`
	var req core.PipelineRequest
	if err := json.Unmarshal([]byte(pipelineJSON), &req); err != nil {
		t.Fatalf("Failed to parse pipeline: %v", err)
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// Explicit ID should be preserved
	if req.Steps[0].ID != "my-search" {
		t.Errorf("Explicit ID overwritten: expected 'my-search', got '%s'", req.Steps[0].ID)
	}
	// Missing ID should be auto-generated
	if req.Steps[1].ID != "step-1" {
		t.Errorf("Expected auto-generated ID 'step-1', got '%s'", req.Steps[1].ID)
	}
	// "type" should map to "action"
	if req.Steps[1].Action != "count_occurrences" {
		t.Errorf("Expected Action='count_occurrences' from 'type' alias, got '%s'", req.Steps[1].Action)
	}
}
