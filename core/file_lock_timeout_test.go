package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Two managers = two server processes: separate in-process maps, shared IPC
// lock file under os.TempDir()/mcp-fsu-locks.
func TestPathLock_TimeoutReturnsLockBusyWithOwnerPID(t *testing.T) {
	canon := CanonicalPath(filepath.Join(t.TempDir(), "shared.txt"))
	holder := newPathLockManagerTimeout(0)
	waiter := newPathLockManagerTimeout(150 * time.Millisecond)

	unlock, err := holder.Acquire(context.Background(), canon)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}
	defer unlock()

	start := time.Now()
	_, err = waiter.Acquire(context.Background(), canon)
	waited := time.Since(start)
	var busy *LockBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("want *LockBusyError, got %T: %v", err, err)
	}
	if busy.Path != canon {
		t.Errorf("path: got %q want %q", busy.Path, canon)
	}
	if busy.HeldBy != os.Getpid() {
		t.Errorf("held_by: got %d want own pid %d (same process plays both roles)", busy.HeldBy, os.Getpid())
	}
	if waited < 150*time.Millisecond || waited > 2*time.Second {
		t.Errorf("waited %v, want ~150ms", waited)
	}
	if busy.Waited <= 0 {
		t.Errorf("Waited not populated: %v", busy.Waited)
	}
}

func TestPathLock_ZeroTimeoutHonorsCallerCancel(t *testing.T) {
	canon := CanonicalPath(filepath.Join(t.TempDir(), "shared.txt"))
	holder := newPathLockManagerTimeout(0)
	waiter := newPathLockManagerTimeout(0)

	unlock, err := holder.Acquire(context.Background(), canon)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = waiter.Acquire(ctx, canon)
	var busy *LockBusyError
	if errors.As(err, &busy) {
		t.Fatalf("zero timeout must not synthesize LockBusyError, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want caller's DeadlineExceeded, got %v", err)
	}
}

func TestPathLock_AcquiresAfterHolderReleases(t *testing.T) {
	canon := CanonicalPath(filepath.Join(t.TempDir(), "shared.txt"))
	holder := newPathLockManagerTimeout(0)
	waiter := newPathLockManagerTimeout(2 * time.Second)

	unlock, err := holder.Acquire(context.Background(), canon)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		unlock()
	}()
	u2, err := waiter.Acquire(context.Background(), canon)
	if err != nil {
		t.Fatalf("waiter should acquire once holder releases: %v", err)
	}
	u2()
}
