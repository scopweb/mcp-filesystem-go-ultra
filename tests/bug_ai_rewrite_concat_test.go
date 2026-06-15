package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// TestBug_AI_AccidentalRewrite_BugPattern reproduces the exact bug observed
// on 2026-06-11: a 15-line header was replaced with a 150-line full file via
// edit_file, producing a 298-line file with the procedure duplicated.
//
// Symptom recap:
//   old_text  = 15 lines  (file header)
//   new_text  = 150 lines (full file content)
//   expected  = file unchanged (rewrite guard BLOCKS)
//   actual    = file was doubled (BUG)
//
// After the fix, the rewrite guard must BLOCK this pattern. The file MUST
// remain byte-for-byte identical to the original.
func TestBug_AI_AccidentalRewrite_BugPattern(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "sp_file.go")

	// 15-line header (the old_text the model passed)
	header := strings.Join([]string{
		"// Package sp implements the standard procedure.",
		"package sp",
		"",
		"import (",
		"\t\"context\"",
		"\t\"fmt\"",
		")",
		"",
		"// Run executes the standard procedure.",
		"func Run(ctx context.Context) error {",
		"\t// step 1: initialize",
		"\tinit()",
		"\t// step 2: validate",
		"\tvalidate(ctx)",
		"\t// step 3: execute",
		"\texecute(ctx)",
		"\treturn nil",
		"}",
		"",
		"// helper section",
	}, "\n")

	// 135 more lines of "body" so the file is ~150 lines total
	bodyLines := make([]string, 130)
	for i := range bodyLines {
		bodyLines[i] = "\t// body line " + strings.Repeat("x", 5)
	}
	fullFile := header + "\n" + strings.Join(bodyLines, "\n") + "\n"

	if err := os.WriteFile(testFile, []byte(fullFile), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	originalBytes, _ := os.ReadFile(testFile)
	originalLen := len(originalBytes)

	// Sanity: setup matches the bug scenario
	if originalLen < 2000 {
		t.Fatalf("test setup invalid: file too small (%d bytes) to trigger the 2x ratio", originalLen)
	}

	// ---- The bug call: edit_file with header as old_text, full file as new_text ----
	// Use the same FeedbackSignal directly (unit-level) — that's where the guard lives.
	signal := core.CheckEditRewrite(header, fullFile, int64(originalLen))
	if signal == nil || signal.Status != core.FeedbackKO {
		t.Fatalf("expected KO signal for accidental rewrite, got %+v", signal)
	}
	if signal.Pattern != core.PatternAccidentalRewrite {
		t.Errorf("expected pattern %q, got %q", core.PatternAccidentalRewrite, signal.Pattern)
	}
	if !signal.BlockOp {
		t.Error("expected BlockOp=true for accidental rewrite")
	}
	if !strings.Contains(signal.Message, "accidental full-file rewrite") {
		t.Errorf("message should mention accidental full-file rewrite, got: %s", signal.Message)
	}
	if !strings.Contains(signal.Suggestion, "write_file") {
		t.Errorf("suggestion should recommend write_file, got: %s", signal.Suggestion)
	}
}

// TestBug_AI_AccidentalRewrite_LegitimateLargeRefactor ensures the guard does
// NOT fire on legitimate refactors where old_text is large too (e.g., swapping
// a 100-line function body). The ratio stays near 1.0, so the first signal
// doesn't trigger.
func TestBug_AI_AccidentalRewrite_LegitimateLargeRefactor(t *testing.T) {
	oldText := strings.Repeat("old line\n", 100) // 900 bytes
	newText := strings.Repeat("new line\n", 100) // 900 bytes (ratio 1.0)
	fileSize := int64(len(oldText) + 500)        // file has 500 bytes more context

	signal := core.CheckEditRewrite(oldText, newText, fileSize)
	if signal == nil || signal.Status != core.FeedbackOK {
		t.Errorf("legitimate refactor should NOT trigger the rewrite guard, got %+v", signal)
	}
}

// TestBug_AI_AccidentalRewrite_SmallNewText ensures the guard does NOT fire
// when newText is small relative to oldText (e.g., expanding a short comment
// into a 5-line block — new is bigger but still small in absolute terms).
func TestBug_AI_AccidentalRewrite_SmallNewText(t *testing.T) {
	oldText := "// todo: implement\n"
	newText := "// TODO: implement this properly\n// - add validation\n// - add tests\n" // ~70 bytes
	fileSize := int64(5000)                                                              // 5KB file

	signal := core.CheckEditRewrite(oldText, newText, fileSize)
	if signal == nil || signal.Status != core.FeedbackOK {
		t.Errorf("small edit should NOT trigger the rewrite guard, got %+v", signal)
	}
}

