package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// openCheckedDir opens dir as an os.Root only when that handle is still the
// directory just lstat'd. os.OpenRoot follows a symlink swapped in after the
// check; SameFile on the handle refuses that directory. A ModeSymlink is
// refused before the open so the race cannot be won by planting the link
// first. A directory junction is not ModeSymlink; if the handle is not the
// lstat'd entry the open fails closed rather than following it.
func openCheckedDir(dir string) (*os.Root, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("security: refusing to follow symlink directory %s", dir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("security: directory changed during open: %s", dir)
	}
	return root, nil
}

// openCheckedDirFile is openCheckedDir for callers that need the directory
// descriptor (renameat). The returned file is the directory that was checked.
func openCheckedDirFile(dir string) (*os.File, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("security: refusing to follow symlink directory %s", dir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		f.Close()
		return nil, fmt.Errorf("security: directory changed during open: %s", dir)
	}
	return f, nil
}

// openReadAnchored opens the final component of path relative to its
// already-checked parent. The bytes are read from that descriptor. A symlink
// leaf, or a file swapped for a different entry between lstat and open, is
// refused. os.Root on an allowlist base does not cover every read.
func openReadAnchored(path string) (*os.File, error) {
	parent := filepath.Dir(path)
	name := filepath.Base(path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return nil, fmt.Errorf("cannot open a filesystem root as a file: %s", path)
	}
	root, err := openCheckedDir(parent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("security: refusing to read symlink %s", path)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		f.Close()
		return nil, fmt.Errorf("security: %s changed while it was being opened", path)
	}
	return f, nil
}

func readAnchored(path string) ([]byte, error) {
	f, err := openReadAnchored(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// symlinkInCheckedDir reports whether name, relative to the already-open
// directory, is a symlink. The directory handle is re-checked so a swapped
// path cannot make the lstat describe a different directory.
func symlinkInCheckedDir(dir *os.File, name string) (bool, error) {
	root, err := os.OpenRoot(dir.Name())
	if err != nil {
		return false, err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil {
		return false, err
	}
	held, err := dir.Stat()
	if err != nil {
		return false, err
	}
	if !os.SameFile(held, opened) {
		return false, fmt.Errorf("security: directory changed during open: %s", dir.Name())
	}
	info, err := root.Lstat(name)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// renameAnchored renames src to dst with both parents pinned to the
// directories just checked. The source is not passed to the kernel as a path
// string: a parent swapped for an outside symlink between validation and
// rename cannot import that outside file (GHSA-7m4x-rv66-mxhh).
func renameAnchored(src, dst string) error {
	srcName := filepath.Base(src)
	dstName := filepath.Base(dst)
	if srcName == "" || srcName == "." || dstName == "" || dstName == "." {
		return fmt.Errorf("cannot rename a filesystem root")
	}
	srcDir, err := openCheckedDirFile(filepath.Dir(src))
	if err != nil {
		return err
	}
	defer srcDir.Close()
	dstDir, err := openCheckedDirFile(filepath.Dir(dst))
	if err != nil {
		return err
	}
	defer dstDir.Close()
	link, err := symlinkInCheckedDir(srcDir, srcName)
	if err != nil {
		return err
	}
	if link {
		return fmt.Errorf("security: refusing to move symlink %s", src)
	}
	return renameAt(srcDir, dstDir, srcName, dstName)
}
