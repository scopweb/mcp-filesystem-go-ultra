package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExecuteHooks_ModifiedContentFollowsConfigOrder(t *testing.T) {
	dir := t.TempDir()
	slowJSON := filepath.Join(dir, "slow.json")
	fastJSON := filepath.Join(dir, "fast.json")
	if err := os.WriteFile(slowJSON, []byte(`{"modified_content":"from-slow"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fastJSON, []byte(`{"modified_content":"from-fast"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	slowCmd, fastCmd := hookOrderCommands(t, dir, slowJSON, fastJSON)
	hm := NewHookManager()
	hm.SetEnabled(true)
	hm.AddHook(HookPreWrite, "*", &Hook{Type: HookTypeCommand, Command: slowCmd, Enabled: true})
	hm.AddHook(HookPreWrite, "*", &Hook{Type: HookTypeCommand, Command: fastCmd, Enabled: true})

	res, err := hm.ExecuteHooks(context.Background(), HookPreWrite, &HookContext{ToolName: "write_file"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ModifiedContent != "from-fast" {
		t.Fatalf("ModifiedContent=%q, want last configured hook %q (stdout=%q stderr=%q)", res.ModifiedContent, "from-fast", res.Stdout, res.Stderr)
	}
}

func hookOrderCommands(t *testing.T, dir, slowJSON, fastJSON string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		slowCmd := filepath.Join(dir, "slow.cmd")
		fastCmd := filepath.Join(dir, "fast.cmd")
		slowBody := "@echo off\r\nping -n 2 127.0.0.1 >nul\r\ntype \"" + slowJSON + "\"\r\n"
		fastBody := "@echo off\r\ntype \"" + fastJSON + "\"\r\n"
		if err := os.WriteFile(slowCmd, []byte(slowBody), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fastCmd, []byte(fastBody), 0o600); err != nil {
			t.Fatal(err)
		}
		return slowCmd, fastCmd
	}
	return "sleep 0.4; cat \"" + slowJSON + "\"", "cat \"" + fastJSON + "\""
}
