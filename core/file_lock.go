package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type ctxHeldPathsKey struct{}
type ctxExpectedHashKey struct{}
type ctxOCCConflictDiffKey struct{}
type ctxSessionIDKey struct{}

func withHeldPath(ctx context.Context, canon string) context.Context {
	prev, _ := ctx.Value(ctxHeldPathsKey{}).(map[string]int)
	next := make(map[string]int, len(prev)+1)
	for k, v := range prev {
		next[k] = v
	}
	next[canon]++
	return context.WithValue(ctx, ctxHeldPathsKey{}, next)
}

func holdsPath(ctx context.Context, canon string) bool {
	prev, _ := ctx.Value(ctxHeldPathsKey{}).(map[string]int)
	return prev[canon] > 0
}

// WithExpectedHash stashes an OCC token so BeginFileTxn can re-check it
// after acquiring the destination lock.
func WithExpectedHash(ctx context.Context, hash string) context.Context {
	if hash == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxExpectedHashKey{}, hash)
}

func expectedHashFrom(ctx context.Context) string {
	h, _ := ctx.Value(ctxExpectedHashKey{}).(string)
	return h
}

// WithOCCConflictDiff requests a bounded diff if an OCC mismatch occurs.
func WithOCCConflictDiff(ctx context.Context, include bool) context.Context {
	if !include {
		return ctx
	}
	return context.WithValue(ctx, ctxOCCConflictDiffKey{}, true)
}

func occConflictDiffRequested(ctx context.Context) bool {
	include, _ := ctx.Value(ctxOCCConflictDiffKey{}).(bool)
	return include
}

// WithSessionID binds auto-OCC state to an effective session.
func WithSessionID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxSessionIDKey{}, id)
}

func sessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxSessionIDKey{}).(string)
	return id
}

type refLock struct {
	ch   chan struct{}
	refs int
}

// pathLockManager serializes mutations per canonical path (in-process) and
// coordinates participating server processes via a lock file.
type pathLockManager struct {
	mu    sync.Mutex
	locks map[string]*refLock
}

func newPathLockManager() *pathLockManager {
	return &pathLockManager{locks: make(map[string]*refLock)}
}

// Acquire locks canon. Cancelable. Nested acquires for a path already in ctx
// are no-ops (unlock is a no-op too).
func (m *pathLockManager) Acquire(ctx context.Context, canon string) (func(), error) {
	if canon == "" {
		return func() {}, nil
	}
	if holdsPath(ctx, canon) {
		return func() {}, nil
	}
	if err := m.acquireInProc(ctx, canon); err != nil {
		return nil, err
	}
	ipc, ipcErr := acquireIPCLock(ctx, canon)
	if ipcErr != nil {
		m.releaseInProc(canon)
		return nil, ipcErr
	}
	return func() {
		if ipc != nil {
			ipc()
		}
		m.releaseInProc(canon)
	}, nil
}

// AcquireAll locks multiple paths in sorted order to avoid deadlock.
func (m *pathLockManager) AcquireAll(ctx context.Context, paths []string) (func(), error) {
	canon := uniqueSorted(paths)
	var unlocks []func()
	for _, p := range canon {
		u, err := m.Acquire(ctx, p)
		if err != nil {
			for i := len(unlocks) - 1; i >= 0; i-- {
				unlocks[i]()
			}
			return nil, err
		}
		unlocks = append(unlocks, u)
		ctx = withHeldPath(ctx, p)
	}
	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}, nil
}

func uniqueSorted(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		c := CanonicalPath(p)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (m *pathLockManager) acquireInProc(ctx context.Context, canon string) error {
	m.mu.Lock()
	rl, ok := m.locks[canon]
	if !ok {
		rl = &refLock{ch: make(chan struct{}, 1)}
		m.locks[canon] = rl
	}
	rl.refs++
	ch := rl.ch
	m.mu.Unlock()

	select {
	case ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		m.releaseInProcRef(canon)
		return ctx.Err()
	}
}

func (m *pathLockManager) releaseInProc(canon string) {
	m.mu.Lock()
	rl := m.locks[canon]
	if rl == nil {
		m.mu.Unlock()
		return
	}
	select {
	case <-rl.ch:
	default:
	}
	rl.refs--
	if rl.refs <= 0 {
		delete(m.locks, canon)
	}
	m.mu.Unlock()
}

func (m *pathLockManager) releaseInProcRef(canon string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rl := m.locks[canon]
	if rl == nil {
		return
	}
	rl.refs--
	if rl.refs <= 0 {
		delete(m.locks, canon)
	}
}

func acquireIPCLock(ctx context.Context, canon string) (func(), error) {
	dir := filepath.Join(os.TempDir(), "mcp-fsu-locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return func() {}, nil
	}
	sum := sha256.Sum256([]byte(canon))
	lockPath := filepath.Join(dir, hex.EncodeToString(sum[:16])+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return func() {}, nil
	}
	for {
		if err := tryLockFile(f); err == nil {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		} else if !isLockBusy(err) {
			// Permanent error (bad handle, ACL, …): fail-open like OpenFile,
			// never spin. A busy lock is the only case that retries.
			_ = f.Close()
			return func() {}, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(15 * time.Millisecond):
		}
	}
}
