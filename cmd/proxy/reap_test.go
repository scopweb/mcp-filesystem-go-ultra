package main

import "testing"

func TestPidsToReap(t *testing.T) {
	target := `C:\bin\filesystem-ultra-c00a9cc.exe`
	logical := "filesystem-ultra"
	alive := map[int]bool{100: true, 200: true}
	parentAlive := func(pid int) bool { return alive[pid] }

	procs := []procInfo{
		{PID: 1, PPID: 100, Name: "filesystem-ultra-v4-embed_rg.exe", ExePath: `C:\bin\filesystem-ultra-v4-embed_rg.exe`},
		{PID: 2, PPID: 0, Name: "filesystem-ultra-c00a9cc.exe", ExePath: target},
		{PID: 3, PPID: 200, Name: "filesystem-ultra-c00a9cc.exe", ExePath: target},
		{PID: 4, PPID: 200, Name: "mcp-proxy.exe", ExePath: `C:\bin\mcp-proxy.exe`},
		{PID: 5, PPID: 200, Name: "notepad.exe", ExePath: `C:\Windows\notepad.exe`},
		{PID: 99, PPID: 1, Name: "filesystem-ultra-old.exe", ExePath: `C:\bin\filesystem-ultra-old.exe`},
	}

	got := pidsToReap(procs, target, logical, 99, parentAlive)
	want := map[int]bool{1: true, 2: true}
	if len(got) != 2 {
		t.Fatalf("got %d pids %#v, want 2 (old-hash + orphan)", len(got), got)
	}
	for _, p := range got {
		if !want[p.PID] {
			t.Errorf("unexpected reap pid %d (%s)", p.PID, p.Name)
		}
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
