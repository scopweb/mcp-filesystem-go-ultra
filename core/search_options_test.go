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
	braces := ParseFileTypeFilters("{A.cs,B.cs}")
	if len(braces) != 2 || braces[0] != "{A.cs" || braces[1] != "B.cs}" {
		t.Fatalf("file_types must not expand braces, got %#v", braces)
	}
}

func TestFileTypeGlobsForRipgrep(t *testing.T) {
	got := FileTypeGlobsForRipgrep([]string{".go", "*.ts", "md", "A.cs", "Test/*.razor"})
	want := []string{"*.go", "*.ts", "*.md", "A.cs", "Test/*.razor"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestExpandIncludeFilter(t *testing.T) {
	got, err := ExpandIncludeFilter("{A.cs,B.cs}")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "A.cs" || got[1] != "B.cs" {
		t.Fatalf("names: %#v", got)
	}
	got, err = ExpandIncludeFilter("{Test,ObservacionesPedido}/*.razor")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Test/*.razor" || got[1] != "ObservacionesPedido/*.razor" {
		t.Fatalf("dirs: %#v", got)
	}
	got, err = ExpandIncludeFilter("*.go, *.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "*.go" || got[1] != "*.txt" {
		t.Fatalf("simple: %#v", got)
	}
	got, err = ExpandIncludeFilter("*.{go,razor}")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "*.go" || got[1] != "*.razor" {
		t.Fatalf("suffix: %#v", got)
	}
	for _, raw := range []string{"{A.cs,B.cs", "A.cs}", "{A,,B}", "{}", "{A,}", "{,B}"} {
		if _, err := ExpandIncludeFilter(raw); err == nil {
			t.Fatalf("invalid %q should error", raw)
		}
	}
	if _, err := ExpandIncludeFilter("{A.cs, B.cs}"); err != nil {
		t.Fatal(err)
	}
}

func TestFileMatchesIncludeBraces(t *testing.T) {
	root := filepath.Join("proj")
	names, err := ExpandIncludeFilter("{A.cs,B.cs}")
	if err != nil {
		t.Fatal(err)
	}
	if !FileMatchesTypeFilterIn(filepath.Join(root, "A.cs"), root, names) {
		t.Fatal("A.cs should match")
	}
	if !FileMatchesTypeFilterIn(filepath.Join(root, "B.cs"), root, names) {
		t.Fatal("B.cs should match")
	}
	if FileMatchesTypeFilterIn(filepath.Join(root, "C.cs"), root, names) {
		t.Fatal("C.cs should not match")
	}
	globs, err := ExpandIncludeFilter("{Test,ObservacionesPedido}/*.razor")
	if err != nil {
		t.Fatal(err)
	}
	if !FileMatchesTypeFilterIn(filepath.Join(root, "Test", "Alpha.razor"), root, globs) {
		t.Fatal("Test/Alpha.razor should match")
	}
	if !FileMatchesTypeFilterIn(filepath.Join(root, "ObservacionesPedido", "Beta.razor"), root, globs) {
		t.Fatal("ObservacionesPedido/Beta.razor should match")
	}
	if FileMatchesTypeFilterIn(filepath.Join(root, "Other", "Gamma.razor"), root, globs) {
		t.Fatal("Other/Gamma.razor should not match")
	}
	if FileMatchesTypeFilterIn(filepath.Join(root, "Pages", "Test", "Nested.razor"), root, globs) {
		t.Fatal("nested Test/*.razor should not match without **")
	}
	nested, err := ExpandIncludeFilter("**/{Test,ObservacionesPedido}/*.razor")
	if err != nil {
		t.Fatal(err)
	}
	if !FileMatchesTypeFilterIn(filepath.Join(root, "Pages", "Test", "Nested.razor"), root, nested) {
		t.Fatal("**/{Test}/*.razor should match nested")
	}
}
