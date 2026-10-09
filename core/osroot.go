package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// os.Root covers file I/O inside an allowed base. It does not sandbox Git,
// hooks, or other child processes, and it does not replace file or secret policy.

func (e *UltraFastEngine) closeRoots() {
	e.rootMu.Lock()
	defer e.rootMu.Unlock()
	for key, root := range e.rootHandles {
		_ = root.Close()
		delete(e.rootHandles, key)
	}
}

func (e *UltraFastEngine) rootFor(base string) (*os.Root, error) {
	e.rootMu.Lock()
	defer e.rootMu.Unlock()
	if e.rootHandles == nil {
		e.rootHandles = map[string]*os.Root{}
	}
	if root, ok := e.rootHandles[base]; ok {
		return root, nil
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", base, err)
	}
	e.rootHandles[base] = root
	return root, nil
}

func (e *UltraFastEngine) statWithinRoot(path string) (os.FileInfo, error) {
	open, bases := e.allowedBases()
	if open {
		return os.Stat(path)
	}
	base, rel, err := relWithinBases(path, bases)
	if err != nil {
		return nil, err
	}
	root, err := e.rootFor(base)
	if err != nil {
		return nil, err
	}
	return root.Stat(rel)
}

func relWithinBases(path string, bases []string) (base, rel string, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	target := foldRootPath(abs)
	var best string
	for _, b := range bases {
		nb := foldRootPath(b)
		if target == nb || strings.HasPrefix(target, nb+string(os.PathSeparator)) {
			if best == "" || len(nb) > len(foldRootPath(best)) {
				best = b
			}
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("path is outside os.Root bases")
	}
	trimmed := target[len(foldRootPath(best)):]
	trimmed = strings.TrimPrefix(trimmed, string(os.PathSeparator))
	if trimmed == "" {
		return best, ".", nil
	}
	return best, trimmed, nil
}

func foldRootPath(p string) string {
	p = filepath.Clean(p)
	if os.PathSeparator == '\\' {
		return strings.ToLower(p)
	}
	return p
}
