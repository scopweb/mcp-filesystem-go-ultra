package mcpserver

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readLifecycle(t *testing.T, dir string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "lifecycle.jsonl"))
	if err != nil {
		t.Fatalf("open lifecycle.jsonl: %v", err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		out = append(out, rec)
	}
	return out
}

func TestLifecycleLogger_StartStopAndCrashFileCleanup(t *testing.T) {
	dir := t.TempDir()
	lc := newLifecycleLogger(dir)
	if lc == nil {
		t.Fatal("expected logger for non-empty dir")
	}
	crash := lc.installCrashOutput()
	if crash == "" {
		t.Fatal("installCrashOutput returned empty path")
	}
	if _, err := os.Stat(crash); err != nil {
		t.Fatalf("crash file should exist while running: %v", err)
	}

	lc.event("start", map[string]any{"version": "test"})
	lc.event("stop", map[string]any{"reason": "stdin_closed_or_signal"})
	lc.removeEmptyCrashFile()

	if _, err := os.Stat(crash); !os.IsNotExist(err) {
		t.Fatalf("empty crash file should be removed on clean stop, stat err=%v", err)
	}

	recs := readLifecycle(t, dir)
	if len(recs) != 2 {
		t.Fatalf("want 2 events, got %d: %v", len(recs), recs)
	}
	if recs[0]["kind"] != "start" || recs[0]["version"] != "test" {
		t.Errorf("first event: %v", recs[0])
	}
	if recs[1]["kind"] != "stop" || recs[1]["reason"] != "stdin_closed_or_signal" {
		t.Errorf("second event: %v", recs[1])
	}
	for _, r := range recs {
		if pid, ok := r["pid"].(float64); !ok || int(pid) != os.Getpid() {
			t.Errorf("pid missing or wrong in %v", r)
		}
	}
}

func TestLifecycleLogger_NilIsNoop(t *testing.T) {
	var lc *lifecycleLogger
	if lc = newLifecycleLogger(""); lc != nil {
		t.Fatal("empty log dir must yield nil logger")
	}
	lc.event("start", nil)
	if got := lc.installCrashOutput(); got != "" {
		t.Fatalf("nil logger must not install crash output, got %q", got)
	}
	lc.removeEmptyCrashFile()
}
