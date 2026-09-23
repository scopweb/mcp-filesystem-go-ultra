package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func twoFilePatch() string {
	return `--- a/a.txt
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
}

func TestApplyMultiFilePatch_TwoFiles(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	e3File(t, a, "a\n")
	e3File(t, b, "b\n")
	files, err := ParseUnifiedDiffs(twoFilePatch())
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: dir, CreateBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("%+v", res)
	}
	e3Content(t, a, "A\n")
	e3Content(t, b, "B\n")
}

func TestApplyMultiFilePatch_SecondHunkFailsFirstIntact(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	e3File(t, a, "a\n")
	e3File(t, b, "b\n")
	patch := `--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-a
+A
--- a/b.txt
+++ b/b.txt
@@ -1 +1 @@
-NOPE
+B
`
	files, err := ParseUnifiedDiffs(patch)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: dir, CreateBackup: true})
	if err == nil {
		t.Fatal("expected hunk failure")
	}
	pe := AsPatchError(err)
	if pe.Reason != PatchReasonContextNotFound {
		t.Fatalf("%+v", pe)
	}
	e3Content(t, a, "a\n")
	e3Content(t, b, "b\n")
}

func TestApplyMultiFilePatch_DryRunNoWrites(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	e3File(t, a, "a\n")
	e3File(t, b, "b\n")
	files, err := ParseUnifiedDiffs(twoFilePatch())
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: dir, DryRun: true, CreateBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("%+v", res)
	}
	e3Content(t, a, "a\n")
	e3Content(t, b, "b\n")
}

func TestApplyMultiFilePatch_MissingDestRejected(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	e3File(t, filepath.Join(dir, "a.txt"), "a\n")
	files, err := ParseUnifiedDiffs(twoFilePatch())
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: dir})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("got %v", err)
	}
	e3Content(t, filepath.Join(dir, "a.txt"), "a\n")
}

func TestApplyMultiFilePatch_MissingBaseIsNotNotADirectory(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	files, err := ParseUnifiedDiffs(twoFilePatch())
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone")
	_, err = e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: missing})
	if err == nil || err.Error() != "base directory does not exist" || strings.Contains(err.Error(), "requires path to be a directory") {
		t.Fatalf("got %v", err)
	}
	file := filepath.Join(dir, "notdir.txt")
	e3File(t, file, "x\n")
	_, err = e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: file})
	if err == nil || !strings.Contains(err.Error(), "requires path to be a directory") {
		t.Fatalf("got %v", err)
	}
}

func TestApplyMultiFilePatch_NoMkdir(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	e3File(t, filepath.Join(dir, "a.txt"), "a\n")
	e3File(t, filepath.Join(dir, "b.txt"), "b\n")
	patch := `--- a/missing/a.txt
+++ b/missing/a.txt
@@ -1 +1 @@
-a
+A
--- a/b.txt
+++ b/b.txt
@@ -1 +1 @@
-b
+B
`
	files, err := ParseUnifiedDiffs(patch)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: dir})
	if err == nil {
		t.Fatal("expected missing dest")
	}
	if _, stErr := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(stErr) {
		t.Fatal("must not create directories")
	}
	e3Content(t, filepath.Join(dir, "b.txt"), "b\n")
}

func TestResolvePatchDest_RejectsEscape(t *testing.T) {
	dir := t.TempDir()
	if _, err := ResolvePatchDest(dir, "b/../..\\outside.txt"); err == nil {
		t.Fatal("expected escape reject")
	}
}

func TestAttachRollbackKeepsCompleteAndSurfacesPartial(t *testing.T) {
	commitErr := fmt.Errorf("write conflict")
	gotComplete := attachRollback(commitErr, "complete", nil)
	var done *RollbackError
	if !errors.As(gotComplete, &done) || done.Status != "complete" || done.Unwrap() != commitErr || !strings.Contains(gotComplete.Error(), "rollback complete") {
		t.Fatalf("complete rollback must stay visible, got %v", gotComplete)
	}
	got := attachRollback(commitErr, "partial", []string{"a.txt: changed"})
	var incomplete *RollbackError
	if !errors.As(got, &incomplete) || incomplete.Status != "partial" || incomplete.Unwrap() != commitErr {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(got.Error(), "a.txt") {
		t.Fatal(got)
	}
}

func TestApplyMultiFilePatch_ThirdCommitConflictIsPartial(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	e3File(t, a, "a\n")
	e3File(t, b, "b\n")
	e3File(t, c, "c\n")
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
--- a/c.txt
+++ b/c.txt
@@ -1 +1 @@
-c
+C
`
	files, err := ParseUnifiedDiffs(patch)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { commitProbe = nil })
	commitProbe = func(i int, path string) {
		if i != 2 {
			return
		}
		if err := os.WriteFile(a, []byte("external\n"), 0644); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(c, []byte("tampered\n"), 0644); err != nil {
			t.Error(err)
		}
	}
	_, err = e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: dir})
	var incomplete *RollbackError
	if !errors.As(err, &incomplete) || incomplete.Status != "partial" {
		t.Fatalf("want partial through ApplyMultiFilePatch, got %v", err)
	}
	e3Content(t, a, "external\n")
	e3Content(t, b, "b\n")
	e3Content(t, c, "tampered\n")
}

func TestApplyMultiFilePatch_SecondCommitFailRollsBackComplete(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir)
	defer e.Close()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	e3File(t, a, "a\n")
	e3File(t, b, "b\n")
	files, err := ParseUnifiedDiffs(twoFilePatch())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { commitProbe = nil })
	commitProbe = func(i int, path string) {
		if i != 1 {
			return
		}
		if err := os.WriteFile(b, []byte("tampered\n"), 0644); err != nil {
			t.Error(err)
		}
	}
	_, err = e.ApplyMultiFilePatch(context.Background(), files, MultiFilePatchOpts{BaseDir: dir})
	var incomplete *RollbackError
	if !errors.As(err, &incomplete) || incomplete.Status != "complete" || !strings.Contains(err.Error(), "rollback complete") {
		t.Fatalf("want visible complete rollback, got %v", err)
	}
	e3Content(t, a, "a\n")
	e3Content(t, b, "tampered\n")
}

func TestApplyUnifiedPatch_StillSingleFile(t *testing.T) {
	_, err := ApplyUnifiedPatch("a\n", twoFilePatch())
	if err == nil || !strings.Contains(err.Error(), "multi-file") {
		t.Fatalf("got %v", err)
	}
}
