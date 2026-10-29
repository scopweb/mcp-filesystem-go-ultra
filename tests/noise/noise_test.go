package noise

// Response-noise budget.
//
// This package drives the REAL server binary over stdio with a fixed fixture
// and the same tool arguments on every run, so the serialized size of each
// response can be compared before/after a change and guarded by a budget.
//
// It measures three channels of every tool result:
//   - wire:   the complete JSON-RPC response line (what the client receives),
//   - text:   the text fallback content block,
//   - struct: the JSON-serialized structuredContent.
//
// The text block and structuredContent carry the same information twice by
// design (compat mode needs the text copy for clients that never render
// structuredContent). This test keeps that duplication from growing, and
// checks that --response-mode=structured really keeps every body once.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------- raw client

type rawClient struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines <-chan string
	id    int
}

func startRaw(t *testing.T, exe string, args []string) *rawClient {
	t.Helper()
	cmd := exec.Command(exe, args...)
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
		done := make(chan struct{})
		go func() {
			_, _ = cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	ch := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	c := &rawClient{cmd: cmd, stdin: stdin, lines: ch}
	c.rpc(t, "initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "noise", "version": "1"},
	})
	c.notify(t, "notifications/initialized", map[string]any{})
	return c
}

func (c *rawClient) notify(t *testing.T, method string, params any) {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, _ := json.Marshal(msg)
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

// rpc returns the raw response line and its decoded envelope.
func (c *rawClient) rpc(t *testing.T, method string, params any) (string, map[string]any) {
	t.Helper()
	c.id++
	id := c.id
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, _ := json.Marshal(msg)
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case line, ok := <-c.lines:
			if !ok {
				t.Fatalf("%s: stdout closed", method)
			}
			var env map[string]any
			if json.Unmarshal([]byte(line), &env) != nil {
				continue
			}
			// The server may ask the client for its Roots list. Answer it
			// with an empty list and keep waiting for our response.
			if method, isReq := env["method"].(string); isReq {
				if rid, ok := env["id"].(float64); ok {
					reply := map[string]any{
						"jsonrpc": "2.0",
						"id":      int(rid),
						"result":  map[string]any{"roots": []any{}},
					}
					rb, _ := json.Marshal(reply)
					_, _ = c.stdin.Write(append(rb, '\n'))
				}
				_ = method
				continue
			}
			if got, ok := env["id"].(float64); !ok || int(got) != id {
				continue
			}
			return line, env
		case <-time.After(time.Until(deadline)):
			t.Fatalf("%s: timeout", method)
		}
	}
	t.Fatalf("%s: timeout", method)
	return "", nil
}

type sample struct {
	tool    string
	wire    int
	text    int
	struct_ int
}

func (c *rawClient) call(t *testing.T, tool string, args map[string]any) sample {
	t.Helper()
	raw, env := c.rpc(t, "tools/call", map[string]any{"name": tool, "arguments": args})
	if env["error"] != nil {
		t.Fatalf("%s rpc error: %v", tool, env["error"])
	}
	res, _ := env["result"].(map[string]any)
	if res == nil {
		t.Fatalf("%s missing result: %v", tool, env)
	}
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("%s failed: %s", tool, textOf(res))
	}
	structured, _ := json.Marshal(res["structuredContent"])
	return sample{tool: tool, wire: len(raw), text: len(textOf(res)), struct_: len(structured)}
}

func textOf(res map[string]any) string {
	arr, _ := res["content"].([]any)
	if len(arr) == 0 {
		return ""
	}
	m, _ := arr[0].(map[string]any)
	s, _ := m["text"].(string)
	return s
}

// ------------------------------------------------------------------- fixture

func buildServer(t *testing.T) string {
	t.Helper()
	if candidate := os.Getenv("MCP_NOISE_BINARY"); candidate != "" {
		return candidate
	}
	exe := filepath.Join(t.TempDir(), "fsu-noise")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", exe, "github.com/mcp/filesystem-ultra/cmd/filesystem-ultra").CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return exe
}

