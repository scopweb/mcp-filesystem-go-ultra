package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeSymbols_CSharpAndJS(t *testing.T) {
	dir := t.TempDir()
	cs := filepath.Join(dir, "Pda.cs")
	if err := os.WriteFile(cs, []byte("namespace N;\npublic class PdaController {\n    public void CargaRestore() {}\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := AnalyzeSymbols(cs, "", 50)
	if res.Status != "ok" {
		t.Fatalf("status %s msg %s", res.Status, res.Message)
	}
	sawClass, sawMethod := false, false
	for _, f := range res.Findings {
		if f.Symbol == "PdaController" && f.Kind == "type" && f.Line == 2 {
			sawClass = true
		}
		if f.Symbol == "CargaRestore" && f.Kind == "method" && f.Line == 3 {
			sawMethod = true
		}
	}
	if !sawClass || !sawMethod {
		t.Fatalf("missing symbols: %+v", res.Findings)
	}

	js := filepath.Join(dir, "vue.js")
	if err := os.WriteFile(js, []byte("export function loadPda() {}\nclass View {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	jsRes := AnalyzeSymbols(js, "loadPda", 20)
	if len(jsRes.Findings) != 1 || jsRes.Findings[0].Line != 1 {
		t.Fatalf("js: %+v", jsRes.Findings)
	}
}

func TestAnalyzeSymbols_SQLProcedure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nav.sql")
	body := "CREATE OR ALTER PROCEDURE dbo.CargaRestore\nAS\nSELECT 1\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res := AnalyzeSymbols(p, "", 10)
	if len(res.Findings) != 1 || res.Findings[0].Symbol != "CargaRestore" {
		t.Fatalf("%+v", res.Findings)
	}
}
