package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type rpcClient struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines <-chan string
	id    int
}

func startRPC(proxy, logDir, server, workDir, profile string) (*rpcClient, error) {
	if profile == "" {
		profile = "strict"
	}
	cmd := exec.Command(proxy,
		"--model", "pr0-baseline",
		"--log-dir", logDir,
		"--reap-stale=false",
		"--",
		server,
		"--profile", profile,
		"--compact-mode",
		"--roots-mode", "union",
		workDir,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ch := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	c := &rpcClient{cmd: cmd, stdin: stdin, lines: ch}
	initRes, err := c.rpc("initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "pr0-baseline", "version": "1"},
	})
	if err != nil {
		c.close()
		return nil, err
	}
	if initRes["result"] == nil {
		c.close()
		return nil, fmt.Errorf("initialize: %#v", initRes)
	}
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *rpcClient) close() {
	_ = c.stdin.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
}

func (c *rpcClient) notify(method string, params any) error {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *rpcClient) rpc(method string, params any) (map[string]any, error) {
	c.id++
	id := c.id
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		remain := time.Until(deadline)
		select {
		case line, ok := <-c.lines:
			if !ok {
				return nil, fmt.Errorf("%s: stdout closed", method)
			}
			var env map[string]any
			if json.Unmarshal([]byte(line), &env) != nil {
				continue
			}
			if method, _ := env["method"].(string); method == "roots/list" {
				if err := c.respondRoots(env["id"]); err != nil {
					return nil, err
				}
				continue
			}
			got, ok := asInt(env["id"])
			if !ok || got != id {
				continue
			}
			return env, nil
		case <-time.After(remain):
			return nil, fmt.Errorf("%s: timeout id=%d", method, id)
		}
	}
	return nil, fmt.Errorf("%s: timeout", method)
}

func (c *rpcClient) respondRoots(id any) error {
	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"roots": []any{}},
	}
	b, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *rpcClient) tool(name string, args map[string]any) (map[string]any, error) {
	env, err := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	if env["error"] != nil {
		return nil, fmt.Errorf("%s rpc error: %#v", name, env["error"])
	}
	res, _ := env["result"].(map[string]any)
	if res == nil {
		return nil, fmt.Errorf("%s missing result: %#v", name, env)
	}
	return res, nil
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

func resultText(res map[string]any) string {
	arr, _ := res["content"].([]any)
	if len(arr) == 0 {
		return ""
	}
	m, _ := arr[0].(map[string]any)
	s, _ := m["text"].(string)
	return s
}

func structured(res map[string]any) map[string]any {
	m, _ := res["structuredContent"].(map[string]any)
	return m
}

func isError(res map[string]any) bool {
	b, _ := res["isError"].(bool)
	return b
}
