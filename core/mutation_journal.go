package core

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// mutationJournal is an in-memory undo log, not a crash-durable transaction.
// Entries contain the exact version replaced and the version written by us.
// Recovery compares the latter under the same path locks used by FileTxn.
type mutationJournal struct {
	mu       sync.Mutex
	entries  []mutationEntry
	status   string
	failures []string
}
type mutationEntry struct{ before, after FileSnapshot }
type journalKey struct{}

func journalFrom(ctx context.Context) *mutationJournal {
	j, _ := ctx.Value(journalKey{}).(*mutationJournal)
	return j
}
func (j *mutationJournal) record(before, after FileSnapshot) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, mutationEntry{before, after})
}

func recoverySnapshot(path string) (FileSnapshot, error) {
	canon := CanonicalPath(path)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return FileSnapshot{Path: path, Canon: canon}, nil
	}
	if err != nil {
		return FileSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return FileSnapshot{}, fmt.Errorf("transactional recovery requires a regular file: %s", path)
	}
	return readSnapshot(path, canon)
}

// rollback never runs mutating hooks: recovery must restore the captured bytes.
// Cancellation of the request does not cancel recovery of already applied work.
func (j *mutationJournal) rollback(ctx context.Context, e *UltraFastEngine) (string, []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status != "" {
		return j.status, j.failures
	}
	if len(j.entries) == 0 {
		j.status = "complete"
		return j.status, j.failures
	}
	paths := make([]string, 0, len(j.entries))
	for _, entry := range j.entries {
		paths = append(paths, entry.before.Path)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	locks := newPathLockManager()
	if e != nil {
		locks = e.pathLocks
	}
	unlock, err := locks.AcquireAll(ctx, paths)
	if err != nil {
		return "failed", []string{err.Error()}
	}
	defer unlock()
	var failures []string
	restored := 0
	blocked := make(map[string]bool)
	for i := len(j.entries) - 1; i >= 0; i-- {
		entry := j.entries[i]
		path := entry.before.Path
		if blocked[path] {
			continue
		}
		current, err := recoverySnapshot(path)
		if err == nil && (current.Exists != entry.after.Exists || current.Hash != entry.after.Hash) {
			err = fmt.Errorf("rollback conflict: %s changed after this operation", path)
		}
		if err == nil {
			if entry.before.Exists {
				err = atomicWriteFile(path, entry.before.Bytes, entry.before.Mode)
			} else if current.Exists {
				err = os.Remove(path)
			}
		}
		if err != nil {
			blocked[path] = true
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		if e != nil {
			e.invalidateMutatedPath(path)
		}
		if entry.before.Exists {
			if e != nil {
				e.RecordWriteHash(path, entry.before.Hash)
			} else {
				RecordWriteHash(path, entry.before.Hash)
			}
		} else {
			InvalidateKnownHash(path)
		}
		restored++
	}
	j.failures = failures
	j.status = "failed"
	if len(failures) == 0 {
		j.status = "complete"
	} else if restored > 0 {
		j.status = "partial"
	}
	return j.status, j.failures
}

// beginRecoverableAction coordinates non-content mutations. A pipeline journal
// rejects directory/symlink actions before they run, because byte snapshots
// cannot safely recover a tree. finish must run before releasing coordination.
func (e *UltraFastEngine) beginRecoverableAction(ctx context.Context, paths []string) (context.Context, func(), error) {
	ctx, unlock, err := e.coordinateFiles(ctx, paths)
	if err != nil {
		return ctx, nil, err
	}
	j := journalFrom(ctx)
	if j == nil {
		return ctx, unlock, nil
	}
	before := []FileSnapshot{}
	for _, path := range uniqueSorted(paths) {
		snap, err := recoverySnapshot(path)
		if err != nil {
			unlock()
			return ctx, nil, err
		}
		before = append(before, snap)
	}
	return ctx, func() {
		defer unlock()
		for _, snap := range before {
			after, err := recoverySnapshot(snap.Path)
			if err != nil {
				// An unreadable result cannot be restored safely; retain a conflict entry.
				after = FileSnapshot{Path: snap.Path, Exists: true, Hash: "unknown"}
			}
			if snap.Exists != after.Exists || snap.Hash != after.Hash {
				j.record(snap, after)
			}
		}
	}, nil
}

// coordinateFiles authorizes and acquires the complete known path set in stable
// order. Callers pass the returned context to nested FileTxn operations.
func (e *UltraFastEngine) coordinateFiles(ctx context.Context, paths []string) (context.Context, func(), error) {
	for _, path := range paths {
		if !e.IsPathAllowed(path) {
			return ctx, nil, e.AccessDeniedError("mutate", path)
		}
	}
	unlock, err := e.pathLocks.AcquireAll(ctx, paths)
	if err != nil {
		return ctx, nil, err
	}
	for _, path := range uniqueSorted(paths) {
		ctx = withHeldPath(ctx, path)
	}
	return ctx, unlock, nil
}
