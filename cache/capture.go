package cache

import (
	"io"
	"os"
	"sync"
	"time"
)

const maxCaptureAttempts = 3

type FileIdentity struct {
	vol uint64
	idx uint64
}

type FileStatMeta struct {
	Valid    bool
	Mtime    time.Time
	Size     int64
	Identity FileIdentity
	HasID    bool
}

type FileGen struct {
	Global uint64
	Local  uint64
}

var (
	captureHookMu    sync.Mutex
	captureAfterOpen func(string)
	captureMutate    func(string)
)

func SetCaptureHooksForTest(afterOpen, mutate func(string)) {
	captureHookMu.Lock()
	captureAfterOpen = afterOpen
	captureMutate = mutate
	captureHookMu.Unlock()
}

func runCaptureAfterOpen(path string) {
	captureHookMu.Lock()
	fn := captureAfterOpen
	captureHookMu.Unlock()
	if fn != nil {
		fn(path)
	}
}

func runCaptureMutate(path string) {
	captureHookMu.Lock()
	fn := captureMutate
	captureHookMu.Unlock()
	if fn != nil {
		fn(path)
	}
}

func MetaFromInfo(info os.FileInfo) FileStatMeta {
	if info == nil {
		return FileStatMeta{}
	}
	meta := FileStatMeta{
		Valid: true,
		Mtime: info.ModTime(),
		Size:  info.Size(),
	}
	if id, ok := identityFromInfo(info); ok {
		meta.Identity = id
		meta.HasID = true
	}
	return meta
}

func MetaFromFile(f *os.File) (FileStatMeta, error) {
	info, err := f.Stat()
	if err != nil {
		return FileStatMeta{}, err
	}
	meta := MetaFromInfo(info)
	if id, ok := identityFromFile(f); ok {
		meta.Identity = id
		meta.HasID = true
	}
	return meta, nil
}

func (a FileStatMeta) sameVersion(b FileStatMeta) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	if a.Size != b.Size || !a.Mtime.Equal(b.Mtime) {
		return false
	}
	if a.HasID && b.HasID && a.Identity != b.Identity {
		return false
	}
	return true
}

func ReadFileStable(path string) (content []byte, meta FileStatMeta, stable bool, err error) {
	var last []byte
	var lastMeta FileStatMeta
	for attempt := 0; attempt < maxCaptureAttempts; attempt++ {
		content, meta, stable, err = readFileCaptureOnce(path)
		if err != nil {
			return nil, FileStatMeta{}, false, err
		}
		last, lastMeta = content, meta
		if stable {
			return content, meta, true, nil
		}
	}
	return last, lastMeta, false, nil
}

func readFileCaptureOnce(path string) ([]byte, FileStatMeta, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, FileStatMeta{}, false, err
	}
	defer f.Close()

	runCaptureAfterOpen(path)

	before, err := MetaFromFile(f)
	if err != nil {
		return nil, FileStatMeta{}, false, err
	}

	runCaptureMutate(path)

	content, err := io.ReadAll(f)
	if err != nil {
		return nil, FileStatMeta{}, false, err
	}

	after, err := MetaFromFile(f)
	if err != nil {
		return nil, FileStatMeta{}, false, err
	}
	if int64(len(content)) != after.Size || !before.sameVersion(after) {
		return content, after, false, nil
	}
	return content, after, true, nil
}
