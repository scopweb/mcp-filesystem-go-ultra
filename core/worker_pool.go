package core

import (
	"fmt"
	"sync"
)

type workerPool struct {
	sem    chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

func newWorkerPool(size int) (*workerPool, error) {
	if size < 1 {
		return nil, fmt.Errorf("worker pool size must be at least 1")
	}
	return &workerPool{sem: make(chan struct{}, size)}, nil
}

// Submit blocks until a slot is free. It returns an error only after Release.
func (p *workerPool) Submit(task func()) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("worker pool closed")
	}
	p.wg.Add(1)
	p.mu.Unlock()

	p.sem <- struct{}{}
	go func() {
		defer p.wg.Done()
		defer func() { <-p.sem }()
		task()
	}()
	return nil
}

// Release rejects new tasks and waits for tasks already accepted.
func (p *workerPool) Release() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.wg.Wait()
}
