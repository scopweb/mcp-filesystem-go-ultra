package main

import (
	"fmt"
	"io"
	"os/exec"
)

func startChild(args []string, errOut io.Writer, extraEnv []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, func(), error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = errOut
	if extraEnv != nil {
		cmd.Env = extraEnv
	}
	configureChild(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, nil, nil, nil, err
	}

	cleanup, err := bindChildLifetime(cmd.Process)
	if err != nil {
		fmt.Fprintf(errOut, "mcp-proxy: child lifetime bind failed (orphans possible on crash): %v\n", err)
		cleanup = func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	}
	return cmd, stdin, stdout, cleanup, nil
}
