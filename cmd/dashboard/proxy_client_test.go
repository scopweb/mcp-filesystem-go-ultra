package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyClientLabels(t *testing.T) {
	for raw, want := range map[string]string{
		"opencode/1.0":     "OpenCode",
		"OpenCode/2.0":     "OpenCode",
		"claude-code/1":    "Claude Code",
		"Claude Code":      "Claude Code",
		"claude-ai/1":      "Claude Desktop",
		"Claude Desktop/2": "Claude Desktop",
		"codex_cli_rs/1":   "Codex",
		"codex-cli/2":      "Codex",
		"Other UI/3":       "Other UI",
		"":                 "Unknown client",
	} {
		if got := proxyClientLabel(raw); got != want {
			t.Errorf("label(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestProxyStatsGroupsClientsAcrossVersionsAndModels(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Join([]string{
		`{"client":"opencode/1","model":"model-a","tool":"read_file","tokens_in":2,"tokens_out":3,"duration_ns":1500000,"status":"ok"}`,
		`{"client":"OpenCode/2","model":"model-b","tool":"edit_file","tokens_in":4,"tokens_out":5,"duration_ms":4,"status":"timeout"}`,
		`{"client":"Claude Desktop/1","tool":"read_file","status":"ok"}`,
		`{"tool":"read_file","status":"ok"}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "proxy.jsonl"), []byte(raw+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	proxyStatsHandler(dir)(rr, httptest.NewRequest(http.MethodGet, "/api/proxy-stats", nil))
	var resp ProxyStatsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	cs := resp.ByClient["OpenCode"]
	if cs == nil || cs.Count != 2 || cs.Errors != 1 || cs.ErrorRate != 50 || cs.TokensIn != 6 || cs.TokensOut != 8 || cs.AvgMs != 2.75 {
		t.Fatalf("OpenCode stats = %+v", cs)
	}
	if len(resp.ByClient) != 3 || resp.ByClient["Unknown client"].Count != 1 || resp.ByClient["Claude Desktop"].Count != 1 {
		t.Fatalf("client groups = %+v", resp.ByClient)
	}
	if resp.TotalCalls != 4 || len(resp.ByModel) != 4 {
		t.Fatalf("existing aggregates changed: %+v", resp)
	}
}
