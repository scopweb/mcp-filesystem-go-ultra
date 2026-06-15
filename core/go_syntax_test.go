package core

import "testing"

func TestCheckGoSyntax(t *testing.T) {
	good := "package main\n\nfunc main() {}\n"
	broken := "package main\n\nfunc main() {\n" // missing closing brace

	if w := CheckGoSyntax(good, broken, "x.go"); w == "" {
		t.Error("expected a warning when an edit breaks the Go parse")
	}
	if w := CheckGoSyntax(good, good, "x.go"); w != "" {
		t.Errorf("expected no warning for valid Go, got %q", w)
	}
	// Delta: if the file was already broken before the edit, stay silent.
	if w := CheckGoSyntax(broken, broken, "x.go"); w != "" {
		t.Errorf("expected no warning when old already broken, got %q", w)
	}
	// Non-Go extension → AST not used.
	if w := CheckGoSyntax(good, broken, "x.txt"); w != "" {
		t.Errorf("expected no warning for non-go extension, got %q", w)
	}
}

// TestCheckStructureDelta_DispatchesGoVsBrace shows the dispatch: .go gets a real
// parse (catches non-brace syntax errors), other languages get brace balance
// (which by design does NOT flag brace-balanced token junk).
func TestCheckStructureDelta_DispatchesGoVsBrace(t *testing.T) {
	goodGo := "package main\n\nfunc main() { x := 1; _ = x }\n"
	badGoTokens := "package main\n\nfunc main() { x := := 1 }\n" // braces balanced, bad token
	if w := CheckStructureDelta(goodGo, badGoTokens, "x.go"); w == "" {
		t.Error("AST should catch a non-brace syntax error in .go")
	}

	goodCs := "class F { void M() { } }\n"
	balancedJunkCs := "class F { void M() { int int x } }\n" // brace-balanced nonsense
	if w := CheckStructureDelta(goodCs, balancedJunkCs, "F.cs"); w != "" {
		t.Errorf("brace check should not flag brace-balanced .cs token junk, got %q", w)
	}
}
