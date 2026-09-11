//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func bindChildLifetime(p *os.Process) (func(), error) {
	return func() {
		if p != nil {
			_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
		}
	}, nil
}
