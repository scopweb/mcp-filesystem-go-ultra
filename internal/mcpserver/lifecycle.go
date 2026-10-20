package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
)

// lifecycleLogger appends process-level events (start, stop, crash) to
// lifecycle.jsonl under --log-dir. operations.jsonl records tool calls; this
// file records why the process itself came and went, which is the only trace
// left when a client reports "lost connection to the MCP server".
//
// One line per event. The file is opened per event with O_APPEND so several
// server processes sharing a log dir (Desktop + OpenCode + Claude Code) can
// write to it without coordination. A nil logger is a no-op.
type lifecycleLogger struct {
	mu        sync.Mutex
	path      string
	crashPath string
}

func newLifecycleLogger(logDir string) *lifecycleLogger {
	if logDir == "" {
		return nil
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil
	}
	return &lifecycleLogger{
		path:      filepath.Join(logDir, "lifecycle.jsonl"),
		crashPath: filepath.Join(logDir, "crash-"+time.Now().Format("20060102-150405")+"-"+strconv.Itoa(os.Getpid())+".log"),
	}
}

// event appends one JSON line. fields may be nil.
func (l *lifecycleLogger) event(kind string, fields map[string]any) {
	if l == nil {
		return
	}
	rec := map[string]any{
		"ts":   time.Now(),
		"kind": kind,
		"pid":  os.Getpid(),
		"ppid": os.Getppid(),
	}
	for k, v := range fields {
		rec[k] = v
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	data = append(data, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	_, _ = f.Write(data)
	_ = f.Close()
}

// installCrashOutput routes the Go runtime's fatal crash report (unrecovered
// panic in any goroutine, "fatal error: concurrent map writes", deadlock, OOM
// stack dump) to a per-process file next to lifecycle.jsonl. server.WithRecovery
// only covers tool handlers; background goroutines (cache, metrics, normalizer)
// and runtime fatals bypass it and would otherwise leave their trace only in
// the client's stderr capture, if any.
//
// The crash file is created lazily by the runtime: an empty log dir after a
// clean run is expected. Returns the path the runtime will write to.
func (l *lifecycleLogger) installCrashOutput() string {
	if l == nil {
		return ""
	}
	f, err := os.OpenFile(l.crashPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return ""
	}
	// SetCrashOutput dups the descriptor; closing ours here is correct and keeps
	// an idle process from pinning an open handle. Empty files are removed on
	// clean shutdown by removeEmptyCrashFile.
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		_ = f.Close()
		return ""
	}
	_ = f.Close()
	return l.crashPath
}

// removeEmptyCrashFile drops the crash file after a clean stop so the log dir
// only keeps files that carry a real crash report.
//
// The runtime keeps its own duplicate of the crash descriptor open, and
// Windows refuses to delete an open file. Detaching crash output first makes
// the runtime close that handle; a fatal error during the remaining shutdown
// then goes to stderr, which is what an unconfigured process does anyway.
func (l *lifecycleLogger) removeEmptyCrashFile() {
	if l == nil || l.crashPath == "" {
		return
	}
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	if info, err := os.Stat(l.crashPath); err == nil && info.Size() == 0 {
		_ = os.Remove(l.crashPath)
	}
}
