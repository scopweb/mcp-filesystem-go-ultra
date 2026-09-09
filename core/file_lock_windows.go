//go:build windows

package core

import (
	"os"

	"golang.org/x/sys/windows"
)

func tryLockFile(f *os.File) error {
	var ol windows.Overlapped
	const exclusive = windows.LOCKFILE_EXCLUSIVE_LOCK
	const failNow = windows.LOCKFILE_FAIL_IMMEDIATELY
	return windows.LockFileEx(windows.Handle(f.Fd()), exclusive|failNow, 0, 1, 0, &ol)
}

func unlockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
