//go:build !windows

package core

import "testing"

func TestOsRoot_WindowsShortName(t *testing.T) {
	t.Skip("8.3 short names are Windows-only")
}
