//go:build windows

package core

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type ntfsIdent struct {
	vol   uint32
	idx   uint64
	birth int64
	nlink uint32
}

func ntfsIdentOf(t *testing.T, path string) ntfsIdent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		t.Fatal(err)
	}
	return ntfsIdent{
		vol:   uint32(info.VolumeSerialNumber),
		idx:   uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		birth: info.CreationTime.Nanoseconds(),
		nlink: info.NumberOfLinks,
	}
}

func TestWriteAndEdit_TempRenameDropsNTFSIdentity(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	dir := engine.config.AllowedPaths[0]
	path := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "kept-link.txt")
	if err := os.Link(path, link); err != nil {
		t.Skipf("hardlink unavailable: %v", err)
	}
	before := ntfsIdentOf(t, path)
	if before.nlink < 2 {
		t.Fatalf("nlink=%d, want a hardlink", before.nlink)
	}

	ctx := context.Background()
	if err := engine.WriteFileContent(ctx, path, "new-write"); err != nil {
		t.Fatal(err)
	}
	afterWrite := ntfsIdentOf(t, path)
	linkAfterWrite := ntfsIdentOf(t, link)
	if afterWrite.idx == before.idx {
		t.Fatal("write replaced the file via temp+rename; NTFS file id of the path must change")
	}
	if afterWrite.birth == before.birth {
		t.Fatal("birthtime of the path must be the new file, not the original")
	}
	if linkAfterWrite.idx != before.idx {
		t.Fatalf("hardlink file id changed: before %d link %d", before.idx, linkAfterWrite.idx)
	}
	linkBody, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(linkBody) != "old" {
		t.Fatalf("hardlink followed the rename, got %q", linkBody)
	}

	if _, err := engine.EditFile(ctx, path, "new-write", "new-edit", false, false, false); err != nil {
		t.Fatal(err)
	}
	afterEdit := ntfsIdentOf(t, path)
	if afterEdit.idx == afterWrite.idx {
		t.Fatal("edit replaced the file via temp+rename; NTFS file id of the path must change")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new-edit" {
		t.Fatalf("path content=%q", body)
	}
}
