package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type procInfo struct {
	PID     int
	PPID    int
	Name    string
	ExePath string
}

func pathsEqual(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func sameTargetExe(p procInfo, targetPath string) bool {
	if p.ExePath != "" && pathsEqual(p.ExePath, targetPath) {
		return true
	}
	if p.ExePath == "" {
		return strings.EqualFold(exeBasename(p.Name), exeBasename(targetPath))
	}
	return false
}

func pidsToReap(procs []procInfo, targetPath, logical string, selfPID int, parentAlive func(int) bool) []procInfo {
	var out []procInfo
	for _, p := range procs {
		if p.PID == 0 || p.PID == selfPID {
			continue
		}
		if !sameLogicalServer(p.Name, logical) {
			continue
		}
		if sameTargetExe(p, targetPath) {
			if parentAlive(p.PPID) {
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

func reapStaleChildren(targetPath string) {
	logical := logicalServerName(targetPath)
	if logical == "" {
		return
	}
	procs, err := listProcesses()
	if err != nil {
		log.Printf("mcp-proxy: reap: list processes: %v", err)
		return
	}
	stale := pidsToReap(procs, targetPath, logical, os.Getpid(), processAlive)
	if len(stale) == 0 {
		return
	}
	for _, p := range stale {
		why := "old-hash"
		if sameTargetExe(p, targetPath) {
			why = "orphan"
		}
		log.Printf("mcp-proxy: reaping stale %s pid=%d ppid=%d path=%s reason=%s", p.Name, p.PID, p.PPID, p.ExePath, why)
		if err := terminatePID(p.PID, 2*time.Second); err != nil {
			log.Printf("mcp-proxy: reap pid=%d: %v", p.PID, err)
		}
	}
}
