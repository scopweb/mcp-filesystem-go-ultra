package core

import (
	"strings"
	"testing"
	"time"
)

func TestCompileRegex_ReDoSStaysBounded(t *testing.T) {
	e, cleanup := setupTestEngine(t)
	defer cleanup()

	re, err := e.CompileRegex(`(a+)+b`)
	if err != nil {
		t.Fatal(err)
	}
	in := strings.Repeat("a", 1000)
	start := time.Now()
	if re.MatchString(in) {
		t.Fatal("input has no trailing b; match must fail")
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("RE2 match took %s; a backtracking engine would hang", d)
	}
	if _, err := e.CompileRegex(`(a)\1`); err == nil {
		t.Fatal("backreference must be rejected; Go regexp is RE2")
	}
}
