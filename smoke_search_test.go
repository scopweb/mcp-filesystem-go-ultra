package main

// Smoke E2E for search_files output_format using the actual MCP binary via stdio.
// This is an integration test (not a unit test) — it spawns the built binary and
// exchanges JSON-RPC messages to verify the new default output format works
// end-to-end through the full MCP tool handler.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mcpMessage mirrors the JSON-RPC envelope used by the MCP server.
type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// runSmokeCase spawns the MCP binary, performs the initialize handshake,
// invokes search_files with the given args, and returns the result text.
func runSmokeCase(t *testing.T, binPath, workdir string, args map[string]any) string {
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

	// 1. initialize
	initParams, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "smoke", "version": "0.1"},
	})
	send(mcpMessage{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: initParams})
	if _, err := recv(); err != nil {
		t.Fatalf("initialize response: %v", err)
	}

	// 2. initialized notification (no response expected)
	send(mcpMessage{JSONRPC: "2.0", Method: "notifications/initialized"})

	// 3. tools/call
	callParams, _ := json.Marshal(map[string]any{
		"name":      "search_files",
		"arguments": args,
	})
	send(mcpMessage{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: callParams})
	resp, err := recv()
	if err != nil {
		t.Fatalf("tools/call response: %v", err)
	}
	if resp.ID != 2 {
		t.Fatalf("unexpected id=%d, want 2", resp.ID)
	}

	var toolResult mcpToolResult
	if err := json.Unmarshal(resp.Result, &toolResult); err != nil {
		t.Fatalf("unmarshal result: %v (raw=%s)", err, string(resp.Result))
	}
	if len(toolResult.Content) == 0 {
		t.Fatalf("empty content in result: %s", string(resp.Result))
	}
	if toolResult.IsError {
		t.Fatalf("tool reported error: %s", toolResult.Content[0].Text)
	}
	return toolResult.Content[0].Text
}

// TestSmoke_SearchFiles_FewMatches: build, then spawn the binary and verify
// the default output is ripgrep-style for ≤5 matches. Requires the binary at
// bin/filesystem-ultra-v4-new.exe relative to the repo root.
func TestSmoke_SearchFiles_FewMatches(t *testing.T) {
	if os.Getenv("SKIP_SMOKE") == "1" {
		t.Skip("SKIP_SMOKE=1")
	}
	binPath := findBinary(t)

	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "a.txt"), []byte("hello world\nfoo bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "b.txt"), []byte("hello there\nskip\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := runSmokeCase(t, binPath, workdir, map[string]any{
		"path":            workdir,
		"pattern":         "hello",
		"case_sensitive":  false,
		"include_content": true,
	})
	t.Logf("output:\n%s", out)

	if !strings.Contains(out, "a.txt:1:hello world") {
		t.Errorf("expected ripgrep line for a.txt, got: %q", out)
	}
	if !strings.Contains(out, "b.txt:1:hello there") {
		t.Errorf("expected ripgrep line for b.txt, got: %q", out)
	}
	if strings.Contains(out, "🔍 Found") {
		t.Errorf("did not expect verbose header for ≤5 matches, got: %q", out)
	}
}

// TestSmoke_SearchFiles_ManyMatchesVerbose: >5 matches → verbose header.
func TestSmoke_SearchFiles_ManyMatchesVerbose(t *testing.T) {
	if os.Getenv("SKIP_SMOKE") == "1" {
		t.Skip("SKIP_SMOKE=1")
	}
	binPath := findBinary(t)

	workdir := t.TempDir()
	var b strings.Builder
	for i := range 10 {
		fmt.Fprintf(&b, "match line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(workdir, "many.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	out := runSmokeCase(t, binPath, workdir, map[string]any{
		"path":            filepath.Join(workdir, "many.txt"),
		"pattern":         "match",
		"include_content": true,
	})
	t.Logf("output (first 400 chars):\n%s", out[:min(400, len(out))])

	if !strings.Contains(out, "🔍 Found") {
		t.Errorf("expected verbose header for >5 matches, got: %q", out)
	}
}

// TestSmoke_SearchFiles_ExplicitTextBackwardCompat: output_format:"text"
// preserves legacy verbose even for few matches.
func TestSmoke_SearchFiles_ExplicitTextBackwardCompat(t *testing.T) {
	if os.Getenv("SKIP_SMOKE") == "1" {
		t.Skip("SKIP_SMOKE=1")
	}
	binPath := findBinary(t)

	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "t.txt"), []byte("only match\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := runSmokeCase(t, binPath, workdir, map[string]any{
		"path":           workdir,
		"pattern":        "only",
		"case_sensitive": true,
		"output_format":  "text",
	})
	t.Logf("output:\n%s", out)

	if !strings.Contains(out, "🔍 Found") {
		t.Errorf("explicit output_format:text should preserve legacy verbose, got: %q", out)
	}
	if strings.Contains(out, "t.txt:1:only match") {
		t.Errorf("explicit output_format:text should NOT use ripgrep layout, got: %q", out)
	}
}

// TestSmoke_SearchFiles_JSONUnchanged: output_format:"json" still emits JSON.
func TestSmoke_SearchFiles_JSONUnchanged(t *testing.T) {
	if os.Getenv("SKIP_SMOKE") == "1" {
		t.Skip("SKIP_SMOKE=1")
	}
	binPath := findBinary(t)

	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "j.txt"), []byte("find me\nskip\nfind me too\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := runSmokeCase(t, binPath, workdir, map[string]any{
		"path":           workdir,
		"pattern":        "find",
		"case_sensitive": true,
		"output_format":  "json",
	})
	t.Logf("output:\n%s", out)

	if !strings.Contains(out, `"matches":`) {
		t.Errorf("JSON output_format must emit structured JSON, got: %q", out)
	}
	if strings.Contains(out, "🔍") {
		t.Errorf("JSON should not contain emoji headers, got: %q", out)
	}
}

// findBinary locates the binary to smoke-test. Override via SMOKE_BIN env var,
// otherwise default to bin/filesystem-ultra-v4-new.exe relative to cwd.
func findBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("SMOKE_BIN"); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("SMOKE_BIN=%q not found: %v", p, err)
		}
		return p
	}
	// try repo-relative
	cwd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(cwd, "bin", "filesystem-ultra-v4-new.exe"),
		filepath.Join(cwd, "filesystem-ultra-v4-new.exe"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("no smoke binary found (tried %v); set SMOKE_BIN or build with `go build -o bin/filesystem-ultra-v4-new.exe .`", candidates)
	return ""
}