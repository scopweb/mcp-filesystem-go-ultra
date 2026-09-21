//go:build unix

package cache

import (
	"os"
	"syscall"
)

func identityFromInfo(info os.FileInfo) (FileIdentity, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return FileIdentity{}, false
	}
	return FileIdentity{vol: uint64(st.Dev), idx: uint64(st.Ino)}, true
}

func identityFromFile(f *os.File) (FileIdentity, bool) {
	info, err := f.Stat()
	if err != nil {
		return FileIdentity{}, false
	}
	return identityFromInfo(info)
}
