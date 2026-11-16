//go:build !unix && !windows

package core

import (
	"fmt"
	"os"
)

func renameAt(oldDir, newDir *os.File, oldName, newName string) error {
	return fmt.Errorf("anchored rename is not supported on this platform")
}
