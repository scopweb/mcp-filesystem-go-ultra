package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	mu      sync.Mutex
	locks   map[string]*refLock
	timeout time.Duration // 0 = wait until ctx is cancelled
}

func newPathLockManager() *pathLockManager {
	return newPathLockManagerTimeout(0)
}

func newPathLockManagerTimeout(timeout time.Duration) *pathLockManager {
	return &pathLockManager{locks: make(map[string]*refLock), timeout: timeout}
}

// Acquire locks canon. Cancelable. Nested acquires for a path already in ctx
// are no-ops (unlock is a no-op too). With a non-zero manager timeout the wait
// is bounded and a busy lock surfaces as *LockBusyError instead of hanging
// until the MCP client drops the call.
func (m *pathLockManager) Acquire(ctx context.Context, canon string) (func(), error) {
	if canon == "" {
		return func() {}, nil
	}
	if holdsPath(ctx, canon) {
		return func() {}, nil
	}
	start := time.Now()
	waitCtx := ctx
	var cancel context.CancelFunc = func() {}
	if m.timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, m.timeout)
	}
	defer cancel()

	if err := m.acquireInProc(waitCtx, canon); err != nil {
		return nil, m.busyOrCtxErr(ctx, waitCtx, err, canon, 0, start)
	}
	ipc, ipcErr := acquireIPCLock(waitCtx, canon)
	if ipcErr != nil {
		m.releaseInProc(canon)
		var busy *LockBusyError
		if errors.As(ipcErr, &busy) {
			busy.Waited = time.Since(start)
			return nil, m.busyOrCtxErr(ctx, waitCtx, ipcErr, canon, busy.HeldBy, start)
		}
		return nil, m.busyOrCtxErr(ctx, waitCtx, ipcErr, canon, 0, start)
	}
	return func() {
		if ipc != nil {
			ipc()
		}
		m.releaseInProc(canon)
	}, nil
}

// busyOrCtxErr translates a wait failure: if the caller's own ctx is done the
// original error stands (cancellation is theirs); if only the manager timeout
// expired, report LockBusyError.
func (m *pathLockManager) busyOrCtxErr(ctx, waitCtx context.Context, err error, canon string, heldBy int, start time.Time) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if m.timeout > 0 && errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
		return &LockBusyError{Path: canon, HeldBy: heldBy, Waited: time.Since(start)}
	}
	return err
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
			writeLockOwner(f)
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
			heldBy := readLockOwner(lockPath)
			_ = f.Close()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, &LockBusyError{Path: canon, HeldBy: heldBy}
			}
			return nil, ctx.Err()
		case <-time.After(15 * time.Millisecond):
		}
	}
}

// lockOwnerOffset keeps the pid clear of byte 0, the only byte LockFileEx
// guards on Windows: a waiter reading a locked byte gets ERROR_LOCK_VIOLATION,
// an unlocked region reads fine with ReadAt.
const lockOwnerOffset = 16

// writeLockOwner records our pid in the lock file so a waiter that times out
// can name who holds it. Best effort: the byte-range lock is the mechanism,
// the pid is diagnostics.
func writeLockOwner(f *os.File) {
	buf := make([]byte, lockOwnerOffset+16)
	copy(buf[lockOwnerOffset:], strconv.Itoa(os.Getpid())+"\n")
	_, _ = f.WriteAt(buf, 0)
}

// readLockOwner returns the pid stored by writeLockOwner, 0 if unreadable.
func readLockOwner(lockPath string) int {
	f, err := os.Open(lockPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	buf := make([]byte, 16)
	n, err := f.ReadAt(buf, lockOwnerOffset)
	if n == 0 && err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimRight(string(buf[:n]), "\x00")))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
