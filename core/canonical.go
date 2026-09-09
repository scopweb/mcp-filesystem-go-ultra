package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CanonicalPath returns a stable absolute path for locking and OCC.
// Symlinks are resolved when the target exists; missing files keep Abs(Clean).
func CanonicalPath(path string) string {
	path = NormalizePath(path)
	if path == "" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	dir, base := filepath.Dir(abs), filepath.Base(abs)
	if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(resolvedDir, base)
	}
	return abs
}

// CanonicalOCCKey is the session-map key for auto-OCC. Windows keys are
// case-folded so C:\Foo and c:\foo identify the same file.
func CanonicalOCCKey(path string) string {
	canon := CanonicalPath(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(canon)
	}
	return canon
}

// ClassifyReadError distinguishes missing files from permission/lock failures.
func ClassifyReadError(op, path string, err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return &PathError{Op: op, Path: path, Err: fmtNotExist(err)}
	}
	if os.IsPermission(err) {
		return &PathError{Op: op, Path: path, Err: fmtPermission(err)}
	}
	return &PathError{Op: op, Path: path, Err: err}
}

func fmtNotExist(err error) error {
	return wrapMsg("file does not exist", err)
}

func fmtPermission(err error) error {
	return wrapMsg("permission denied", err)
}

type msgError struct {
	msg string
	err error
}

func wrapMsg(msg string, err error) error {
	return &msgError{msg: msg, err: err}
}

func (e *msgError) Error() string {
	if e.err == nil {
		return e.msg
	}
	return e.msg + ": " + e.err.Error()
}

func (e *msgError) Unwrap() error { return e.err }
