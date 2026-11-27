package cache

import (
	"bytes"
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
	// Hash is the OCC content hash of the bytes stored with this entry.
	// It is a memo, not part of the freshness verdict: freshness is
	// size + mtime + identity, and an empty Hash means "not computed".
	// Storing it lets a cache hit return the OCC token without hashing
	// the whole file again.
	Hash string
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
	return ReadFileStableOpen(path, os.Open)
}

func ReadFileStableOpen(path string, open func(string) (*os.File, error)) (content []byte, meta FileStatMeta, stable bool, err error) {
	if open == nil {
		open = os.Open
	}
	var last []byte
	var lastMeta FileStatMeta
	for attempt := 0; attempt < maxCaptureAttempts; attempt++ {
		content, meta, stable, err = readFileCaptureOnce(path, open)
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

// readAllSized reads f to EOF, pre-sizing the buffer from the known file
// size. A negative size falls back to the growth strategy of io.ReadAll.
func readAllSized(f *os.File, size int64) ([]byte, error) {
	if size < 0 {
		return io.ReadAll(f)
	}
	var buf bytes.Buffer
	if size > 0 {
		// One extra byte so the final Read that reports EOF does not have
		// to grow the buffer.
		buf.Grow(int(size) + 1)
	}
	if _, err := buf.ReadFrom(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readFileCaptureOnce(path string, open func(string) (*os.File, error)) ([]byte, FileStatMeta, bool, error) {
	f, err := open(path)
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

	// Read into a buffer sized from the stat. io.ReadAll starts at 512
	// bytes and doubles, so reading a 5 MB file copied ~11 MB of garbage
	// before the content was even used.
	content, err := readAllSized(f, before.Size)
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
