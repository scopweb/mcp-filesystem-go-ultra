//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func listProcesses() ([]procInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []procInfo
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		name := string(bytesTrimNL(comm))
		exe, _ := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		ppid := readPPID(filepath.Join("/proc", e.Name(), "stat"))
		out = append(out, procInfo{PID: pid, PPID: ppid, Name: name, ExePath: exe})
	}
	return out, nil
}

func bytesTrimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func readPPID(statPath string) int {
	raw, err := os.ReadFile(statPath)
	if err != nil {
		return 0
	}
	rparen := -1
	for i := len(raw) - 1; i >= 0; i-- {
		if raw[i] == ')' {
			rparen = i
			break
		}
	}
	if rparen < 0 || rparen+1 >= len(raw) {
		return 0
	}
	fields := splitWS(string(raw[rparen+1:]))
	if len(fields) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(fields[1])
	return ppid
}

func splitWS(s string) []string {
	var out []string
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func terminatePID(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = p.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return p.Kill()
}
