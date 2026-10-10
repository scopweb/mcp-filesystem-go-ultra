package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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

func (e *UltraFastEngine) lstatWithinRoot(path string) (os.FileInfo, error) {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return nil, err
	}
	if open {
		return os.Lstat(path)
	}
	return root.Lstat(rel)
}

func (e *UltraFastEngine) statWithinRoot(path string) (os.FileInfo, error) {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return nil, err
	}
	if open {
		return os.Stat(path)
	}
	return root.Stat(rel)
}

func (e *UltraFastEngine) readDirWithinRoot(path string) ([]os.DirEntry, error) {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return nil, err
	}
	if open {
		return os.ReadDir(path)
	}
	dir, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

func (e *UltraFastEngine) openWithinRoot(path string) (*os.File, error) {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return nil, err
	}
	if open {
		return os.Open(path)
	}
	return root.Open(rel)
}

func (e *UltraFastEngine) readWithinRoot(path string) ([]byte, error) {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return nil, err
	}
	if open {
		return os.ReadFile(path)
	}
	return root.ReadFile(rel)
}

func (e *UltraFastEngine) locateRoot(path string) (*os.Root, string, bool, error) {
	open, bases := e.allowedBases()
	if open {
		return nil, "", true, nil
	}
	base, rel, err := relWithinBases(path, bases)
	if err != nil {
		return nil, "", false, err
	}
	root, err := e.rootFor(base)
	if err != nil {
		return nil, "", false, err
	}
	return root, rel, false, nil
}

func (e *UltraFastEngine) atomicWriteWithinRoot(path string, data []byte, mode os.FileMode) error {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return err
	}
	if open {
		return atomicWriteFile(path, data, mode)
	}
	parent := filepath.Dir(rel)
	if parent != "." && parent != "" {
		if err := root.MkdirAll(parent, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}
	tmpRel := rel + ".tmp." + secureRandomSuffix()
	if err := root.WriteFile(tmpRel, data, mode); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := renameRootWithRetry(root, tmpRel, rel); err != nil {
		_ = root.Remove(tmpRel)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

func (e *UltraFastEngine) moveWithinRoot(src, dst string, createParent bool) error {
	sRoot, sRel, sOpen, err := e.locateRoot(src)
	if err != nil {
		return err
	}
	dRoot, dRel, dOpen, err := e.locateRoot(dst)
	if err != nil {
		return err
	}
	if sOpen || dOpen || sRoot != dRoot {
		if createParent {
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return fmt.Errorf("failed to create destination directory: %w", err)
			}
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("failed to move: %w", err)
		}
		return nil
	}
	if createParent {
		parent := filepath.Dir(dRel)
		if parent != "." && parent != "" {
			if err := dRoot.MkdirAll(parent, 0755); err != nil {
				return fmt.Errorf("failed to create destination directory: %w", err)
			}
		}
	}
	if err := renameRootWithRetry(sRoot, sRel, dRel); err != nil {
		return fmt.Errorf("failed to move: %w", err)
	}
	return nil
}

func (e *UltraFastEngine) mkdirWithinRoot(path string, perm os.FileMode) error {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return err
	}
	if open {
		return os.MkdirAll(path, perm)
	}
	perm &= 0o777
	if perm == 0 {
		perm = 0o755
	}
	return root.MkdirAll(rel, perm)
}

func (e *UltraFastEngine) removeWithinRoot(path string, dir bool) error {
	root, rel, open, err := e.locateRoot(path)
	if err != nil {
		return err
	}
	if open {
		if dir {
			return os.RemoveAll(path)
		}
		return os.Remove(path)
	}
	if dir {
		return root.RemoveAll(rel)
	}
	return root.Remove(rel)
}

func renameRootWithRetry(root *os.Root, oldRel, newRel string) error {
	var err error
	for _, delay := range []time.Duration{0, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond} {
		if delay > 0 {
			time.Sleep(delay)
		}
		err = root.Rename(oldRel, newRel)
		if err == nil {
			return nil
		}
	}
	return err
}

func relWithinBases(path string, bases []string) (base, rel string, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	cleaned := filepath.Clean(abs)
	target := foldRootPath(cleaned)
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
	rel, err = relPreserveCase(cleaned, best)
	if err != nil {
		return "", "", err
	}
	return best, rel, nil
}

func relPreserveCase(cleaned, best string) (string, error) {
	absVol, absParts := splitVolume(cleaned)
	bestVol, bestParts := splitVolume(best)
	if !strings.EqualFold(absVol, bestVol) || len(absParts) < len(bestParts) {
		return "", fmt.Errorf("path is outside os.Root bases")
	}
	for i := range bestParts {
		if !strings.EqualFold(absParts[i], bestParts[i]) {
			return "", fmt.Errorf("path is outside os.Root bases")
		}
	}
	rest := absParts[len(bestParts):]
	if len(rest) == 0 {
		return ".", nil
	}
	return filepath.Join(rest...), nil
}

func splitVolume(p string) (string, []string) {
	p = filepath.Clean(p)
	vol := filepath.VolumeName(p)
	rest := strings.Trim(p[len(vol):], `\/`)
	if rest == "" {
		return vol, nil
	}
	return vol, strings.FieldsFunc(rest, func(r rune) bool {
		return r == '/' || r == '\\'
	})
}

func foldRootPath(p string) string {
	p = filepath.Clean(p)
	if os.PathSeparator == '\\' {
		return strings.ToLower(p)
	}
	return p
}
