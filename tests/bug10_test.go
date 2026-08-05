package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Bug 10 (feedback 2026-08-05, BUG 1): when old_text ends with "\n" and
// new_text does not, the line following the match must stay on its own line.
// Previously the splice fused it onto the last line of new_text, breaking
// builds (e.g. "{        using var conn ...").

func TestBug10_TrailingNewlineMatchDoesNotFuseNextLine(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "public PressupostTeoWin? Carrega(string codiPressupost)\n    {\n        using var conn = new SqlConnection(_connectionString);\n    }\n"
	oldText := "public PressupostTeoWin? Carrega(string codiPressupost)\n    {\n"
	newText := "public PressupostTeoWin? Carrega(string codiPressupost)\n    {"

	testFile := filepath.Join(tempDir, "bug10_basic.cs")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := engine.EditFile(context.Background(), testFile, oldText, newText, false, false, false); err != nil {
		t.Fatalf("EditFile: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "public PressupostTeoWin? Carrega(string codiPressupost)\n    {\n        using var conn = new SqlConnection(_connectionString);\n    }\n"
	if string(got) != want {
		t.Errorf("line fusion detected.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestBug10_TrailingNewlineMatchCRLF(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "line1\r\nline2\r\nline3\r\n"
	oldText := "line1\r\nline2\r\n"
	newText := "line1\r\nline2"

	testFile := filepath.Join(tempDir, "bug10_crlf.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := engine.EditFile(context.Background(), testFile, oldText, newText, false, false, false); err != nil {
		t.Fatalf("EditFile: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "line1\r\nline2\r\nline3\r\n"
	if string(got) != want {
		t.Errorf("CRLF boundary not preserved.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestBug10_MatchAtEndOfFileNoNewlineAdded(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "aaa\nbbb\n"
	oldText := "bbb\n"
	newText := "bbb"

	testFile := filepath.Join(tempDir, "bug10_eof.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := engine.EditFile(context.Background(), testFile, oldText, newText, false, false, false); err != nil {
		t.Fatalf("EditFile: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "aaa\nbbb"
	if string(got) != want {
		t.Errorf("unexpected trailing newline at EOF.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestBug10_BothEndWithNewlineUnchanged(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	content := "aaa\nbbb\nccc\n"
	oldText := "bbb\n"
	newText := "BBB\n"

	testFile := filepath.Join(tempDir, "bug10_both.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := engine.EditFile(context.Background(), testFile, oldText, newText, false, false, false); err != nil {
		t.Fatalf("EditFile: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	want := "aaa\nBBB\nccc\n"
	if string(got) != want {
		t.Errorf("unexpected extra newline.\n got: %q\nwant: %q", string(got), want)
	}
}

func TestBug10_TolerantWhitespaceBoundary(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// File uses tabs; old_text uses spaces → forces the tolerant matcher path.
	content := "func f() {\n\treturn 1\n}\nnext()\n"
	oldText := "func f() {\n    return 1\n}\n"
	newText := "func f() {\n    return 2\n}"

	testFile := filepath.Join(tempDir, "bug10_tolerant.go")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := engine.EditFile(context.Background(), testFile, oldText, newText, false, false, true); err != nil {
		t.Fatalf("EditFile: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	s := string(got)
	if strings.Contains(s, "}next()") {
		t.Errorf("tolerant path fused next line: %q", s)
	}
	if !strings.Contains(s, "}\nnext()\n") {
		t.Errorf("tolerant path lost boundary newline: %q", s)
	}
}

func TestBug10_ScannerPathPreservesTrailingNewline(t *testing.T) {
	engine, tempDir := setupBug16Engine(t)

	// old_text matches a single line with different indentation (TrimSpace
	// variant) so the bufio.Scanner path (OPTIMIZATION 4) handles it. The
	// file ends with "\n" — previously the reassembly dropped it.
	content := "alpha\n    target\nomega\n"
	oldText := "target"
	newText := "replaced"

	testFile := filepath.Join(tempDir, "bug10_scanner.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := engine.EditFile(context.Background(), testFile, oldText, newText, false, false, false); err != nil {
		t.Fatalf("EditFile: %v", err)
	}

	got, _ := os.ReadFile(testFile)
	if !strings.HasSuffix(string(got), "\n") {
		t.Errorf("scanner path dropped trailing newline: %q", string(got))
	}
	want := "alpha\n    replaced\nomega\n"
	if string(got) != want {
		t.Errorf("scanner path mismatch.\n got: %q\nwant: %q", string(got), want)
	}
}
