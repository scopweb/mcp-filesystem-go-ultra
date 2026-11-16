//go:build unix

package core

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameAt(oldDir, newDir *os.File, oldName, newName string) error {
	return unix.Renameat(int(oldDir.Fd()), oldName, int(newDir.Fd()), newName)
}
