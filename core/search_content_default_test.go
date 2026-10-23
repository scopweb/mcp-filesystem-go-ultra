package core

import "testing"

func TestSearchContentByDefault(t *testing.T) {
	cases := []struct {
		pattern    string
		includeSet bool
		want       bool
	}{
		{pattern: "preview.*range", want: true},
		{pattern: "foo[0-9]+", want: true},
		{pattern: `foo\.bar`, want: true},
		{pattern: "a|b", want: true},
		{pattern: "(foo)", want: true},
		{pattern: "^start", want: true},
		{pattern: "end$", want: true},
		{pattern: "a{2}", want: true},
		{pattern: "hidden-token", want: true},
		{pattern: "*.go", want: false},
		{pattern: "file?.txt", want: false},
		{pattern: "Reports.*", want: false},
		{pattern: "*.*", want: false},
		{pattern: "foo.*", want: false},
		{pattern: "*.go", includeSet: true, want: true},
		{pattern: "preview.*range", includeSet: true, want: true},
	}
	for _, tc := range cases {
		got := SearchContentByDefault(tc.pattern, tc.includeSet)
		if got != tc.want {
			t.Errorf("SearchContentByDefault(%q, %v)=%v want %v", tc.pattern, tc.includeSet, got, tc.want)
		}
	}
}

func TestIsGlobPattern_RegexIsNotAGlob(t *testing.T) {
	if isGlobPattern("preview.*range") || isGlobPattern("foo[0-9]+") {
		t.Fatal("regex must not use filepath.Match")
	}
	if !isGlobPattern("*.go") || !isGlobPattern("Reports.*") || !isGlobPattern("file?.txt") {
		t.Fatal("filename globs must stay globs")
	}
}
