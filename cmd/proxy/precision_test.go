package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTimedOutIDsStayBounded(t *testing.T) {
	ids := map[string]time.Time{}
	start := time.Now()
	for i := 0; i < maxTimedOutIDs+10; i++ {
		noteTimedOut(ids, fmt.Sprintf("id-%d", i), start.Add(time.Duration(i)))
	}
	if len(ids) != maxTimedOutIDs {
		t.Fatalf("len=%d, want cap %d", len(ids), maxTimedOutIDs)
	}
	noteTimedOut(ids, "stale", start.Add(-timedOutTTL-time.Second))
	if _, ok := ids["stale"]; ok {
		t.Fatal("expired timeout mark must not accumulate")
	}
	ids["late"] = start
	if !consumeLateTimeout(ids, "late", start.Add(time.Minute)) {
		t.Fatal("fresh timeout mark should swallow the late response")
	}
	if consumeLateTimeout(ids, "late", start) {
		t.Fatal("mark must be consumed once")
	}
}

func TestRequestKeyDistinguishesStringAndNumber(t *testing.T) {
	if requestKey([]byte(`1`)) == requestKey([]byte(`"1"`)) {
		t.Fatal("numeric and string JSON-RPC ids must not share a pending slot")
	}
	if extractID([]byte(`"abc"`)) != "abc" {
		t.Fatal("logged request id should remain the raw id text")
	}
}

func TestDurationPreservesSubMillisecond(t *testing.T) {
	var e ProxyLogEntry
	e.setDuration(123456 * time.Nanosecond)
	b, err := json.Marshal(e)
	if err != nil || e.DurationMs != 0 || !strings.Contains(string(b), `"duration_ns":123456`) {
		t.Fatalf("%s %v", b, err)
	}
}

func TestFastRepliesAllLoggedBeforeForward(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cfg := helperCfg(t, "ok", 2*time.Second)
	done := make(chan int, 1)
	go func() { done <- runProxy(cfg, inR, outW, io.Discard); outW.Close() }()
	defer inW.Close()
	sc := bufio.NewScanner(outR)
	for i := 1; i <= 100; i++ {
		if _, err := fmt.Fprintf(inW, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"method\":\"tools/call\",\"params\":{\"name\":\"read_file\"}}\n", i); err != nil {
			t.Fatal(err)
		}
		if !sc.Scan() {
			t.Fatal("missing reply", sc.Err())
		}
		b, err := os.ReadFile(filepath.Join(cfg.logDir, "proxy.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) != i {
			t.Fatalf("reply %d preceded log: %d", i, len(lines))
		}
		var e ProxyLogEntry
		if err := json.Unmarshal([]byte(lines[i-1]), &e); err != nil || e.RequestID != fmt.Sprint(i) || e.DurationNs <= 0 || e.Status != "ok" {
			t.Fatalf("entry %+v %v", e, err)
		}
	}
	inW.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not close")
	}
}
