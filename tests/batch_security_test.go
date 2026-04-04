package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

// TestBatchOperationsPathValidation verifies that batch operations enforce
// --allowed-paths access control. Before the fix, batch_operations bypassed
// isPathAllowed entirely, allowing reads/writes outside the sandbox.
func TestBatchOperationsPathValidation(t *testing.T) {
	// Create two temp dirs: one allowed, one forbidden
	allowedDir := t.TempDir()
	forbiddenDir := t.TempDir()

	// Create a target file in the forbidden directory
	forbiddenFile := filepath.Join(forbiddenDir, "secret.txt")
	if err := os.WriteFile(forbiddenFile, []byte("secret data"), 0644); err != nil {
		t.Fatalf("Failed to create forbidden file: %v", err)
	}

	// Create engine with only allowedDir in AllowedPaths
	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedDir},
		ParallelOps:  2,
	}
	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	batchManager := core.NewBatchOperationManager("", 10)
	batchManager.SetEngine(engine)

	t.Run("write to forbidden path is blocked", func(t *testing.T) {
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "write", Path: filepath.Join(forbiddenDir, "injected.txt"), Content: "pwned"},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch write to forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")
	})

	t.Run("edit in forbidden path is blocked", func(t *testing.T) {
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "edit", Path: forbiddenFile, OldText: "secret", NewText: "hacked"},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch edit to forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")

		// Verify file was NOT modified
		content, _ := os.ReadFile(forbiddenFile)
		if string(content) != "secret data" {
			t.Errorf("forbidden file was modified: got %q", string(content))
		}
	})

	t.Run("move from forbidden path is blocked", func(t *testing.T) {
		dest := filepath.Join(allowedDir, "stolen.txt")
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "move", Source: forbiddenFile, Destination: dest},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch move from forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")
	})

	t.Run("move to forbidden path is blocked", func(t *testing.T) {
		allowedFile := filepath.Join(allowedDir, "safe.txt")
		os.WriteFile(allowedFile, []byte("safe"), 0644)
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "move", Source: allowedFile, Destination: filepath.Join(forbiddenDir, "exfil.txt")},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch move to forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")
	})

	t.Run("copy from forbidden path is blocked", func(t *testing.T) {
		dest := filepath.Join(allowedDir, "copied.txt")
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "copy", Source: forbiddenFile, Destination: dest},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch copy from forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")
	})

	t.Run("delete in forbidden path is blocked", func(t *testing.T) {
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "delete", Path: forbiddenFile},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch delete of forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")

		// Verify file still exists
		if _, err := os.Stat(forbiddenFile); os.IsNotExist(err) {
			t.Error("forbidden file was deleted")
		}
	})

	t.Run("create_dir in forbidden path is blocked", func(t *testing.T) {
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "create_dir", Path: filepath.Join(forbiddenDir, "subdir")},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch create_dir in forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")
	})

	t.Run("search_and_replace in forbidden path is blocked", func(t *testing.T) {
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "search_and_replace", Path: forbiddenFile, OldText: "secret", NewText: "hacked"},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch search_and_replace to forbidden path should have been blocked")
		}
		assertContains(t, result.Errors, "access denied")
	})

	t.Run("allowed paths still work", func(t *testing.T) {
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "write", Path: filepath.Join(allowedDir, "ok.txt"), Content: "allowed content"},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if !result.Success {
			t.Errorf("batch write to allowed path should succeed, errors: %v", result.Errors)
		}
	})

	t.Run("mixed allowed and forbidden is blocked atomically", func(t *testing.T) {
		req := core.BatchRequest{
			Operations: []core.FileOperation{
				{Type: "write", Path: filepath.Join(allowedDir, "good.txt"), Content: "ok"},
				{Type: "write", Path: filepath.Join(forbiddenDir, "bad.txt"), Content: "pwned"},
			},
		}
		result := batchManager.ExecuteBatch(req)
		if result.Success {
			t.Error("batch with mixed allowed/forbidden paths should be blocked in validation")
		}
		assertContains(t, result.Errors, "access denied")
	})
}

// TestBatchPathValidationJSON tests via JSON parsing (same path as main.go handler)
func TestBatchPathValidationJSON(t *testing.T) {
	allowedDir := t.TempDir()
	forbiddenDir := t.TempDir()

	cacheInstance, _ := cache.NewIntelligentCache(1024 * 1024)
	config := &core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowedDir},
		ParallelOps:  2,
	}
	engine, _ := core.NewUltraFastEngine(config)

	batchManager := core.NewBatchOperationManager("", 10)
	batchManager.SetEngine(engine)

	// Simulate what main.go does: parse JSON then execute
	reqJSON := `{
		"operations": [
			{"type": "write", "path": "` + filepath.ToSlash(filepath.Join(forbiddenDir, "pwned.txt")) + `", "content": "hacked"}
		]
	}`

	var batchReq core.BatchRequest
	if err := json.Unmarshal([]byte(reqJSON), &batchReq); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	result := batchManager.ExecuteBatch(batchReq)
	if result.Success {
		t.Error("JSON-parsed batch to forbidden path should be blocked")
	}
}

func assertContains(t *testing.T, items []string, substr string) {
	t.Helper()
	for _, item := range items {
		if contains(item, substr) {
			return
		}
	}
	t.Errorf("expected errors to contain %q, got: %v", substr, items)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