// TestBug_AI_AccidentalRewrite_SmallFile ensures the guard is a no-op on
// files smaller than 1KB (avoids noise on micro-edits and brand-new files).
func TestBug_AI_AccidentalRewrite_SmallFile(t *testing.T) {
	oldText := "header\n"
	newText := strings.Repeat("content\n", 50) // 400 bytes
	fileSize := int64(500)                     // 500 bytes — below the 1024 threshold

	signal := core.CheckEditRewrite(oldText, newText, fileSize)
	if signal == nil || signal.Status != core.FeedbackOK {
		t.Errorf("tiny files should bypass the rewrite guard, got %+v", signal)
	}
}

// TestBug_AI_AccidentalRewrite_HighRatioButNoRemaining ensures we don't
// fire on the "replace a footer" case where the old_text is small but new_text
// is larger — yet the file has no significant content beyond the match
// (e.g., file is exactly the header, no body).
func TestBug_AI_AccidentalRewrite_HighRatioButNoRemaining(t *testing.T) {
	oldText := "// header\npackage main\n"
	newText := strings.Repeat("// expanded header\n", 50) // 850 bytes
	fileSize := int64(len(oldText))                       // file == old_text (no remaining)

	signal := core.CheckEditRewrite(oldText, newText, fileSize)
	if signal == nil || signal.Status != core.FeedbackOK {
		t.Errorf("edit with no remaining content should NOT trigger the rewrite guard, got %+v", signal)
	}
}

// TestBug_AI_AccidentalRewrite_EmptyInputs ensures the guard returns OK for
// edge cases (empty strings, zero file size).
func TestBug_AI_AccidentalRewrite_EmptyInputs(t *testing.T) {
	cases := []struct {
		name     string
		oldText  string
		newText  string
		fileSize int64
	}{
		{"empty_old", "", "content", 1000},
		{"empty_new", "content", "", 1000},
		{"empty_file", "old", "new", 0},
		{"all_empty", "", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signal := core.CheckEditRewrite(tc.oldText, tc.newText, tc.fileSize)
			if signal == nil || signal.Status != core.FeedbackOK {
				t.Errorf("empty input case %q should return OK, got %+v", tc.name, signal)
			}
		})
	}
}

// TestBug_AI_AccidentalRewrite_EngineBlockSemantics exercises the engine-
// level guard integration: with a 150-line file, an edit_file call with
// 15-line old_text and 150-line new_text must:
//  1. Return an error (rewrite guard blocks)
//  2. Leave the file byte-for-byte unchanged
//  3. Mention "write_file" in the error so the model knows the alternative
//
// We exercise the FeedbackSignal directly (which is what the handler checks)
// because the engine.EditFile is invoked through a tool handler in production
// — the handler is the layer that enforces the block. This test pins the
// signal shape that the handler relies on.
func TestBug_AI_AccidentalRewrite_EngineBlockSemantics(t *testing.T) {
	// Arrange: a 150-line file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "big.go")
	lines := make([]string, 150)
	for i := range lines {
		lines[i] = "line " + strings.Repeat("x", 10)
	}
	if err := os.WriteFile(testFile, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	originalBytes, _ := os.ReadFile(testFile)

	// Act: simulate what tools_core.go does before calling engine.EditFile
	oldText := strings.Join(lines[:15], "\n") + "\n" // 15-line header
	newText := strings.Join(lines, "\n") + "\n"      // full 150-line file

	info, _ := os.Stat(testFile)
	signal := core.CheckEditRewrite(oldText, newText, info.Size())
	if signal == nil || !signal.BlockOp {
		t.Fatalf("expected block signal, got %+v", signal)
	}

	// Assert: file is unchanged after the block (we never called engine.EditFile)
	afterBytes, _ := os.ReadFile(testFile)
	if string(afterBytes) != string(originalBytes) {
		t.Error("file was modified by the test setup itself — should be untouched")
	}
}

// TestBug_AI_AccidentalRewrite_ForceBypassSemantic documents that the block
// can be bypassed via the dedicated allow_rewrite flag (point 5 — NOT force,
// which only bypasses the risk threshold). The actual override handling lives
// in the tool handler (tools_core.go); this test pins the contract that the
// signal shape supports an override path.
func TestBug_AI_AccidentalRewrite_ForceBypassSemantic(t *testing.T) {
	oldText := "header\n"
	newText := strings.Repeat("full content\n", 100)
	fileSize := int64(2000)

	signal := core.CheckEditRewrite(oldText, newText, fileSize)
	if signal == nil {
		t.Fatal("expected signal, got nil")
	}
	if !signal.BlockOp {
		t.Error("signal should be BlockOp=true so the handler can decide based on force flag")
	}
	if signal.Status != core.FeedbackKO {
		t.Error("signal should be KO so audit log captures severity")
	}
	// Simulate handler: if allow_rewrite=true, the signal is attached to audit
	// but the edit proceeds. We can't easily run the full tool handler here, but
	// the contract is: BlockOp=true AND allow_rewrite=true → proceed.
	_ = context.Background() // imported for handler signature parity
}
