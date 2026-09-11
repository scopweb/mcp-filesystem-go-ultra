package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if v := os.Getenv("MCP_PROXY_HELPER"); v != "" {
		helperMain(v)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperMain(mode string) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	switch mode {
	case "hang":
		for scanner.Scan() {
		}
	case "ok", "slow":
		for scanner.Scan() {
			var msg jsonRPCMessage
			if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.Method != "tools/call" {
				continue
			}
			if mode == "slow" {
				time.Sleep(400 * time.Millisecond)
			}
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"result": map[string]any{
					"content": []map[string]string{{"type": "text", "text": "ok"}},
				},
			}
			b, _ := json.Marshal(resp)
			os.Stdout.Write(b)
			os.Stdout.Write([]byte("\n"))
		}
	}
}

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func helperCfg(t *testing.T, mode string, callTimeout time.Duration) config {
	t.Helper()
	return config{
		logDir:      t.TempDir(),
		callTimeout: callTimeout,
		reapStale:   false,
		target:      []string{os.Args[0], "-test.run=^$"},
		childEnv:    append(os.Environ(), "MCP_PROXY_HELPER="+mode),
	}
}

func waitContains(t *testing.T, buf *safeBuf, sub string, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := buf.String()
		if strings.Contains(s, sub) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %q, got: %s", sub, buf.String())
	return ""
}

func TestCallTimeout_HangingChild(t *testing.T) {
	inR, inW := io.Pipe()
	out := &safeBuf{}
	errOut := &safeBuf{}
	cfg := helperCfg(t, "hang", 200*time.Millisecond)
	done := make(chan int, 1)
	go func() {
		done <- runProxy(cfg, inR, out, errOut)
	}()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"apply_patch","arguments":{"path":"f.txt"}}}`
	if _, err := io.WriteString(inW, req+"\n"); err != nil {
		t.Fatal(err)
	}
	s := waitContains(t, out, "timed out", 3*time.Second)
	if !strings.Contains(s, "apply_patch") {
		t.Fatalf("timeout should name the tool: %s", s)
	}
	if !strings.Contains(s, `"isError":true`) {
		t.Fatalf("expected MCP tool error, got %s", s)
	}
	_ = inW.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not exit after stdin close")
	}
}

func TestCallTimeout_FastChildOK(t *testing.T) {
	inR, inW := io.Pipe()
	out := &safeBuf{}
	cfg := helperCfg(t, "ok", time.Second)
	done := make(chan int, 1)
	go func() {
		done <- runProxy(cfg, inR, out, io.Discard)
	}()

	req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"server_info","arguments":{}}}`
	if _, err := io.WriteString(inW, req+"\n"); err != nil {
		t.Fatal(err)
	}
	s := waitContains(t, out, `"text":"ok"`, 3*time.Second)
	if strings.Contains(s, "timed out") {
		t.Fatalf("fast child should not time out: %s", s)
	}
	_ = inW.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not exit")
	}
}

func TestCallTimeout_SwallowsLateResponse(t *testing.T) {
	inR, inW := io.Pipe()
	out := &safeBuf{}
	cfg := helperCfg(t, "slow", 100*time.Millisecond)
	done := make(chan int, 1)
	go func() {
		done <- runProxy(cfg, inR, out, io.Discard)
	}()

	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"apply_patch","arguments":{}}}`
	if _, err := io.WriteString(inW, req+"\n"); err != nil {
		t.Fatal(err)
	}
	_ = waitContains(t, out, "timed out", 3*time.Second)
	time.Sleep(500 * time.Millisecond)
	s := out.String()
	if strings.Count(s, `"id":3`) != 1 {
		t.Fatalf("expected exactly one response for id=3, got %s", s)
	}
	if strings.Contains(s, `"text":"ok"`) {
		t.Fatalf("late child response must be swallowed: %s", s)
	}
	_ = inW.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not exit")
	}
}
