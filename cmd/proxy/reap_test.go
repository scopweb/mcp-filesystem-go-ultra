package main

import "testing"

func reapedPIDs(ps []procInfo) map[int]bool {
	out := map[int]bool{}
	for _, p := range ps {
		out[p.PID] = true
	}
	return out
}

func TestPidsToReap(t *testing.T) {
	target := `C:\bin\filesystem-ultra-c00a9cc.exe`
	logical := "filesystem-ultra"
	alive := map[int]bool{100: true, 200: true}
	parentAlive := func(pid int) bool { return alive[pid] }

	procs := []procInfo{
		// variant with living parent (Desktop on embed_rg while we run c00a9cc): keep
		{PID: 1, PPID: 100, Name: "filesystem-ultra-v4-embed_rg.exe", ExePath: `C:\bin\filesystem-ultra-v4-embed_rg.exe`},
		// same build, parent dead: orphan
		{PID: 2, PPID: 0, Name: "filesystem-ultra-c00a9cc.exe", ExePath: target},
		// same build, other living client: keep
		{PID: 3, PPID: 200, Name: "filesystem-ultra-c00a9cc.exe", ExePath: target},
		{PID: 4, PPID: 200, Name: "mcp-proxy.exe", ExePath: `C:\bin\mcp-proxy.exe`},
		{PID: 5, PPID: 200, Name: "notepad.exe", ExePath: `C:\Windows\notepad.exe`},
		// older hash build, parent alive: old-hash
		{PID: 6, PPID: 100, Name: "filesystem-ultra-9935ea0.exe", ExePath: `C:\bin\filesystem-ultra-9935ea0.exe`},
		// variant, parent dead: orphan
		{PID: 7, PPID: 0, Name: "filesystem-ultra-v4.exe", ExePath: `C:\bin\filesystem-ultra-v4.exe`},
		// companion binaries, never
		{PID: 8, PPID: 100, Name: "filesystem-ultra-v4-dashboard.exe", ExePath: `C:\bin\filesystem-ultra-v4-dashboard.exe`},
		{PID: 9, PPID: 0, Name: "filesystem-ultra-v4-dashboard.exe", ExePath: `C:\bin\filesystem-ultra-v4-dashboard.exe`},
		// self
		{PID: 99, PPID: 1, Name: "filesystem-ultra-old.exe", ExePath: `C:\bin\filesystem-ultra-old.exe`},
	}

	got := reapedPIDs(pidsToReap(procs, target, logical, 99, parentAlive))
	want := map[int]bool{2: true, 6: true, 7: true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for pid := range want {
		if !got[pid] {
			t.Errorf("pid %d should be reaped", pid)
		}
	}
}

func TestPidsToReap_PlainTargetKeepsHashedSibling(t *testing.T) {
	// Target without a git hash cannot call another build "old": keep it.
	target := `C:\bin\filesystem-ultra-v4.exe`
	parentAlive := func(int) bool { return true }
	procs := []procInfo{
		{PID: 8, PPID: 7, Name: "filesystem-ultra-c00a9cc.exe", ExePath: `C:\bin\filesystem-ultra-c00a9cc.exe`},
	}
	if got := pidsToReap(procs, target, "filesystem-ultra", 1, parentAlive); len(got) != 0 {
		t.Fatalf("hashed sibling with living parent must be kept when target is unhashed, got %#v", got)
	}
}

func TestPidsToReap_KeepsOtherClient(t *testing.T) {
	target := `C:\bin\filesystem-ultra-c00a9cc.exe`
	parentAlive := func(pid int) bool { return pid == 7 }
	procs := []procInfo{
		{PID: 8, PPID: 7, Name: "filesystem-ultra-c00a9cc.exe", ExePath: target},
	}
	got := pidsToReap(procs, target, "filesystem-ultra", 1, parentAlive)
	if len(got) != 0 {
		t.Fatalf("living sibling client should be kept, got %#v", got)
	}
}
