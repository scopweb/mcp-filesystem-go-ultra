package core

import (
	"runtime"
	"strings"
	"testing"
)

// TestValidatePathSecurity_PseudoLinuxOnWindows checks that Unix-style absolute
// paths leaked from a Linux container or sandbox are rejected on Windows hosts.
// This is the v4.2.2 mitigation for the create_file confusion bug where paths
// like /home/claude/x.txt would be silently rewritten by filepath.Abs to
// C:\home\claude\x.txt instead of failing loudly.
func TestValidatePathSecurity_PseudoLinuxOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("pseudo-Linux path rejection only applies on Windows")
	}

	rejected := []string{
		"/home/claude/test.txt",
		"/tmp/scratch.go",
		"/root/.ssh/id_rsa",
		"/var/log/syslog",
		"/etc/passwd",
		"/usr/local/bin/foo",
		"/opt/app/data",
		"/proc/self/maps",
		"/sessions/abc/mnt/outputs/x.txt", // Cowork container path
		"/dev/null",
		"/run/systemd/private",
		"/mnt",       // partial WSL prefix without drive letter
		"/mnt/",      // partial WSL prefix without drive letter
		"/mnt/cd/x",  // multi-letter "drive" — not a valid WSL mount
		"/notadrive", // arbitrary unix-absolute path
	}
	for _, p := range rejected {
		if err := ValidatePathSecurity(p); err == nil {
			t.Errorf("expected rejection for %q on Windows but got nil", p)
		} else if !strings.Contains(err.Error(), "Unix-style") {
			t.Errorf("expected Unix-style error for %q, got: %v", p, err)
		}
	}
}

// TestValidatePathSecurity_AllowedPathsOnWindows ensures the pseudo-Linux check
// does not over-reject legitimate Windows or WSL paths.
func TestValidatePathSecurity_AllowedPathsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("WSL/Windows path acceptance check only meaningful on Windows")
	}

	accepted := []string{
		`C:\Users\foo\bar.txt`,
		`C:/Users/foo/bar.txt`,
		`D:\repo\file.go`,
		"/mnt/c/Users/foo",      // canonical WSL path
		"/mnt/c",                // drive-only WSL path
		"/mnt/c/",               // drive-only WSL path with trailing slash
		"/mnt/d/Projects/x.txt", // another drive
		`relative\path\file.txt`,
		`./relative/file.txt`,
		`file.txt`,
		"", // empty input is treated as no-op
	}
	for _, p := range accepted {
		if err := ValidatePathSecurity(p); err != nil {
			// /mnt/c is allowed by the pseudo-Linux check; other validators may
			// still reject (e.g. NTFS ADS) but should not for these inputs.
			t.Errorf("expected acceptance for %q on Windows but got: %v", p, err)
		}
	}
}

// TestValidatePathSecurity_PseudoLinuxOnUnix ensures the new check is a no-op
// on non-Windows hosts. /home/foo is a perfectly valid path on Linux/macOS.
func TestValidatePathSecurity_PseudoLinuxOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-host behavior cannot be exercised on Windows")
	}
	for _, p := range []string{"/home/foo", "/tmp/x", "/root/y", "/var/log/z"} {
		if err := ValidatePathSecurity(p); err != nil {
			t.Errorf("expected acceptance for %q on %s but got: %v", p, runtime.GOOS, err)
		}
	}
}

// TestIsPseudoLinuxPathOnWindows_DirectMatrix exercises the helper directly so
// regressions are easy to localize. The function is platform-aware: it returns
// false on non-Windows hosts regardless of input.
func TestIsPseudoLinuxPathOnWindows_DirectMatrix(t *testing.T) {
	cases := []struct {
		path string
		want bool // expected result on Windows
	}{
		// Pseudo-Linux — should reject on Windows
		{"/home/claude", true},
		{"/tmp/x", true},
		{"/sessions/abc", true},
		{"/", true},
		{"/mnt", true},
		{"/mnt/", true},
		{"/mnt/cd/x", true}, // multi-letter mount

		// Legitimate WSL — should accept
		{"/mnt/c", false},
		{"/mnt/c/", false},
		{"/mnt/c/Users", false},
		{"/mnt/D/repo", false}, // uppercase drive letter

		// Non-absolute or Windows — should accept
		{"", false},
		{"file.txt", false},
		{`C:\foo`, false},
		{`C:/foo`, false},
		{`relative\path`, false},
	}
	for _, tc := range cases {
		got := isPseudoLinuxPathOnWindows(tc.path)
		// On non-Windows, isPseudoLinuxPathOnWindows always returns false.
		expected := tc.want && runtime.GOOS == "windows"
		if got != expected {
			t.Errorf("isPseudoLinuxPathOnWindows(%q) = %v, want %v (GOOS=%s)",
				tc.path, got, expected, runtime.GOOS)
		}
	}
}

// TestValidatePathSecurity_ExportedAlias ensures the public ValidatePathSecurity
// wrapper preserves the same behavior as the unexported helper. Handlers rely
// on the public version to surface a specific error message instead of the
// engine's generic "access denied".
func TestValidatePathSecurity_ExportedAlias(t *testing.T) {
	// Reserved name — should reject on all platforms.
	if err := ValidatePathSecurity("CON"); err == nil {
		t.Error("expected ValidatePathSecurity to reject reserved name 'CON'")
	}
	// Empty — should accept (treated as no-op).
	if err := ValidatePathSecurity(""); err != nil {
		t.Errorf("expected ValidatePathSecurity('') to be nil, got: %v", err)
	}
}
