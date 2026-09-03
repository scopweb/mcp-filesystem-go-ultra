package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRequireAllowedPaths_EmptyIsFailClosed(t *testing.T) {
	if err := requireAllowedPaths(nil, false); err != errFailClosed {
		t.Fatalf("nil paths: got %v, want errFailClosed", err)
	}
	if err := requireAllowedPaths([]string{}, false); err != errFailClosed {
		t.Fatalf("empty slice: got %v, want errFailClosed", err)
	}
	if err := requireAllowedPaths([]string{"", "  ", "\t"}, false); err != errFailClosed {
		t.Fatalf("whitespace-only: got %v, want errFailClosed", err)
	}
}

func TestRequireAllowedPaths_InsecureOpenAllowsEmpty(t *testing.T) {
	if err := requireAllowedPaths(nil, true); err != nil {
		t.Fatalf("insecure-open + nil: %v", err)
	}
	if err := requireAllowedPaths([]string{"", " "}, true); err != nil {
		t.Fatalf("insecure-open + whitespace: %v", err)
	}
}

func TestRequireAllowedPaths_PresentOK(t *testing.T) {
	if err := requireAllowedPaths([]string{`C:\proj`}, false); err != nil {
		t.Fatalf("single path: %v", err)
	}
	if err := requireAllowedPaths([]string{"", `D:\other`, "  "}, false); err != nil {
		t.Fatalf("mixed empty + real: %v", err)
	}
}

func TestSanitizeAllowedPaths_DropsEmpty(t *testing.T) {
	got := sanitizeAllowedPaths([]string{"  C:\\a  ", "", "D:\\b"})
	if len(got) != 2 || got[0] != `C:\a` || got[1] != `D:\b` {
		t.Fatalf("got %#v", got)
	}
}

func TestFailClosed_Binary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build in -short")
	}
	exe := filepath.Join(t.TempDir(), "fsu-failclosed")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	t.Run("no_args_exit_2", func(t *testing.T) {
		cmd := exec.Command(exe)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected exit 2, got success. output:\n%s", out)
		}
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("expected ExitError, got %T %v", err, err)
		}
		if ee.ExitCode() != 2 {
			t.Errorf("exit code = %d, want 2. output:\n%s", ee.ExitCode(), out)
		}
		if !strings.Contains(string(out), "fail-closed") {
			t.Errorf("stderr/stdout must mention fail-closed, got:\n%s", out)
		}
	})

	t.Run("version_without_paths_ok", func(t *testing.T) {
		ver := exec.Command(exe, "--version")
		verOut, verErr := ver.CombinedOutput()
		if verErr != nil {
			t.Fatalf("--version without allowed paths must succeed: %v\n%s", verErr, verOut)
		}
		if !strings.Contains(string(verOut), serverVersion) {
			t.Errorf("--version output %q does not contain %s", verOut, serverVersion)
		}
	})

	t.Run("insecure_open_does_not_exit_2", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "--insecure-open")
		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		err := cmd.Wait()
		if ctx.Err() == context.DeadlineExceeded {
			return
		}
		if err == nil {
			return
		}
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 2 {
			t.Fatal("--insecure-open must not fail-closed (exit 2)")
		}
	})
}
