package core

import (
	"strings"
	"testing"
)

func TestFormatNumberedContext_UsesRecordedSplit(t *testing.T) {
	m := SearchMatch{
		File:          "nav.json",
		LineNumber:    12,
		Line:          "  \"id\": 1,",
		Context:       []string{"  \"prev\": true,", "  \"next\": 2,", "  \"tail\": 3,"},
		ContextBefore: 1,
		ContextSplit:  true,
	}
	out := formatNumberedContext(m, 3, true)
	prev := strings.Index(out, "prev")
	id := strings.Index(out, "\"id\"")
	next := strings.Index(out, "next")
	if prev < 0 || id < 0 || next < 0 || !(prev < id && id < next) {
		t.Fatalf("disordered context:\n%s", out)
	}
	if strings.Contains(out, "9 |") {
		t.Fatalf("guessed before-count, got:\n%s", out)
	}
}
