package cache

import (
	"sync"
	"time"
)

type ttlItem struct {
	value   any
	expires time.Time
}

// ttlMap is a TTL map with one sweeper. A non-positive ttl on Set uses defTTL.
// Expired keys are dropped on Get and by the sweeper. onEvict runs once per drop.
type ttlMap struct {
	mu      sync.Mutex
	items   map[string]ttlItem
	defTTL  time.Duration
	onEvict func()
	stop    chan struct{}
	stopped bool
}

func newTTLMap(defTTL, sweepEvery time.Duration, onEvict func()) *ttlMap {
	m := &ttlMap{
		items:   make(map[string]ttlItem),
		defTTL:  defTTL,
		onEvict: onEvict,
		stop:    make(chan struct{}),
	}
	if sweepEvery > 0 {
		go m.sweep(sweepEvery)
	}
	return m
}

func (m *ttlMap) Set(key string, value any, ttl time.Duration) {
	if ttl <= 0 {
		ttl = m.defTTL
	}
	m.mu.Lock()
	m.items[key] = ttlItem{value: value, expires: time.Now().Add(ttl)}
	m.mu.Unlock()
}

func (m *ttlMap) Get(key string) (any, bool) {
	m.mu.Lock()
	it, ok := m.items[key]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	if !it.expires.After(time.Now()) {
		delete(m.items, key)
		m.mu.Unlock()
		m.evict(1)
		return nil, false
	}
	m.mu.Unlock()
	return it.value, true
}

func (m *ttlMap) Delete(key string) {
	m.mu.Lock()
	_, ok := m.items[key]
	if ok {
		delete(m.items, key)
	}
	m.mu.Unlock()
	if ok {
		m.evict(1)
	}
}

func (m *ttlMap) Flush() {
	m.mu.Lock()
	n := len(m.items)
	m.items = make(map[string]ttlItem)
	m.mu.Unlock()
	m.evict(n)
}

func (m *ttlMap) stopSweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	close(m.stop)
}

func (m *ttlMap) sweep(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.dropExpired()
		}
	}
}

func (m *ttlMap) dropExpired() {
	now := time.Now()
	var n int
	m.mu.Lock()
	for k, it := range m.items {
		if !it.expires.After(now) {
			delete(m.items, k)
			n++
		}
	}
	m.mu.Unlock()
	m.evict(n)
}

func (m *ttlMap) evict(n int) {
	if n == 0 || m.onEvict == nil {
		return
	}
	for i := 0; i < n; i++ {
		m.onEvict()
	}
}
