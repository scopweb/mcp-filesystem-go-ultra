package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestPipelineRefreshesAutoOCCBaseline verifies that a pipeline edit refreshes
// the auto-OCC baseline, so a file the session read then modified via pipeline
// is not falsely flagged as an external change on a later edit.
func TestPipelineRefreshesAutoOCCBaseline(t *testing.T) {
	defer SetAutoOCCMode("warn")
	SetAutoOCCMode("warn")

	dir := t.TempDir()
	path := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(path, []byte("foo bar\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Session read records the original baseline.
	RecordReadHash(NormalizePath(path), contentHashFNV("foo bar\n"))

	engine := newTestEngine(dir)
	executor := NewPipelineExecutor(engine)
	res, err := executor.Execute(context.Background(), PipelineRequest{
		Name: "refresh-test",
		Steps: []PipelineStep{
			{ID: "e", Action: "edit", Params: map[string]interface{}{
				"path": path, "old_text": "foo", "new_text": "FOO",
			}},
		},
	})
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	if !res.Success {
		t.Fatalf("pipeline not successful: %+v", res)
	}

	raw, _ := os.ReadFile(path)
	// Auto-OCC against the new on-disk hash must be OK — the pipeline refreshed
	// the baseline, so the session's own pipeline edit isn't flagged external.
	if sig := CheckAutoOCC(NormalizePath(path), contentHashFNV(string(raw))); sig.Status != FeedbackOK {
		t.Errorf("expected OK after pipeline refreshed baseline, got status=%v pattern=%v", sig.Status, sig.Pattern)
	}
}
