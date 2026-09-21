package benchclock

import (
	"testing"
	"time"
)

func TestMonotonicHighResolution(t *testing.T) {
	start := Now()
	prev := start
	subMS := false
	for i := 0; i < 10000; i++ {
		next := Now()
		if next < prev {
			t.Fatal("clock went backwards")
		}
		d := Since(prev)
		if d > 0 && d < time.Millisecond {
			subMS = true
		}
		prev = next
	}
	if !subMS || Since(start) <= 0 {
		t.Fatal("clock lacks observable sub-ms progress")
	}
}
