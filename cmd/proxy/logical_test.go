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
	cases := []struct {
		name string
		want bool
	}{
		{"filesystem-ultra.exe", true},
		{"filesystem-ultra-c00a9cc.exe", true},
		{"filesystem-ultra-v4.exe", true},
		{"filesystem-ultra-v4-embed_rg.exe", true},
		{"FILESYSTEM-ULTRA-V4.EXE", true},
		{"filesystem-ultra-v4-dashboard.exe", false},
		{"filesystem-ultra-dashboard.exe", false},
		{"filesystem-ultra-proxy.exe", false},
		{"mcp-proxy.exe", false},
		{"dashboard.exe", false},
		{"filesystem-ultrax.exe", false},
	}
	for _, tc := range cases {
		if got := sameLogicalServer(tc.name, "filesystem-ultra"); got != tc.want {
			t.Errorf("sameLogicalServer(%q)=%v want %v", tc.name, got, tc.want)
		}
	}
	if sameLogicalServer("filesystem-ultra.exe", "") {
		t.Error("empty logical must never match")
	}
}

func TestGitHashOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\bin\filesystem-ultra-c00a9cc.exe`, "c00a9cc"},
		{`filesystem-ultra-6E61964.exe`, "6e61964"},
		{`filesystem-ultra-v4.exe`, ""},
		{`filesystem-ultra-v4-embed_rg.exe`, ""},
		{`filesystem-ultra.exe`, ""},
	}
	for _, tc := range cases {
		if got := gitHashOf(tc.in); got != tc.want {
			t.Errorf("gitHashOf(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
