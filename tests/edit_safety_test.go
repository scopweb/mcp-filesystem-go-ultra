package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/core"
)

// TestEditSafetyValidator tests the EditSafetyValidator with various scenarios
func TestEditSafetyValidator(t *testing.T) {
	validator := core.NewEditSafetyValidator(true)

	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	testContent := `    // Orders list
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;
    private Dictionary<int, bool> listadoValidados = new Dictionary<int, bool>();`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name      string
		oldText   string
		newText   string
		expectOK  bool
		expectMsg string
	}{
		{
			name: "Exact multiline match",
			oldText: `    // Orders list
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;
    private Dictionary<int, bool> listadoValidados = new Dictionary<int, bool>();`,
			newText: `    // Orders list (updated)
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;`,
			expectOK:  true,
			expectMsg: "exact match found",
		},
		{
			name: "Single line match",
			oldText: `    private Dictionary<int, bool> listadoValidados = new Dictionary<int, bool>();`,
			newText: `    // Removed: Dictionary not needed`,
			expectOK:  true,
			expectMsg: "exact match found",
		},
		{
			name: "Nonexistent text",
			oldText: `    private string nonexistentVariable = "";`,
			newText: `    // removed`,
			expectOK:  false,
			expectMsg: "not found",
		},
		{
			name: "Partial line match should still work",
			oldText: `    private bool isFiltered = false;`,
			newText: `    private bool isFiltered = true;`,
			expectOK:  true,
			expectMsg: "match found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateEditSafety(testFile, tt.oldText, tt.newText)

			if tt.expectOK && !result.CanProceed {
				t.Errorf("Expected edit to be valid but got: %s", result.Diagnostics.ErrorDetails)
			}
			if !tt.expectOK && result.CanProceed {
				t.Errorf("Expected edit to be invalid but it passed")
			}

			if result.MatchFound != tt.expectOK {
				t.Errorf("Expected MatchFound=%v, got %v", tt.expectOK, result.MatchFound)
			}

			t.Logf("✓ Validation: Match=%v, Confidence=%.0f%%", result.MatchFound, result.Confidence*100)
		})
	}
}

// TestEditSafetyWithLineEndingVariations tests handling of different line endings
func TestEditSafetyWithLineEndingVariations(t *testing.T) {
	validator := core.NewEditSafetyValidator(true)
	tmpDir := t.TempDir()

	// Test with different line ending types
	tests := []struct {
		name       string
		content    string
		oldText    string
		newText    string
		shouldWork bool
	}{
		{
			name:       "Unix line endings (LF)",
			content:    "line1\nline2\nline3",
			oldText:    "line2",
			newText:    "line2_modified",
			shouldWork: true,
		},
		{
			name:       "Windows line endings (CRLF)",
			content:    "line1\r\nline2\r\nline3",
			oldText:    "line2",
			newText:    "line2_modified",
			shouldWork: true,
		},
		{
			name:       "Mixed line endings",
			content:    "line1\r\nline2\nline3\r\n",
			oldText:    "line2",
			newText:    "line2_modified",
			shouldWork: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.name+".txt")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			result := validator.ValidateEditSafety(testFile, tt.oldText, tt.newText)

			if tt.shouldWork && !result.CanProceed {
				t.Errorf("Expected edit to succeed with %s, got: %s",
					tt.name, result.Diagnostics.ErrorDetails)
			}

			t.Logf("Line endings: %s, Match: %v", result.Diagnostics.LineEndingType, result.MatchFound)
		})
	}
}

// TestEditSafetyMultilineScenarios tests the problematic multiline scenario from Bug #8
func TestEditSafetyMultilineScenarios(t *testing.T) {
	validator := core.NewEditSafetyValidator(true)
	tmpDir := t.TempDir()

	// Recreate the exact scenario from Bug #8
	content := `    // Orders list
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;
    private Dictionary<int, bool> listadoValidados = new Dictionary<int, bool>();

    public void SomeMethod()
    {
        // some code
    }`

	testFile := filepath.Join(tmpDir, "bug8_scenario.cs")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// This is the exact old_text that failed in Bug #8
	oldText := `    // Orders list
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;
    private Dictionary<int, bool> listadoValidados = new Dictionary<int, bool>();`

	newText := `    // Orders list
    public List<C1Pedidos> Orders { get; set; } = new();
    private List<C1Pedidos> filteredOrders = new();
    private bool isFiltered = false;`

	result := validator.ValidateEditSafety(testFile, oldText, newText)

	t.Logf("Bug #8 Scenario Test:")
	t.Logf("  Match Found: %v", result.MatchFound)
	t.Logf("  Can Proceed: %v", result.CanProceed)
	t.Logf("  Confidence: %.0f%%", result.Confidence*100)
	t.Logf("  Old Text Lines: %d", result.Diagnostics.OldTextLineCount)
	t.Logf("  Exact Matches: %d", result.Diagnostics.ExactMatches)
	t.Logf("  Normalized Matches: %d", result.Diagnostics.NormalizedMatches)

	if !result.CanProceed {
		t.Errorf("Bug #8 scenario should be solvable. Error: %s", result.Diagnostics.ErrorDetails)
		t.Logf("Suggestion: %s", result.SuggestedAlternative)
	}

	// Test the recommended strategy
	strategy := validator.RecommendedEditStrategy(result)
	t.Logf("Recommended strategy: %s", strategy)
}

