//go:build windows

package core

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileRenameInformation matches the NT FILE_RENAME_INFORMATION layout used by
// os.Root. RootDirectory is the destination directory handle, so the rename
// does not resolve either side from a path string.
type fileRenameInformation struct {
	ReplaceIfExists byte
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [syscall.MAX_PATH]uint16
}

func renameAt(oldDir, newDir *os.File, oldName, newName string) error {
	oa, err := ntObjectAttrs(windows.Handle(oldDir.Fd()), oldName)
	if err != nil {
		return err
	}
	var h windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(
		&h,
		windows.SYNCHRONIZE|windows.DELETE,
		oa,
		&iosb,
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	p16, err := syscall.UTF16FromString(newName)
	if err != nil {
		return err
	}
	info := fileRenameInformation{RootDirectory: windows.Handle(newDir.Fd())}
	if len(p16) == 0 || len(p16) > len(info.FileName) {
		return syscall.ENAMETOOLONG
	}
	copy(info.FileName[:], p16)
	info.FileNameLength = uint32((len(p16) - 1) * 2)
	const fileRenameInformationClass = 10
	return windows.NtSetInformationFile(
		h,
		&iosb,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		fileRenameInformationClass,
	)
}

func ntObjectAttrs(root windows.Handle, name string) (*windows.OBJECT_ATTRIBUTES, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		RootDirectory: root,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	oa.Length = uint32(unsafe.Sizeof(*oa))
	return oa, nil
}
