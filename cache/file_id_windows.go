//go:build windows

package cache

import (
	"os"
	"syscall"
)

func identityFromInfo(os.FileInfo) (FileIdentity, bool) {
	return FileIdentity{}, false
}

func identityFromFile(f *os.File) (FileIdentity, bool) {
	h := syscall.Handle(f.Fd())
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return FileIdentity{}, false
	}
	idx := (uint64(info.FileIndexHigh) << 32) | uint64(info.FileIndexLow)
	return FileIdentity{vol: uint64(info.VolumeSerialNumber), idx: idx}, true
}
