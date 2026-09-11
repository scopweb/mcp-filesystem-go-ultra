package core

import (
	"strings"
	"testing"
)

func TestMatchLineNumbers(t *testing.T) {
	got := MatchLineNumbers("aa\nxx aa\naa", "aa")
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("%v", got)
	}
}

func TestEditPolicy_CheckMatchCount(t *testing.T) {
	n := 2
	p := EditPolicy{ExpectedMatches: &n}
	if err := p.checkMatchCount("x x", "x", 2); err != nil {
		t.Fatal(err)
	}
	err := p.checkMatchCount("x x x", "x", 3)
	if err == nil || !strings.Contains(err.Error(), "expected 2") || !strings.Contains(err.Error(), "lines:") {
		t.Fatalf("%v", err)
	}
	strict := EditPolicy{Strict: true}
	if err := strict.checkMatchCount("only once", "once", 1); err != nil {
		t.Fatal(err)
	}
	if err := strict.checkMatchCount("a a", "a", 2); err == nil {
		t.Fatal("strict must require 1 match")
	}
}
