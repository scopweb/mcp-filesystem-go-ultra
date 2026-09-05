package main

import (
	"errors"
	"strings"
)

// failClosedMessage is printed to stderr when the server is started with no
// allowed paths and without --insecure-open. Exit code 2 (misuse).
const failClosedMessage = `filesystem-ultra requires at least one allowed path (fail-closed since v4.6.0).

Examples:
  filesystem-ultra.exe --allowed-paths C:\proj
  filesystem-ultra.exe C:\proj D:\other

Labs only (disables the sandbox):
  filesystem-ultra.exe --insecure-open`

var errFailClosed = errors.New(failClosedMessage)

func sanitizeAllowedPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func requireAllowedPaths(paths []string, insecureOpen bool) error {
	if insecureOpen {
		return nil
	}
	if len(sanitizeAllowedPaths(paths)) == 0 {
		return errFailClosed
	}
	return nil
}
