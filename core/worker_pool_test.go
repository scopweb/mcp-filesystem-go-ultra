package core

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolCapsConcurrency(t *testing.T) {
	p, err := newWorkerPool(2)
	if err != nil {
		t.Fatal(err)
	}
	var current, maxSeen, entered atomic.Int32
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(2)
	var submitters sync.WaitGroup
	for i := 0; i < 6; i++ {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			if err := p.Submit(func() {
				n := current.Add(1)
				for {
					old := maxSeen.Load()
					if n <= old || maxSeen.CompareAndSwap(old, n) {
						break
					}
				}
				if entered.Add(1) <= 2 {
					started.Done()
				}
				<-release
				current.Add(-1)
			}); err != nil {
				t.Errorf("submit: %v", err)
			}
		}()
	}
	started.Wait()
	time.Sleep(20 * time.Millisecond)
	if got := maxSeen.Load(); got > 2 {
		t.Fatalf("concurrency %d, want <= 2", got)
	}
	close(release)
	submitters.Wait()
	p.Release()
	if err := p.Submit(func() {}); err == nil {
		t.Fatal("submit after Release must fail")
	}
}
