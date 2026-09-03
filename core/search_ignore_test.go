package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/mcp"
)

func TestSmartSearch_RespectsGitignore(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("hidden.txt\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "visible.txt"), []byte("needle"), 0644)
	_ = os.WriteFile(filepath.Join(root, "hidden.txt"), []byte("needle"), 0644)

	engine := newTestEngine(root)
	defer engine.Close()

	resp, err := engine.SmartSearch(context.Background(), mcp.CallToolRequest{
		Arguments: map[string]interface{}{
			"path": root, "pattern": "hidden", "include_content": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := resp.Content[0].Text
	if strings.Contains(text, "hidden.txt") {
		t.Fatalf("gitignore should hide hidden.txt:\n%s", text)
	}
}

func TestAdvancedTextSearch_NoIgnoreFindsGitignored(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("hidden.txt\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "hidden.txt"), []byte("secret-token\n"), 0644)

	engine := newTestEngine(root)
	defer engine.Close()
	engine.ripgrepAvailable = false

	resp, err := engine.AdvancedTextSearch(context.Background(), mcp.CallToolRequest{
		Arguments: map[string]interface{}{
			"path": root, "pattern": "secret-token", "case_sensitive": true,
			"no_ignore": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Content[0].Text, "secret-token") {
		t.Fatalf("no_ignore should find gitignored content:\n%s", resp.Content[0].Text)
	}
}
