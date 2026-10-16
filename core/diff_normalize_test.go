package core

import "testing"

func TestCompareTexts_NormalizeIDSub(t *testing.T) {
	rules := []NormalizeRule{{Pattern: `idSub=\d+`, Replacement: "idSub=<ignored>"}}
	a := "order idSub=12 total=9\n"
	b := "order idSub=99 total=9\n"
	cmp, err := CompareTexts(a, b, rules)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Identical || !cmp.Equivalent || cmp.MatchesA != 1 || cmp.MatchesB != 1 {
		t.Fatalf("%+v", cmp)
	}
	b = "order idSub=99 total=8\n"
	cmp, err = CompareTexts(a, b, rules)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Equivalent || cmp.Identical || !cmp.NormalizedDiff {
		t.Fatalf("real difference hidden: %+v", cmp)
	}
	if _, err := CompareTexts(a, b, []NormalizeRule{{Pattern: "("}}); err == nil {
		t.Fatal("invalid regex must fail")
	}
}

func TestCompareTexts_LiteralIdenticalIgnoresRules(t *testing.T) {
	cmp, err := CompareTexts("same\n", "same\n", []NormalizeRule{{Pattern: "idSub=1", Replacement: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !cmp.Identical || cmp.Equivalent {
		t.Fatalf("%+v", cmp)
	}
}
