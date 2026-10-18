package core

import "testing"

func TestSearchPageSize(t *testing.T) {
	if got := SearchPageSize(1000, 0); got != DefaultSearchPage {
		t.Fatalf("omitted page=%d", got)
	}
	if got := SearchPageSize(1000, 200); got != 200 {
		t.Fatalf("explicit 200=%d", got)
	}
	if got := SearchPageSize(1000, 5000); got != 1000 {
		t.Fatalf("above ceiling=%d", got)
	}
	if got := SearchPageSize(20, 0); got != 20 {
		t.Fatalf("ceiling below default page=%d", got)
	}
	if got := SearchPageSize(0, 0); got != DefaultSearchPage {
		t.Fatalf("zero ceiling omitted=%d", got)
	}
}
