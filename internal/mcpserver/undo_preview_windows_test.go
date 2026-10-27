//go:build windows

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestUndoLast_WindowsShortPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "LongDirectoryNameForUndo")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ptr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetShortPathName(ptr, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		t.Skipf("8.3 alias unavailable: length=%d err=%v", n, err)
	}
	shortDir := windows.UTF16ToString(buf[:n])
	if strings.EqualFold(shortDir, dir) {
		t.Skip("volume did not provide a distinct 8.3 alias")
	}
	t.Run("short", func(t *testing.T) {
		testUndoLastPreviewAndSingleStep(t, shortDir)
	})
	t.Run("case", func(t *testing.T) {
		testUndoLastPreviewAndSingleStep(t, strings.ToUpper(dir))
	})
}
