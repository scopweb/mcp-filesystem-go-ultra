package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBug28_EditFileWithHTMLContent verifies that edit_file works with old_text
// containing HTML tags, attributes with quotes, special characters (<, >, ", @, /).
// Previously, validateEditContext could reject HTML content before performIntelligentEdit
// had a chance to match it with its 8 fallback strategies.
func TestBug28_EditFileWithHTMLContent(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	tests := []struct {
		name    string
		content string
		oldText string
		newText string
	}{
		{
			name:    "HTML form with attributes",
			content: "<html>\n<body>\n  <form name=\"fcargas\" id=\"fcargas\" action=\"/ferratge/cargas\" method=\"post\"></form>\n  <partial name=\"__Ferratge_FormPedido\" />\n</body>\n</html>\n",
			oldText: "  <form name=\"fcargas\" id=\"fcargas\" action=\"/ferratge/cargas\" method=\"post\"></form>\n  <partial name=\"__Ferratge_FormPedido\" />",
			newText: "  <!-- replaced -->",
		},
		{
			name:    "Single HTML tag with quotes",
			content: "<div class=\"container\" data-value=\"test@email.com\">\n  <span>Hello</span>\n</div>\n",
			oldText: "<div class=\"container\" data-value=\"test@email.com\">",
			newText: "<div class=\"wrapper\">",
		},
		{
			name:    "Razor/CSHTML syntax with @",
			content: "@model MyApp.Models.Order\n@{\n    ViewData[\"Title\"] = \"Comanda\";\n}\n<h1>@ViewData[\"Title\"]</h1>\n",
			oldText: "@{\n    ViewData[\"Title\"] = \"Comanda\";\n}",
			newText: "@{\n    ViewData[\"Title\"] = \"Pedido\";\n}",
		},
		{
			name:    "Self-closing tags with slashes",
			content: "<div>\n  <img src=\"/images/logo.png\" alt=\"Logo\" />\n  <br />\n  <input type=\"text\" value=\"a/b/c\" />\n</div>\n",
			oldText: "  <img src=\"/images/logo.png\" alt=\"Logo\" />",
			newText: "  <img src=\"/assets/logo.svg\" alt=\"Company Logo\" />",
		},
		{
			name:    "HTML with angle brackets in content",
			content: "<script>\n  if (a < b && c > d) {\n    console.log(\"<done>\");\n  }\n</script>\n",
			oldText: "  if (a < b && c > d) {\n    console.log(\"<done>\");\n  }",
			newText: "  if (a <= b && c >= d) {\n    console.log(\"<finished>\");\n  }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safeName := strings.NewReplacer(" ", "_", "/", "-", "\\", "-").Replace(tt.name)
			testFile := filepath.Join(tempDir, "bug28_"+safeName+".html")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			result, err := engine.EditFile(context.Background(), testFile, tt.oldText, tt.newText, false)
			if err != nil {
				t.Fatalf("EditFile failed with HTML content: %v", err)
			}

			if result.ReplacementCount == 0 {
				t.Error("Expected at least 1 replacement, got 0")
			}

			// Verify the file was actually modified
			modified, err := os.ReadFile(testFile)
			if err != nil {
				t.Fatalf("Failed to read modified file: %v", err)
			}

			if !strings.Contains(string(modified), tt.newText) {
				t.Errorf("Modified file does not contain expected new_text.\nGot:\n%s", string(modified))
			}

			if strings.Contains(string(modified), tt.oldText) {
				t.Errorf("Modified file still contains old_text that should have been replaced")
			}
		})
	}
}

// TestBug28_ValidateContextNoLongerBlocks verifies that validateEditContext
// not finding the text does not prevent performIntelligentEdit from succeeding.
// This tests the case where whitespace differences cause validation to fail
// but the intelligent edit's fallback strategies can still match.
func TestBug28_ValidateContextNoLongerBlocks(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// File uses tabs, but old_text uses spaces (common LLM behavior)
	content := "<div>\n\t<form action=\"/submit\" method=\"post\">\n\t\t<input type=\"text\" />\n\t</form>\n</div>\n"
	oldText := "    <form action=\"/submit\" method=\"post\">\n        <input type=\"text\" />\n    </form>"
	newText := "    <form action=\"/api/submit\" method=\"post\">\n        <input type=\"email\" />\n    </form>"

	testFile := filepath.Join(tempDir, "bug28_whitespace.html")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	result, err := engine.EditFile(context.Background(), testFile, oldText, newText, false)
	if err != nil {
		t.Fatalf("EditFile should succeed via fallback strategies, got error: %v", err)
	}

	if result.ReplacementCount == 0 {
		t.Error("Expected replacement via whitespace normalization fallback")
	}
}
