package core

import (
	"context"
	"fmt"
	"os"
	"time"
)

// ExecuteBatchContext retains coordination through recovery. Atomic means
// recoverable on returned errors; it does not imply durability across crashes.
func (m *BatchOperationManager) executeBatchContext(ctx context.Context, request BatchRequest) (result BatchResult) {
	start := time.Now()
	result.TotalOps = len(request.Operations)
	result.ValidationOnly = request.ValidateOnly
	defer func() { result.ExecutionTime = time.Since(start).String() }()
	if err := ctx.Err(); err != nil {
		result.Errors = []string{err.Error()}
		return
	}
	if m.engine != nil && m.engine.IsReadOnly() && !request.ValidateOnly {
		result.Errors = []string{"readonly: batch mutation denied"}
		return
	}
	paths := []string{}
	for _, op := range request.Operations {
		paths = append(paths, m.collectPaths(op)...)
	}
	locks := newPathLockManager()
	if m.engine != nil {
		locks = m.engine.pathLocks
	}
	unlock, err := locks.AcquireAll(ctx, paths)
	if err != nil {
		result.Errors = []string{err.Error()}
		return
	}
	defer unlock()
	for _, path := range uniqueSorted(paths) {
		ctx = withHeldPath(ctx, path)
	}
	if errors := m.validateOperations(request.Operations); len(errors) > 0 {
		result.Errors = errors
		return
	}
	if request.ValidateOnly {
		result.Success = true
		return
	}
	// Capture before any mutation. Unrecoverable file types are rejected rather
	// than silently promising atomic directory/tree recovery.
	journal := &mutationJournal{}
	if request.Atomic {
		for _, path := range uniqueSorted(paths) {
			if _, err := recoverySnapshot(path); err != nil {
				result.Errors = []string{err.Error()}
				return
			}
		}
		for _, op := range request.Operations {
			if op.Type == "create_dir" {
				result.Errors = []string{"atomic create_dir is unsupported; use a separate non-atomic batch"}
				return
			}
		}
	}
	if request.CreateBackup {
		result.BackupPath, err = m.createBackup(request.Operations)
		if err != nil {
			result.Errors = []string{fmt.Sprintf("backup failed: %v", err)}
			return
		}
	}
	for i, op := range request.Operations {
		r := OperationResult{Index: i, Type: op.Type, Path: op.Path}
		before := []FileSnapshot{}
		if request.Atomic {
			for _, path := range uniqueSorted(m.collectPaths(op)) {
				snap, snapErr := recoverySnapshot(path)
				if snapErr != nil {
					err = snapErr
					break
				}
				before = append(before, snap)
			}
		}
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			err = m.executeOperationContext(ctx, op, &r)
		}
		// Include partial changes of the failing operation (not only successful ops).
		for _, snap := range before {
			after, readErr := recoverySnapshot(snap.Path)
			if readErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("cannot inspect partial write %s: %v", snap.Path, readErr))
				if err == nil {
					err = readErr
				}
				continue
			}
			if snap.Exists != after.Exists || snap.Hash != after.Hash {
				journal.record(snap, after)
			}
		}
		r.Success = err == nil
		if err != nil {
			r.Error = err.Error()
			result.FailedOps++
		} else {
			result.CompletedOps++
		}
		result.Results = append(result.Results, r)
		if err != nil && (request.Atomic || ctx.Err() != nil) {
			result.Errors = append(result.Errors, fmt.Sprintf("operation %d failed: %v", i, err))
			if request.Atomic {
				result.RollbackStatus, result.RollbackErrors = journal.rollback(ctx, m.engine)
				result.RollbackDone = result.RollbackStatus == "complete"
			}
			m.refreshKnownHashes(request.Operations)
			return
		}
		err = nil
	}
	result.Success = result.FailedOps == 0
	m.refreshKnownHashes(request.Operations)
	return
}

func (m *BatchOperationManager) commitBytes(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.engine != nil {
		txCtx, txn, err := m.engine.BeginFileTxn(ctx, path, true)
		if err != nil {
			return err
		}
		defer txn.Release()
		_, err = txn.Commit(txCtx, content, false, "batch", "batch write")
		return err
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode()
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWriteFile(path, content, mode)
}
