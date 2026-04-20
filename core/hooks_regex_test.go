package core

import "testing"

func TestMatchesPattern_Regex(t *testing.T) {
	hm := NewHookManager()

	cases := []struct {
		name    string
		pattern string
		tool    string
		want    bool
	}{
		{"regex prefix matches write_*", "re:^write_.*$", "write_file", true},
		{"regex prefix matches multi-edit", "re:^(edit|multi_edit)$", "multi_edit", true},
		{"regex prefix rejects non-match", "re:^write_.*$", "read_file", false},
		{"regex alternation", "re:^(read|search)_files?$", "search_files", true},
		{"regex with anchors", "re:^backup$", "backup", true},
		{"regex partial match without anchors", "re:file", "write_file", true},
		{"invalid regex returns false (no panic)", "re:[invalid(", "anything", false},
		{"exact match still works", "write_file", "write_file", true},
		{"wildcard still works", "write_*", "write_file", true},
		{"regex takes precedence over wildcard syntax", "re:^\\*.*", "*wildcard", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hm.matchesPattern(tc.pattern, tc.tool)
			if got != tc.want {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tc.pattern, tc.tool, got, tc.want)
			}
		})
	}
}

func TestMatchesRegex_Caches(t *testing.T) {
	hm := NewHookManager()

	// First call compiles and caches.
	if !hm.matchesPattern("re:^edit_.*$", "edit_file") {
		t.Fatal("first call should match")
	}

	// Second call should hit the cache and still match.
	if !hm.matchesPattern("re:^edit_.*$", "edit_file") {
		t.Fatal("second call (cached) should still match")
	}

	// Invalid regex is cached as non-matching and must not panic on re-use.
	if hm.matchesPattern("re:(unclosed", "anything") {
		t.Fatal("invalid regex should never match")
	}
	if hm.matchesPattern("re:(unclosed", "anything") {
		t.Fatal("cached invalid regex should still not match")
	}
}
