package core

import (
	"reflect"
	"testing"
)

func TestParseRootsMode(t *testing.T) {
	if ParseRootsMode("") != RootsReplace {
		t.Fatal("default replace")
	}
	if ParseRootsMode("UNION") != RootsUnion {
		t.Fatal("union")
	}
	if ParseRootsMode("ignore") != RootsIgnore {
		t.Fatal("ignore")
	}
}

func TestMergeAllowedPaths_Replace(t *testing.T) {
	got, src := MergeAllowedPaths([]string{`C:\cli`}, []string{`D:\root`}, RootsReplace)
	if src != AllowedSourceRoots || !reflect.DeepEqual(got, []string{`D:\root`}) {
		t.Fatalf("got %v src %s", got, src)
	}
}

func TestMergeAllowedPaths_ReplaceEmptyKeepsCLI(t *testing.T) {
	got, src := MergeAllowedPaths([]string{`C:\cli`}, nil, RootsReplace)
	if src != AllowedSourceCLI || !reflect.DeepEqual(got, []string{`C:\cli`}) {
		t.Fatalf("got %v src %s", got, src)
	}
}

func TestMergeAllowedPaths_Union(t *testing.T) {
	got, src := MergeAllowedPaths([]string{`C:\cli`}, []string{`D:\root`}, RootsUnion)
	if src != AllowedSourceUnion || len(got) != 2 {
		t.Fatalf("got %v src %s", got, src)
	}
}

func TestMergeAllowedPaths_Ignore(t *testing.T) {
	got, src := MergeAllowedPaths([]string{`C:\cli`}, []string{`D:\root`}, RootsIgnore)
	if src != AllowedSourceCLI || !reflect.DeepEqual(got, []string{`C:\cli`}) {
		t.Fatalf("got %v src %s", got, src)
	}
}
