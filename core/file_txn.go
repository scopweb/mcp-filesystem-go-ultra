package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileSnapshot is one coherent view of a file: bytes and hash from the same read.
type FileSnapshot struct {
	Path   string
	Canon  string
	Bytes  []byte
	Hash   string
	Mode   os.FileMode
	Exists bool
}

// FileTxn is the common mutation pipeline: lock, snapshot, OCC, backup, write.
type FileTxn struct {
	engine     *UltraFastEngine
	snap       FileSnapshot
	unlock     func()
	done       bool
	skipBackup bool
}

func (t *FileTxn) SkipBackup() {
	if t != nil {
		t.skipBackup = true
	}
}

func (t *FileTxn) Snapshot() FileSnapshot { return t.snap }

func readSnapshot(path, canon string) (FileSnapshot, error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return FileSnapshot{}, ClassifyReadError("snapshot", path, statErr)
	}
	if info.IsDir() {
		return FileSnapshot{}, fmt.Errorf("cannot mutate directory: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return FileSnapshot{}, ClassifyReadError("snapshot", path, err)
	}
	mode := info.Mode()
	return FileSnapshot{
		Path:   path,
		Canon:  canon,
		Bytes:  raw,
		Hash:   contentHashFNV(string(raw)),
		Mode:   mode,
		Exists: true,
	}, nil
}

// BeginFileTxn resolves, locks, snapshots, and re-checks expected_hash from ctx.
// allowMissing is for create/write of new files.
func (e *UltraFastEngine) BeginFileTxn(ctx context.Context, path string, allowMissing bool) (context.Context, *FileTxn, error) {
	path = NormalizePath(path)
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if !e.IsPathAllowed(path) {
		return ctx, nil, e.AccessDeniedError("mutate", path)
	}
	if resolved, err := e.ResolveAndAuthorize("mutate", path); err == nil {
		path = resolved
	}
	canon := CanonicalPath(path)

	if e.pathLocks == nil {
		e.pathLocks = newPathLockManager()
	}
	unlock, err := e.pathLocks.Acquire(ctx, canon)
	if err != nil {
		return ctx, nil, err
	}
	ctx = withHeldPath(ctx, canon)

	txn := &FileTxn{engine: e, unlock: unlock}
	snap, err := readSnapshot(path, canon)
	if err != nil {
		if allowMissing && isNotExistPathError(err) {
			txn.snap = FileSnapshot{Path: path, Canon: canon, Mode: 0644, Exists: false, Hash: contentHashFNV("")}
			return ctx, txn, nil
		}
		unlock()
		return ctx, nil, err
	}
	if eh := expectedHashFrom(ctx); eh != "" && snap.Hash != eh {
		baseline := e.FindOCCBaseline(path, eh)
		conflict := BuildOCCConflict(eh, snap.Bytes, baseline)
		conflict.Path = path
		if occConflictDiffRequested(ctx) {
			IncludeOCCConflictDiff(&conflict, snap.Bytes, baseline, filepath.Base(path))
		}
		unlock()
		return ctx, nil, &OCCMismatchError{Expected: eh, Actual: snap.Hash, Conflict: conflict}
	}
	txn.snap = snap
	return ctx, txn, nil
}

// FindOCCBaseline returns only a backup whose bytes match expectedHash. The
// feedback session state stores hashes, not file bodies, and must not become an
// agent-facing baseline API. It is safe to use while holding a file transaction lock.
func (e *UltraFastEngine) FindOCCBaseline(path, expectedHash string) []byte {
	if e == nil || e.backupManager == nil || expectedHash == "" {
		return nil
	}
	backups, err := e.backupManager.ListBackups(0, "all", path, 0)
	if err != nil {
		return nil
	}
	cleanPath := filepath.Clean(path)
	for _, backup := range backups {
		for _, file := range backup.Files {
			if filepath.Clean(file.OriginalPath) != cleanPath {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(e.backupManager.GetBackupPath(backup.BackupID), file.BackupPath))
			if err == nil && contentHashFNV(string(raw)) == expectedHash {
				return append([]byte{}, raw...)
			}
		}
	}
	return nil
}

func isNotExistPathError(err error) bool {
	return err != nil && (os.IsNotExist(err) || errors.Is(err, os.ErrNotExist))
}

// Commit backups the snapshot (the version being replaced) and writes newBytes.
// dryRun skips backup, write, cache and OCC updates.
func (t *FileTxn) Commit(ctx context.Context, newBytes []byte, dryRun bool, backupOp, backupNote string) (backupID string, err error) {
	if t == nil {
		return "", fmt.Errorf("nil file transaction")
	}
	if dryRun {
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := t.snap.Path
	current, readErr := recoverySnapshot(path)
	if readErr != nil {
		return "", readErr
	}
	if current.Exists != t.snap.Exists || (current.Exists && current.Hash != t.snap.Hash) {
		return "", fmt.Errorf("write conflict: %s changed since snapshot", path)
	}
	if t.snap.Exists && !t.skipBackup && t.engine.backupManager != nil {
		t.engine.backupChainMu.RLock()
		prev := t.engine.backupChain[path]
		t.engine.backupChainMu.RUnlock()
		backupID, err = t.engine.backupManager.CreateBackupFromBytes(path, t.snap.Bytes, t.snap.Mode, backupOp, backupNote, prev)
		if err != nil {
			return "", fmt.Errorf("could not create backup: %w", err)
		}
		t.engine.backupChainMu.Lock()
		t.engine.backupChain[path] = backupID
		t.engine.backupChainMu.Unlock()
	}
	mode := t.snap.Mode
	if mode == 0 {
		mode = 0644
	}
	if err := atomicWriteFile(path, newBytes, mode); err != nil {
		return backupID, fmt.Errorf("error writing file: %w", err)
	}
	after := FileSnapshot{Path: path, Canon: t.snap.Canon, Exists: true, Hash: contentHashFNV(string(newBytes)), Mode: mode}
	journalFrom(ctx).record(t.snap, after)
	t.snap = after
	t.snap.Bytes = append([]byte(nil), newBytes...)
	t.engine.invalidateMutatedPath(path)
	t.engine.RecordWriteHash(path, contentHashFNV(string(newBytes)))
	return backupID, nil
}

// Release drops coordination. Safe to call twice.
func (t *FileTxn) Release() {
	if t == nil || t.done {
		return
	}
	t.done = true
	if t.unlock != nil {
		t.unlock()
		t.unlock = nil
	}
}

// ReadSnapshot returns content and hash from one disk (or cache) read.
func (e *UltraFastEngine) ReadSnapshot(ctx context.Context, path string) (FileSnapshot, error) {
	path = NormalizePath(path)
	if err := ctx.Err(); err != nil {
		return FileSnapshot{}, err
	}
	if !e.IsPathAllowed(path) {
		return FileSnapshot{}, e.AccessDeniedError("read", path)
	}
	canon := CanonicalPath(path)
	if cached, hit := e.cache.GetFileFresh(path); hit {
		raw := cached
		mode := os.FileMode(0644)
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode()
		}
		return FileSnapshot{
			Path:   path,
			Canon:  canon,
			Bytes:  raw,
			Hash:   contentHashFNV(string(raw)),
			Mode:   mode,
			Exists: true,
		}, nil
	}
	raw, err := e.readFileBytesDeduped(ctx, path)
	if err != nil {
		return FileSnapshot{}, err
	}
	mode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode()
	}
	return FileSnapshot{
		Path:   path,
		Canon:  canon,
		Bytes:  raw,
		Hash:   contentHashFNV(string(raw)),
		Mode:   mode,
		Exists: true,
	}, nil
}
