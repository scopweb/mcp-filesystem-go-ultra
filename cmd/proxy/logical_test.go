package main

import "testing"

func TestLogicalServerName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`C:\bin\filesystem-ultra-c00a9cc.exe`, "filesystem-ultra"},
		{`filesystem-ultra-v4-embed_rg.exe`, "filesystem-ultra"},
		{`filesystem-ultra-v4.exe`, "filesystem-ultra"},
		{`filesystem-ultra.exe`, "filesystem-ultra"},
		{`mcp-go-mssql-abc1234`, "mcp-go-mssql"},
		{`dashboard.exe`, "dashboard"},
	}
	for _, tc := range cases {
		if got := logicalServerName(tc.in); got != tc.want {
			t.Errorf("logicalServerName(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSameLogicalServer(t *testing.T) {
	if !sameLogicalServer("filesystem-ultra-c00a9cc.exe", "filesystem-ultra") {
		t.Fatal("hash build should match")
	}
	if !sameLogicalServer("filesystem-ultra-v4-embed_rg.exe", "filesystem-ultra") {
		t.Fatal("versioned build should match")
	}
	if sameLogicalServer("mcp-proxy.exe", "filesystem-ultra") {
		t.Fatal("proxy must not match")
	}
	if sameLogicalServer("dashboard.exe", "filesystem-ultra") {
		t.Fatal("dashboard must not match")
	}
}