// TestEditSafetyVerifyResult tests verification of edit results
func TestEditSafetyVerifyResult(t *testing.T) {
	validator := core.NewEditSafetyValidator(true)
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "verify_test.txt")
	oldContent := "old content here"
	newContent := "new content here"

	if err := os.WriteFile(testFile, []byte(oldContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Simulate successful edit
	if err := os.WriteFile(testFile, []byte(newContent), 0644); err != nil {
		t.Fatalf("Failed to update test file: %v", err)
	}

	// Verify the edit
	verified, msg := validator.VerifyEditResult(testFile, oldContent, newContent)

	t.Logf("Verification result: %v - %s", verified, msg)

	if !verified {
		t.Errorf("Expected verification to pass, but got: %s", msg)
	}
}

// TestEditSafetyDetailedLog tests detailed logging functionality
func TestEditSafetyDetailedLog(t *testing.T) {
	validator := core.NewEditSafetyValidator(true)
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "log_test.txt")
	testContent := "test content\nline 2\nline 3"

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	oldText := "line 2"
	newText := "line 2 modified"

	// Validate
	validation := validator.ValidateEditSafety(testFile, oldText, newText)

	// Create detailed log
	log := &core.DetailedEditLog{
		Timestamp:        time.Now().Format("2006-01-02 15:04:05"),
		Operation:        "edit_file",
		FilePath:         testFile,
		ValidationResult: validation,
		PreEditHash:      validation.FileHash,
		Success:          validation.CanProceed,
		ExecutionTimeMs:  125,
	}

	formattedLog := log.Format()
	t.Log("\nDetailed Log Output:")
	t.Log(formattedLog)

	// Verify log contains expected information
	expectedStrings := []string{
		"DETAILED EDIT LOG",
		"Operation",
		"VALIDATION",
		"Confidence",
	}

	for _, expected := range expectedStrings {
		if !containsString(formattedLog, expected) {
			t.Errorf("Expected log to contain '%s'", expected)
		}
	}
}

// TestEditSafetyWithLargeFiles tests handling of large multiline edits
func TestEditSafetyWithLargeFiles(t *testing.T) {
	validator := core.NewEditSafetyValidator(true)
	tmpDir := t.TempDir()

	// Create a large file with 100 lines
	var content strings.Builder
	for i := 0; i < 100; i++ {
		content.WriteString(fmt.Sprintf("    Line %d: content here\n", i))
	}

	testFile := filepath.Join(tmpDir, "large_file.txt")
	if err := os.WriteFile(testFile, []byte(content.String()), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Try to edit lines 10-15
	oldText := `    Line 10: content here
    Line 11: content here
    Line 12: content here
    Line 13: content here
    Line 14: content here
    Line 15: content here`

	newText := `    Line 10-15: DELETED`

	result := validator.ValidateEditSafety(testFile, oldText, newText)

	t.Logf("Large file multiline edit:")
	t.Logf("  Old Text Lines: %d", result.Diagnostics.OldTextLineCount)
	t.Logf("  Match Found: %v", result.MatchFound)
	t.Logf("  Confidence: %.0f%%", result.Confidence*100)

	if result.CanProceed {
		strategy := validator.RecommendedEditStrategy(result)
		t.Logf("Strategy: %s", strategy)
	}
}

// Helper function to check if string contains substring
func containsString(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// BenchmarkEditSafetyValidator benchmarks validation performance
func BenchmarkEditSafetyValidator(b *testing.B) {
	validator := core.NewEditSafetyValidator(false)
	tmpDir := b.TempDir()

	testFile := filepath.Join(tmpDir, "bench_test.txt")
	content := strings.Repeat("test line\n", 1000)
	os.WriteFile(testFile, []byte(content), 0644)

	oldText := "test line"
	newText := "modified line"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateEditSafety(testFile, oldText, newText)
	}
}
