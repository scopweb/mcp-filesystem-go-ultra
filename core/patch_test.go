package core

import (
	"strings"
	"testing"
)

func TestApplyUnifiedPatch_SimpleReplace(t *testing.T) {
	old := "alpha\nbeta\ngamma\n"
	patch := `--- a/f.txt
+++ b/f.txt
@@ -1,3 +1,3 @@
 alpha
-beta
+BETA
 gamma
`
	got, err := ApplyUnifiedPatch(old, patch)
	if err != nil {
		t.Fatal(err)
	}
	if got != "alpha\nBETA\ngamma\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyUnifiedPatch_CRLFPreserved(t *testing.T) {
	old := "a\r\nb\r\nc\r\n"
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	got, err := ApplyUnifiedPatch(old, patch)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\r\nB\r\nc\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyUnifiedPatch_ContextMismatch(t *testing.T) {
	old := "alpha\nbeta\n"
	patch := `--- a/f.txt
+++ b/f.txt
@@ -1,2 +1,2 @@
 nope
-beta
+BETA
`
	if _, err := ApplyUnifiedPatch(old, patch); err == nil {
		t.Fatal("expected mismatch")
	}
}

func TestParseUnifiedDiff_RejectsMultiFile(t *testing.T) {
	patch := `--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-a
+A
--- a/b.txt
+++ b/b.txt
@@ -1 +1 @@
-b
+B
`
	if _, err := ParseUnifiedDiff(patch); err == nil || !strings.Contains(err.Error(), "multi-file") {
		t.Fatalf("got %v", err)
	}
}

func TestPatchHeaderMatches(t *testing.T) {
	if !PatchHeaderMatches("b/core/engine.go", `C:\proj\core\engine.go`) {
		t.Fatal("suffix")
	}
	if !PatchHeaderMatches("/dev/null", "x") {
		t.Fatal("devnull")
	}
	if PatchHeaderMatches("b/other.go", `C:\proj\engine.go`) {
		t.Fatal("should not match")
	}
}
