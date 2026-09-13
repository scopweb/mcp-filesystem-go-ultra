package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeSymbols_ExportedFunc(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(p, []byte("package p\nfunc Exported() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := AnalyzeSymbols(p, "Exported", 50)
	if len(got.Findings) == 0 {
		t.Fatalf("status=%s msg=%s", got.Status, got.Message)
	}
	if got.Findings[0].Symbol != "Exported" {
		t.Fatalf("%+v", got.Findings)
	}
}

func TestAnalyzeSec_HardcodedCredential(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.go")
	src := "package p\nvar password = \"supersecretvalue\"\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	got := AnalyzeSec(p, 50)
	if len(got.Findings) == 0 {
		t.Fatalf("expected secret finding, status=%s", got.Status)
	}
}
