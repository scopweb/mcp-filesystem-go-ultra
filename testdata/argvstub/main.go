// argvstub is a test helper binary: each invocation appends its argv (sans
// argv[0]) as one JSON array line to the file named by STUB_ARGV_LOG, then
// prints "stub-ok" and exits 0. Tests build it under the name of a real tool
// (git, rg, cmd) and prepend its directory to PATH to capture exactly how the
// server constructs subprocess argv — proving, for example, that git arguments
// never pass through a command interpreter and that ripgrep receives the
// pattern after -e with flag parsing terminated by --.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if log := os.Getenv("STUB_ARGV_LOG"); log != "" {
		if f, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			if b, err := json.Marshal(os.Args[1:]); err == nil {
				f.Write(append(b, '\n'))
			}
			f.Close()
		}
	}
	fmt.Println("stub-ok")
}
