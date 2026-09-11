//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
	"time"
)

type rawClient struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines <-chan string
	id    int
}

func startRaw(t *testing.T, exe, workDir string) *rawClient {
	t.Helper()
	cmd := exec.Command(exe, workDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	ch := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	c := &rawClient{cmd: cmd, stdin: stdin, lines: ch}
	initRes := c.rpc(t, "initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "e6-raw", "version": "1"},
	})
	if initRes["result"] == nil {
		t.Fatalf("initialize: %#v", initRes)
	}
	c.notify(t, "notifications/initialized", map[string]any{})
	return c
}

func (c *rawClient) notify(t *testing.T, method string, params any) {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (c *rawClient) rpc(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	c.id++
	id := c.id
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		remain := time.Until(deadline)
		select {
		case line, ok := <-c.lines:
			if !ok {
				t.Fatalf("%s: stdout closed", method)
			}
			var env map[string]any
			if json.Unmarshal([]byte(line), &env) != nil {
				continue
			}
			gotID, ok := asInt(env["id"])
			if !ok || gotID != id {
				continue
			}
			return env
		case <-time.After(remain):
			t.Fatalf("%s: timeout waiting for id=%d", method, id)
		}
	}
	t.Fatalf("%s: timeout", method)
	return nil
}

func (c *rawClient) tool(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	env := c.rpc(t, "tools/call", map[string]any{"name": name, "arguments": args})
	if env["error"] != nil {
		t.Fatalf("%s rpc error: %#v", name, env["error"])
	}
	res, _ := env["result"].(map[string]any)
	if res == nil {
		t.Fatalf("%s missing result: %#v", name, env)
	}
	return res
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func rawStructured(res map[string]any) map[string]any {
	m, _ := res["structuredContent"].(map[string]any)
	return m
}

func rawIsError(res map[string]any) bool {
	b, _ := res["isError"].(bool)
	return b
}

func rawText(res map[string]any) string {
	arr, _ := res["content"].([]any)
	if len(arr) == 0 {
		return ""
	}
	m, _ := arr[0].(map[string]any)
	s, _ := m["text"].(string)
	return s
}

func mustRawOK(t *testing.T, name string, res map[string]any) {
	t.Helper()
	if rawIsError(res) {
		t.Fatalf("%s isError: %s", name, rawText(res))
	}
	if rawStructured(res) == nil && rawText(res) == "" {
		t.Fatalf("%s empty result: %#v", name, res)
	}
}
