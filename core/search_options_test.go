package core

import (
	"path/filepath"
	"testing"
)

func TestFileMatchesTypeFilter(t *testing.T) {
	goFile := filepath.Join("pkg", "foo.go")
	txtFile := filepath.Join("pkg", "notes.txt")
	cases := []struct {
		path    string
		filters []string
		want    bool
	}{
		{goFile, nil, true},
		{goFile, []string{".go"}, true},
		{goFile, []string{"go"}, true},
		{goFile, []string{"*.go"}, true},
		{goFile, []string{"**/*.go"}, true},
		{goFile, []string{".txt"}, false},
		{txtFile, []string{".go", ".txt"}, true},
		{txtFile, []string{"*.xyz"}, false},
	}
	for _, tc := range cases {
		got := FileMatchesTypeFilter(tc.path, tc.filters)
		if got != tc.want {
			t.Errorf("FileMatchesTypeFilter(%q, %v) = %v, want %v", tc.path, tc.filters, got, tc.want)
		}
	}
}

func TestParseFileTypeFilters(t *testing.T) {
	got := ParseFileTypeFilters(".go, *.txt,  ")
	if len(got) != 2 || got[0] != ".go" || got[1] != "*.txt" {
		t.Fatalf("got %#v", got)
	}
	if ParseFileTypeFilters("") != nil {
		t.Fatal("empty should be nil")
	}
}

func TestFileTypeGlobsForRipgrep(t *testing.T) {
	got := FileTypeGlobsForRipgrep([]string{".go", "*.ts", "md"})
	want := []string{"*.go", "*.ts", "*.md"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}