// fixtureDir is a fixed-length path so every byte count below is comparable
// between runs (the absolute path length is part of the payload).
func fixtureDir(t *testing.T, tag string) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "fsu-noise-fixture-"+tag)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	var big strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&big, "alpha line %02d with a long payload so the fixture is not trivial\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 24; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file%02d.txt", i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("alpha content %02d\n", i)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		name := filepath.Join(dir, "sub", fmt.Sprintf("nested%02d.txt", i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("alpha nested %02d\n", i)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// ----------------------------------------------------------------- scenario

func scenario(t *testing.T, c *rawClient, dir string) []sample {
	t.Helper()
	var samples []sample
	samples = append(samples, c.call(t, "list_allowed_directories", map[string]any{}))
	samples = append(samples, c.call(t, "project_map", map[string]any{"path": dir, "depth": float64(2)}))
	samples = append(samples, c.call(t, "list_directory", map[string]any{"path": dir}))
	samples = append(samples, c.call(t, "directory_tree", map[string]any{"path": dir}))
	samples = append(samples, c.call(t, "search_files", map[string]any{
		"path": dir, "pattern": "alpha", "include_content": true, "max_results": float64(40),
	}))
	samples = append(samples, c.call(t, "read_file", map[string]any{"path": filepath.Join(dir, "big.txt")}))
	samples = append(samples, c.call(t, "edit_file", map[string]any{
		"path": filepath.Join(dir, "big.txt"), "old_text": "alpha line 01", "new_text": "beta line 01",
	}))
	if _, err := exec.LookPath("git"); err == nil {
		samples = append(samples, c.call(t, "git", map[string]any{"action": "init", "path": dir}))
		samples = append(samples, c.call(t, "git", map[string]any{"action": "status", "path": dir}))
		samples = append(samples, c.call(t, "git", map[string]any{"action": "status", "path": dir, "output": "full"}))
	}
	return samples
}

func total(samples []sample) (wire, text, structured int) {
	for _, s := range samples {
		wire += s.wire
		text += s.text
		structured += s.struct_
	}
	return
}

// modes are the server configurations measured. The flags are part of the
// scenario: the same calls with the same arguments must produce the same
// bytes across runs, so a change in a flag shows up as a size change.
var modes = []struct {
	name   string
	flags  []string
	budget int
}{
	{"default", nil, 34000},
	{"compact", []string{"--compact-mode"}, 34000},
	{"structured", []string{"--response-mode=structured"}, 25000},
	{"compact+structured", []string{"--compact-mode", "--response-mode=structured"}, 25000},
}

func TestResponseNoiseBudget(t *testing.T) {
	exe := buildServer(t)

	for i, mode := range modes {
		dir := fixtureDir(t, fmt.Sprintf("%d", i))
		args := []string{"--profile=ultra", "--roots-mode=union"}
		args = append(args, mode.flags...)
		args = append(args, dir)
		c := startRaw(t, exe, args)
		samples := scenario(t, c, dir)
		wire, text, structured := total(samples)
		for _, s := range samples {
			t.Logf("mode=%-18s %-22s wire=%6d text=%6d structured=%6d", mode.name, s.tool, s.wire, s.text, s.struct_)
		}
		t.Logf("mode=%-18s TOTAL wire=%d text=%d structured=%d", mode.name, wire, text, structured)
		if wire > mode.budget {
			t.Errorf("mode=%s: wire payload %d B exceeds the budget of %d B", mode.name, wire, mode.budget)
		}
	}
}

// TestResponseNoise_StructuredKeepsTheBodyOnce checks the promise of
// --response-mode=structured: the text block stops carrying the body, so the
// same bytes are not sent twice.
func TestResponseNoise_StructuredKeepsTheBodyOnce(t *testing.T) {
	exe := buildServer(t)
	dir := fixtureDir(t, "t")

	compatDir := fixtureDir(t, "c")
	compat := startRaw(t, exe, []string{"--profile=ultra", "--roots-mode=union", compatDir})
	compatSamples := scenario(t, compat, compatDir)

	structured := startRaw(t, exe, []string{"--profile=ultra", "--roots-mode=union", "--response-mode=structured", dir})
	structuredSamples := scenario(t, structured, dir)

	compatWire, _, _ := total(compatSamples)
	structuredWire, _, _ := total(structuredSamples)
	t.Logf("wire payload: compat=%d structured=%d (%.1f%% smaller)", compatWire, structuredWire,
		100*float64(compatWire-structuredWire)/float64(compatWire))
	if structuredWire >= compatWire {
		t.Errorf("structured mode is not smaller: compat=%d structured=%d", compatWire, structuredWire)
	}
}

// dumpPayloads prints the raw text block and structuredContent of the calls
// that carry the most redundant bytes, so the duplication is visible.
func dumpPayloads(t *testing.T, c *rawClient, dir string) {
	t.Helper()
	var dump strings.Builder
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"list_directory", map[string]any{"path": dir}},
		{"directory_tree", map[string]any{"path": dir}},
		{"search_files", map[string]any{"path": dir, "pattern": "alpha", "include_content": true, "max_results": float64(40)}},
		{"project_map", map[string]any{"path": dir, "depth": float64(2)}},
	} {
		_, env := c.rpc(t, "tools/call", map[string]any{"name": tc.tool, "arguments": tc.args})
		res, _ := env["result"].(map[string]any)
		if res == nil {
			continue
		}
		dump.WriteString(fmt.Sprintf("---- %s TEXT ----\n%s\n", tc.tool, textOf(res)))
		b, _ := json.MarshalIndent(res["structuredContent"], "", " ")
		dump.WriteString(fmt.Sprintf("---- %s STRUCTURED ----\n%s\n", tc.tool, string(b)))
	}
	out := filepath.Join(os.TempDir(), "fsu-noise-dump.txt")
	if err := os.WriteFile(out, []byte(dump.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("dump written to %s (%d bytes)", out, dump.Len())
}
