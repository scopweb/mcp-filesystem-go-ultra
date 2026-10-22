package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProxyClientIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, reported, override, mode, want string
	}{
		{"detected", "opencode", "", "ok", "opencode/1.0"},
		{"override", "generic-sdk", " Claude Desktop ", "ok", "Claude Desktop"},
		{"without-initialize", "", "Codex", "ok", "Codex"},
		{"blank-override", "claude-code", "  ", "ok", "claude-code/1.0"},
		{"unknown", "", "", "ok", ""},
		{"timeout", "opencode", "OpenCode", "hang", "OpenCode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := helperCfg(t, tc.mode, 200*time.Millisecond)
			cfg.client = tc.override
			inR, inW := io.Pipe()
			defer inR.Close()
			defer inW.Close()
			out := &safeBuf{}
			done := make(chan int, 1)
			go func() { done <- runProxy(cfg, inR, out, io.Discard) }()
			if tc.reported != "" {
				_, err := fmt.Fprintf(inW, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":%q,"version":"1.0"}}}`+"\n", tc.reported)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := io.WriteString(inW, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"server_info","arguments":{}}}`+"\n"); err != nil {
				t.Fatal(err)
			}
			waitContains(t, out, `"id":2`, 5*time.Second)
			inW.Close()
			select {
			case code := <-done:
				if code != 0 {
					t.Fatalf("proxy exit = %d", code)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("proxy did not exit")
			}
			raw, err := os.ReadFile(filepath.Join(cfg.logDir, "proxy.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var entry ProxyLogEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				t.Fatal(err)
			}
			if entry.Client != tc.want {
				t.Fatalf("client = %q, want %q", entry.Client, tc.want)
			}
		})
	}
}
