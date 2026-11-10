package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: the change analysis attached to an executed edit was computed
// from the file AFTER the write. The literal old_text was already gone, so
// the exact-occurrence count was 0 and the analysis reported
// "No exact matches found - operation will have no effect" for an edit that
// had just been applied successfully.
//
// The analysis must describe what actually happened.
func TestEditFile_Analysis_ReportsTheAppliedEdit(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("ZZ1\nZZ2\nZZ3\nEE\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := e.EditFile(context.Background(), path, "ZZ1\nZZ2\nZZ3", "YY1\nYY2\nYY3\nYY4", false, false, false)
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if res.ReplacementCount != 1 {
		t.Fatalf("want 1 replacement, got %d", res.ReplacementCount)
	}
	if res.Analysis == nil {
		t.Fatal("expected an analysis to be attached to the result")
	}
	a := res.Analysis

	if strings.Contains(a.Preview, "No exact matches found") {
		t.Errorf("analysis preview contradicts the applied edit: %q", a.Preview)
	}
	if strings.Contains(a.Impact, "No changes will be made") {
		t.Errorf("analysis impact contradicts the applied edit: %q", a.Impact)
	}
	for _, factor := range a.RiskFactors {
		if strings.Contains(factor, "No matches found") {
			t.Errorf("analysis risk factor contradicts the applied edit: %q", factor)
		}
	}
	if got := a.Metadata["occurrences"]; got != 1 {
		t.Errorf("analysis occurrences = %v, want 1", got)
	}
	if a.LinesAdded != res.LinesAdded || a.LinesRemoved != res.LinesRemoved {
		t.Errorf("analysis lines +%d -%d disagree with result +%d -%d",
			a.LinesAdded, a.LinesRemoved, res.LinesAdded, res.LinesRemoved)
	}
}

// The CRLF file that triggered the original report: the engine matches it
// (EOL-tolerant), so the analysis must not claim the edit found nothing.
func TestEditFile_Analysis_CRLFFile(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	path := filepath.Join(dir, "f.cs")
	if err := os.WriteFile(path, []byte("using System;\r\nusing System.Text;\r\nvar x = 1;\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := e.EditFile(context.Background(), path,
		"using System;\nusing System.Text;\nvar x = 1;",
		"using System;\nusing System.Text;\nvar x = 2;",
		false, false, false)
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if res.Analysis == nil {
		t.Fatal("expected an analysis to be attached to the result")
	}
	if strings.Contains(res.Analysis.Preview, "No exact matches found") {
		t.Errorf("analysis preview contradicts the applied edit: %q", res.Analysis.Preview)
	}
	if strings.Contains(res.Analysis.Impact, "No changes will be made") {
		t.Errorf("analysis impact contradicts the applied edit: %q", res.Analysis.Impact)
	}
	if got := res.Analysis.Metadata["occurrences"]; got != 1 {
		t.Errorf("analysis occurrences = %v, want 1", got)
	}
}

// A tolerant match (tabs in the file, spaces in old_text) is not an exact
// literal match, but the edit DID apply: the analysis must say so and name
// the fallback instead of claiming the operation had no effect.
func TestEditFile_Analysis_TolerantMatchIsReported(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("head\n\tvalue = 1\ntail\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := e.EditFile(context.Background(), path, "head\n    value = 1\ntail", "head\n    value = 2\ntail", false, false, true)
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if res.MatchMethod != "tolerant_whitespace" {
		t.Fatalf("want tolerant_whitespace match, got %q", res.MatchMethod)
	}
	a := res.Analysis
	if a == nil {
		t.Fatal("expected an analysis to be attached to the result")
	}
	if strings.Contains(a.Preview, "No exact matches found") {
		t.Errorf("analysis preview contradicts the applied edit: %q", a.Preview)
	}
	if strings.Contains(a.Impact, "No changes will be made") {
		t.Errorf("analysis impact contradicts the applied edit: %q", a.Impact)
	}
	if got := a.Metadata["match_method"]; got != "tolerant_whitespace" {
		t.Errorf("analysis match_method = %v, want tolerant_whitespace", got)
	}
	if got := a.Metadata["occurrences"]; got != 1 {
		t.Errorf("analysis occurrences = %v, want 1", got)
	}
}

// The pre-flight API keeps its original semantics: nothing has run yet, so
// an unmatched old_text is still reported as unmatched.
func TestAnalyzeEditChange_PreflightStillReportsUnmatched(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a, err := e.AnalyzeEditChange(context.Background(), path, "does-not-exist", "x")
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}
	if !strings.Contains(a.Preview, "No exact matches found") {
		t.Errorf("pre-flight analysis must still report the missing match: %q", a.Preview)
	}
	if !strings.Contains(a.Impact, "No changes will be made") {
		t.Errorf("pre-flight analysis must still report the missing match: %q", a.Impact)
	}
	if got := a.Metadata["exact_match"]; got != false {
		t.Errorf("pre-flight exact_match = %v, want false", got)
	}
}
