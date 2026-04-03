package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// TestBug27_MultiEditAtomicRollback verifies that multi_edit does NOT write
// partial changes when one of the edits fails. Previously, if edit #1 succeeded
// but edit #2 failed (old_text mismatch), the file was written with only edit #1
// applied, causing file truncation.
func TestBug27_MultiEditAtomicRollback(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	// Create a test file with known content
	testFile := filepath.Join(tmpDir, "atomic_test.go")
	originalContent := `package main

func hello() {
	fmt.Println("Hello, World!")
}

func goodbye() {
	fmt.Println("Goodbye, World!")
}

func main() {
	hello()
	goodbye()
}
`
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Attempt multi_edit where edit #1 will succeed but edit #2 will fail
	edits := []core.MultiEditOperation{
		{
			OldText: `fmt.Println("Hello, World!")`,
			NewText: `fmt.Println("Hi, World!")`,
		},
		{
			OldText: `THIS TEXT DOES NOT EXIST IN THE FILE`,
			NewText: `replacement that should not appear`,
		},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false, false)

	// Should return an error (atomic rollback)
	if err == nil {
		t.Fatal("Expected error for atomic rollback, got nil")
	}

	// Error message should mention atomic rollback
	if !strings.Contains(err.Error(), "atomic rollback") {
		t.Errorf("Expected 'atomic rollback' in error, got: %v", err)
	}

	// Result should still be returned with details
	if result == nil {
		t.Fatal("Expected result with details, got nil")
	}
	if result.SuccessfulEdits != 1 {
		t.Errorf("Expected 1 successful edit (rolled back), got %d", result.SuccessfulEdits)
	}
	if result.FailedEdits != 1 {
		t.Errorf("Expected 1 failed edit, got %d", result.FailedEdits)
	}

	// CRITICAL: File should NOT be modified
	actualContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file after multi_edit: %v", err)
	}
	if string(actualContent) != originalContent {
		t.Errorf("File was modified despite atomic rollback!\nExpected %d bytes, got %d bytes",
			len(originalContent), len(actualContent))
		if strings.Contains(string(actualContent), "Hi, World!") {
			t.Error("Edit #1 was applied but should have been rolled back")
		}
	}
}

// TestBug27_MultiEditAllSucceed verifies that multi_edit still works
// normally when all edits succeed.
func TestBug27_MultiEditAllSucceed(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "success_test.go")
	originalContent := `package main

func hello() {
	fmt.Println("Hello")
}

func goodbye() {
	fmt.Println("Goodbye")
}
`
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	edits := []core.MultiEditOperation{
		{
			OldText: `fmt.Println("Hello")`,
			NewText: `fmt.Println("Hi")`,
		},
		{
			OldText: `fmt.Println("Goodbye")`,
			NewText: `fmt.Println("Bye")`,
		},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false, false)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if result.SuccessfulEdits != 2 {
		t.Errorf("Expected 2 successful edits, got %d", result.SuccessfulEdits)
	}
	if result.FailedEdits != 0 {
		t.Errorf("Expected 0 failed edits, got %d", result.FailedEdits)
	}

	// File should contain both changes
	actualContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !strings.Contains(string(actualContent), `fmt.Println("Hi")`) {
		t.Error("Edit #1 not applied")
	}
	if !strings.Contains(string(actualContent), `fmt.Println("Bye")`) {
		t.Error("Edit #2 not applied")
	}
}

// TestBug27_MultiEditQuoteMismatch simulates the real-world scenario:
// HTML/JS with mixed quotes causing old_text mismatch on the second edit.
func TestBug27_MultiEditQuoteMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	engine := createTestEngineWithPath(t, tmpDir)
	defer engine.Close()

	testFile := filepath.Join(tmpDir, "quotes_test.html")
	originalContent := `<script>
function initTable() {
    var row = $('<tr/>');
    row.append($('<td/>').text("Name"));
    row.append($('<td/>').text("Value"));
    $('#table').append(row);
}

function gestionData() {
    // 400 lines of important code here
    var data = fetchData();
    processData(data);
    renderResults(data);
}
</script>
`
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Edit #1 succeeds, Edit #2 fails due to quote mismatch (double vs single quotes)
	edits := []core.MultiEditOperation{
		{
			OldText: `row.append($('<td/>').text("Name"));`,
			NewText: `row.append($('<td/>').text("Nombre"));`,
		},
		{
			// Mismatch: using double quotes for $("<tr/>") but file has single quotes $('<tr/>')
			OldText: `var row = $("<tr/>");`,
			NewText: `var row = $("<tr/>").addClass("data-row");`,
		},
	}

	result, err := engine.MultiEdit(context.Background(), testFile, edits, false, false)

	// Should fail atomically
	if err == nil {
		t.Fatal("Expected atomic rollback error due to quote mismatch")
	}
	if !strings.Contains(err.Error(), "atomic rollback") {
		t.Errorf("Expected 'atomic rollback' in error, got: %v", err)
	}

	// File must be unchanged — gestionData must NOT disappear
	actualContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(actualContent) != originalContent {
		t.Error("File was modified despite atomic rollback — this is Bug #27!")
	}
	if !strings.Contains(string(actualContent), "gestionData") {
		t.Error("CRITICAL: gestionData block disappeared — file was truncated!")
	}

	// Result should have details
	if result == nil {
		t.Fatal("Expected result with edit details")
	}
	if result.FailedEdits != 1 {
		t.Errorf("Expected 1 failed edit, got %d", result.FailedEdits)
	}
}
