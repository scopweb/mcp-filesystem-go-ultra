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

func TestMergeAllowedPaths_OpenCodeUnionKeepsCLI(t *testing.T) {
	cli := []string{`C:\a`, `D:\b`, `E:\c`}
	client := []string{`C:\workspace`}
	got, src := MergeAllowedPaths(cli, client, RootsUnion)
	if src != AllowedSourceUnion || len(got) != 4 {
		t.Fatalf("OpenCode union dropped CLI paths: %v src %s", got, src)
	}
}

func TestMergeAllowedPaths_VSCodeUnionAddsEncodedRoot(t *testing.T) {
	decoded, err := FileURIToPath("file:///c%3A/temp")
	if err != nil {
		t.Fatal(err)
	}
	got, src := MergeAllowedPaths([]string{`D:\other`}, []string{decoded}, RootsUnion)
	if src != AllowedSourceUnion || len(got) != 2 {
		t.Fatalf("VS Code union: %v src %s", got, src)
	}
	replaced, src := MergeAllowedPaths([]string{`D:\other`}, []string{decoded}, RootsReplace)
	if src != AllowedSourceRoots || len(replaced) != 1 || replaced[0] != decoded {
		t.Fatalf("replace should keep only the client root, got %v src %s", replaced, src)
	}
}
func TestSetAllowedPaths_EmptyNonInsecureDenies(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths(nil, AllowedSourceRoots)
	if engine.IsPathAllowed(`C:\Windows\System32\drivers\etc\hosts`) {
		t.Fatal("empty non-insecure roots must not open the disk")
	}
	if engine.AllowedSource() == AllowedSourceInsecure {
		t.Fatal("empty roots must not be relabeled insecure")
	}
}

func TestMergeAllowedPaths_Ignore(t *testing.T) {
	got, src := MergeAllowedPaths([]string{`C:\cli`}, []string{`D:\root`}, RootsIgnore)
	if src != AllowedSourceCLI || !reflect.DeepEqual(got, []string{`C:\cli`}) {
		t.Fatalf("got %v src %s", got, src)
	}
}
