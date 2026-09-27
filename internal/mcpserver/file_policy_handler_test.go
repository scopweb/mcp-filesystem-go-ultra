package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func TestFilePolicy_HandlerMatrix(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	envPath := write(".env", "TOKEN=secret\n")
	iniPath := write("app.ini", "password=1\n")
	settings := write("settings.json", "{\"a\":1}\n")
	_ = write(filepath.Join("secrets", "key.txt"), "hidden-bytes\n")
	normal := write("main.txt", "hello\n")
	cfg := write("policy.json", `{
	  "version": 1,
	  "rules": [
	    {"pattern": ".env", "level": "hidden"},
	    {"pattern": ".env.*", "level": "hidden"},
	    {"pattern": "*.ini", "level": "protected"},
	    {"pattern": "settings.json", "level": "read_only"},
	    {"pattern": "**/secrets/**", "level": "hidden"}
	  ]
	}`)

	reg := newHelpTestRegistry(t, dir)
	policy, err := core.LoadFilePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.PinConfig(cfg); err != nil {
		t.Fatal(err)
	}
	reg.engine.SetFilePolicy(policy)

	call := func(tool string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		h := reg.handlers[tool]
		if h == nil {
			t.Fatalf("missing handler %s", tool)
		}
		res, err := h(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool, Arguments: args}})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		return res
	}
	text := func(res *mcp.CallToolResult) string {
		t.Helper()
		return resultText(t, res)
	}

	readEnv := call("read_file", map[string]any{"path": envPath})
	if !readEnv.IsError || !strings.Contains(text(readEnv), `"code":"NOT_FOUND"`) || strings.Contains(text(readEnv), "FILE_POLICY") || strings.Contains(text(readEnv), "hidden") {
		t.Fatalf("hidden read: %s", text(readEnv))
	}
	if _, err := os.ReadFile(envPath); err != nil {
		t.Fatal(err)
	}

	readINI := call("read_file", map[string]any{"path": iniPath})
	if !readINI.IsError || !strings.Contains(text(readINI), `"code":"FILE_POLICY_DENIED"`) || strings.Contains(text(readINI), "password=1") {
		t.Fatalf("protected read: %s", text(readINI))
	}

	readSettings := call("read_file", map[string]any{"path": settings})
	if readSettings.IsError || !strings.Contains(text(readSettings), `"a":1`) {
		t.Fatalf("read_only read: %s", text(readSettings))
	}
	writeSettings := call("write_file", map[string]any{"path": settings, "content": "nope"})
	if !writeSettings.IsError || !strings.Contains(text(writeSettings), `"code":"FILE_POLICY_DENIED"`) || strings.Contains(text(writeSettings), `"retryable":true`) {
		t.Fatalf("read_only write: %s", text(writeSettings))
	}
	got, err := os.ReadFile(settings)
	if err != nil || string(got) != "{\"a\":1}\n" {
		t.Fatalf("settings mutated: %q %v", got, err)
	}

	list := call("list_directory", map[string]any{"path": dir, "output_format": "json"})
	if list.IsError {
		t.Fatalf("list: %s", text(list))
	}
	listing := text(list)
	if strings.Contains(listing, ".env") || strings.Contains(listing, "secrets") || strings.Contains(listing, "hidden-bytes") || strings.Contains(listing, "password=1") {
		t.Fatalf("listing leaked hidden or content:\n%s", listing)
	}
	if !strings.Contains(listing, "app.ini") || !strings.Contains(listing, "protected") {
		t.Fatalf("protected name missing:\n%s", listing)
	}
	if !strings.Contains(listing, "main.txt") || !strings.Contains(listing, "settings.json") {
		t.Fatalf("visible names missing:\n%s", listing)
	}

	search := call("search_files", map[string]any{"path": dir, "pattern": "hidden-bytes", "include_content": true})
	if strings.Contains(text(search), "key.txt") || strings.Contains(text(search), "Content matches") {
		t.Fatalf("content search leaked hidden file: %s", text(search))
	}
	count := call("search_files", map[string]any{"path": dir, "pattern": "TOKEN", "count_only": true})
	if strings.Contains(text(count), ".env") || strings.Contains(text(count), "secret") {
		t.Fatalf("count leaked hidden: %s", text(count))
	}

	createHidden := call("write_file", map[string]any{"path": filepath.Join(dir, ".env.local"), "content": "x"})
	if !createHidden.IsError || !strings.Contains(text(createHidden), `"code":"NOT_FOUND"`) {
		t.Fatalf("create hidden name: %s", text(createHidden))
	}
	if _, err := os.Stat(filepath.Join(dir, ".env.local")); !os.IsNotExist(err) {
		t.Fatal("hidden name was created")
	}

	parent := call("delete_file", map[string]any{"path": dir, "permanent": true})
	if !parent.IsError {
		t.Fatal("deleting a parent of a protected file must fail")
	}
	if strings.Contains(text(parent), "key.txt") || strings.Contains(text(parent), ".env") {
		t.Fatalf("parent delete named a hidden path: %s", text(parent))
	}
	if _, err := os.Stat(normal); err != nil {
		t.Fatal("parent delete changed the tree")
	}

	cfgWrite := call("write_file", map[string]any{"path": cfg, "content": `{"version":1,"rules":[]}`})
	if !cfgWrite.IsError {
		t.Fatal("config file must not be writable")
	}

	info := call("security_policy", nil)
	if info.IsError {
		t.Fatalf("security_policy: %s", text(info))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(text(info)), &body); err != nil {
		t.Fatal(err)
	}
	if body["enabled"] != true || body["agent_can_modify"] != false || body["precedence"] != "most_restrictive" {
		t.Fatalf("policy body %#v", body)
	}
	if strings.Contains(text(info), ".env") || strings.Contains(text(info), "policy.json") || strings.Contains(text(info), "secrets") {
		t.Fatalf("security_policy leaked rules: %s", text(info))
	}

	git := call("git", map[string]any{"action": "status", "path": dir})
	if !git.IsError || strings.Contains(text(git), "hidden-bytes") || !strings.Contains(text(git), "FILE_POLICY_DENIED") {
		t.Fatalf("git: %s", text(git))
	}
}

func TestFilePolicy_AbsentPreservesAccess(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("TOKEN=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().AllowSecrets = true
	res, err := reg.handlers["read_file"](context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "read_file", Arguments: map[string]any{"path": p}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("no policy must still read: %s", resultText(t, res))
	}
}
