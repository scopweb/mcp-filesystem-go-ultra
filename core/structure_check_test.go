package core

import "testing"

func TestDelimiterBalance(t *testing.T) {
	cases := []struct {
		name                 string
		in                   string
		wCurly, wRound, wSqr int
	}{
		{"balanced", "func f() {}\n", 0, 0, 0},
		{"open_curly", "func f() {\n", 1, 0, 0},
		{"close_extra", "}\n", -1, 0, 0},
		{"brace_in_string", `x := "{"` + "\n", 0, 0, 0},
		{"paren_in_string", `s := "f("` + "\n", 0, 0, 0},
		{"brace_in_line_comment", "// { ( [\n", 0, 0, 0},
		{"brace_in_block_comment", "/* { ( [ */\n", 0, 0, 0},
		{"nested", "{[()]}\n", 0, 0, 0},
		{"escaped_quote_in_string", `a := "he said \"{\""` + "\n", 0, 0, 0},
		{"raw_string_backtick", "a := `{` \n", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, r, s := delimiterBalance(tc.in)
			if c != tc.wCurly || r != tc.wRound || s != tc.wSqr {
				t.Errorf("delimiterBalance(%q) = (%d,%d,%d), want (%d,%d,%d)",
					tc.in, c, r, s, tc.wCurly, tc.wRound, tc.wSqr)
			}
		})
	}
}

func TestCheckBalanceDelta(t *testing.T) {
	// Old balanced, new unbalanced -> warning (the point-2 scenario: deleting a
	// @code/func body and leaving a dangling brace).
	old := "public class Foo {\n  void M() {\n  }\n}\n"
	broken := "public class Foo {\n  void M() {\n  }\n" // missing closing brace
	if w := CheckBalanceDelta(old, broken, "Foo.cs"); w == "" {
		t.Error("expected a warning when an edit leaves curly braces unbalanced")
	}

	// Old balanced, new balanced -> no warning.
	fixed := "public class Foo {\n  void N() {\n  }\n}\n"
	if w := CheckBalanceDelta(old, fixed, "Foo.cs"); w != "" {
		t.Errorf("expected no warning for a balanced edit, got %q", w)
	}

	// Non-code extension -> never warns even if unbalanced.
	if w := CheckBalanceDelta("{\n", "{\n{\n", "notes.txt"); w != "" {
		t.Errorf("expected no warning for non-code extension, got %q", w)
	}

	// Pre-existing imbalance (old already unbalanced) -> no warning (delta only).
	alreadyBad := "func f() {\n"
	stillBad := "func g() {\n"
	if w := CheckBalanceDelta(alreadyBad, stillBad, "x.go"); w != "" {
		t.Errorf("expected no warning when imbalance pre-existed, got %q", w)
	}
}
