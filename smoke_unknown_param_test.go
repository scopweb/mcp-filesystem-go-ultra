package main

// Smoke E2E: unknown parameters must be rejected with a clear error instead of
// being silently ignored. Regression test for the report where a typo'd
// `include>` param on search_files was accepted without any feedback.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runSmokeCallRaw spawns the MCP binary, performs the handshake, invokes the
// given tool with args, and returns the raw tool result (error results are
// NOT fatal, unlike runSmokeCase).
func runSmokeCallRaw(t *testing.T, binPath, workdir, toolName string, args map[string]any) mcpToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = workdir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start binary: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = io.ReadAll(stderr)
		_ = cmd.Wait()
	}()

	send := func(m mcpMessage) {
		b, _ := json.Marshal(m)
		if _, err := stdin.Write(append(b, '\n')); err != nil {
			t.Fatalf("write to stdin: %v", err)
		}
	}
	recv := func() (mcpMessage, error) {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		if !scanner.Scan() {
			err := scanner.Err()
			if err == nil {
				err = io.EOF
			}
			return mcpMessage{}, fmt.Errorf("scan stdout: %w", err)
		}
		var m mcpMessage
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			return mcpMessage{}, fmt.Errorf("unmarshal: %w (line=%q)", err, scanner.Text())
		}
		return m, nil
	}

	initParams, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "smoke", "version": "0.1"},
	})
	send(mcpMessage{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: initParams})
	if _, err := recv(); err != nil {
		t.Fatalf("initialize response: %v", err)
	}
	send(mcpMessage{JSONRPC: "2.0", Method: "notifications/initialized"})

	callParams, _ := json.Marshal(map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	send(mcpMessage{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: callParams})
	resp, err := recv()
	if err != nil {
		t.Fatalf("tools/call response: %v", err)
	}

	var toolResult mcpToolResult
	if err := json.Unmarshal(resp.Result, &toolResult); err != nil {
		t.Fatalf("unmarshal result: %v (raw=%s)", err, string(resp.Result))
	}
	return toolResult
}

func TestSmoke_SearchFiles_UnknownParamRejected(t *testing.T) {
	if os.Getenv("SKIP_SMOKE") == "1" {
		t.Skip("SKIP_SMOKE=1")
	}
	binPath := findBinary(t)

	workdir := t.TempDir()
	if err := os.WriteFile(workdir+string(os.PathSeparator)+"a.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res := runSmokeCallRaw(t, binPath, workdir, "search_files", map[string]any{
		"path":     workdir,
		"pattern":  "hello",
		"include>": "*.txt", // typo: user meant "include"
	})
	if !res.IsError {
		t.Fatalf("expected error for unknown param \"include>\", got success: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "unknown parameter") {
		t.Errorf("error should mention 'unknown parameter', got: %s", res.Content[0].Text)
	}
}
