package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymbolRanges_GoExact(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sym.go")
	src := "package p\n\nfunc Exported(n int) {\n\t_ = n\n}\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	res := AnalyzeSymbols(nil, p, "Exported", 10)
	if len(res.Findings) != 1 {
		t.Fatalf("%+v", res.Findings)
	}
	f := res.Findings[0]
	if f.RangeStatus != symbolRangeExact || f.StartLine != 3 || f.EndLine != 5 || f.Signature != "Exported(int)" {
		t.Fatalf("%+v", f)
	}
	if res.ContentHash == "" || f.ContentHash != res.ContentHash {
		t.Fatalf("hash result=%q finding=%q", res.ContentHash, f.ContentHash)
	}
}

func TestSymbolRanges_CSharpExactUnavailableAndRazor(t *testing.T) {
	dir := t.TempDir()
	cs := filepath.Join(dir, "Pda.cs")
	body := "namespace N;\npublic class PdaController {\n    public void Escriu() {\n        var x = 1;\n    }\n    public void Interpolated() {\n        var s = $\"a{b}c\";\n    }\n    public int One() => 1;\n}\n"
	if err := os.WriteFile(cs, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res := AnalyzeSymbols(nil, cs, "", 20)
	byName := map[string]AnalyzeFinding{}
	for _, f := range res.Findings {
		byName[f.Symbol] = f
	}
	escriu := byName["Escriu"]
	if escriu.RangeStatus != symbolRangeExact || escriu.StartLine != 3 || escriu.EndLine != 5 || escriu.Container != "PdaController" {
		t.Fatalf("Escriu %+v", escriu)
	}
	if byName["Interpolated"].RangeStatus != symbolRangeUnavailable || byName["Interpolated"].EndLine != 0 {
		t.Fatalf("Interpolated %+v", byName["Interpolated"])
	}
	if byName["One"].RangeStatus != symbolRangeExact || byName["One"].EndLine != 9 {
		t.Fatalf("One %+v", byName["One"])
	}
	if byName["PdaController"].RangeStatus != symbolRangeUnavailable {
		t.Fatalf("class containing an interpolated string must not claim an exact range: %+v", byName["PdaController"])
	}
	plain := filepath.Join(dir, "Plain.cs")
	if err := os.WriteFile(plain, []byte("public class Plain {\n    public void M() {\n        var x = 1;\n    }\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	plainRes := AnalyzeSymbols(nil, plain, "Plain", 5)
	if len(plainRes.Findings) != 1 || plainRes.Findings[0].RangeStatus != symbolRangeExact || plainRes.Findings[0].StartLine != 1 || plainRes.Findings[0].EndLine != 5 {
		t.Fatalf("plain type %+v", plainRes.Findings)
	}

	razor := filepath.Join(dir, "Page.razor")
	rb := "@page \"/\"\n<h1>Hi</h1>\n@code {\n    public void Escriu()\n    {\n        var x = 1;\n    }\n}\n"
	if err := os.WriteFile(razor, []byte(rb), 0644); err != nil {
		t.Fatal(err)
	}
	rz := AnalyzeSymbols(nil, razor, "Escriu", 10)
	if len(rz.Findings) != 1 {
		t.Fatalf("%+v", rz.Findings)
	}
	f := rz.Findings[0]
	if f.RangeStatus != symbolRangeExact || f.StartLine != 4 || f.EndLine != 7 {
		t.Fatalf("razor %+v", f)
	}
}
